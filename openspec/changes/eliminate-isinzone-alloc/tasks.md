
## 1. 先寫測試（TDD baseline）

- [x] **(修法用「邊界檢查」而非「預 cache dottedZones」)** 在 `internal/dnsutil/dnsutil_test.go` 新增 `TestIsInZone_EdgeCases` 含 5 個 fixture：(a) `name == zone`（"foo.com." vs "foo.com." → true）、(b) 正常子網域（"a.foo.com." vs "foo.com." → true）、(c) 後綴像 zone 但邊界不對（"oo.com." vs "foo.com." → false；"barfoo.com." vs "foo.com." → false）、(d) `name` 比 `zone` 短（"o.com." vs "foo.com." → false）、(e) 空字串（"" vs "foo.com." → false；"foo.com." vs "" → 與舊實作行為一致）
- [x] **(同 change 補 micro-benchmark)** 在同檔新增 `BenchmarkIsInZone` 含 4 sub-benchmark：`b.Run("Equal", ...)`、`b.Run("Subdomain", ...)`、`b.Run("BoundaryMismatch", ...)`、`b.Run("Unrelated", ...)`，每個都呼叫 `b.ReportAllocs()`
- [x] [P] 跑 `go test ./internal/dnsutil/...`，確認舊實作下 5 個 fixture 全 pass（建立 TDD baseline）
- [x] [P] 跑 `go test -bench=BenchmarkIsInZone -benchmem -count=3 ./internal/dnsutil/...`，記錄舊實作每 sub-benchmark 的 ns/op 與 allocs/op，作為「修法前」基準（預期 Subdomain/BoundaryMismatch case 應顯示 1 alloc/op）

## 2. 實作新版 IsInZone

- [x] **(條件順序：`len` → byte 邊界 → `HasSuffix`)** **(Constraints)** 改寫 `internal/dnsutil/dnsutil.go:47-49` 的 `IsInZone` 為三層條件（先 `name == zone`，再 `len(name) > len(zone)`，再 `name[len(name)-len(zone)-1] == '.'`，最後 `strings.HasSuffix(name, zone)`），保留原 doc comment「returns true iff name equals zone or is a subdomain of zone」並補一句說明邊界檢查避免 alloc — 對應 design.md Constraints「不得 alloc」與「修改不得改變 IsInZone 對任何輸入的回傳值」
- [x] 跑 `go test ./internal/dnsutil/...`，確認 5 個 fixture 在新實作下全 pass（語義不變）

## 3. Benchmark 驗證 0 alloc

- [x] [P] 跑 `go test -bench=BenchmarkIsInZone -benchmem -count=3 ./internal/dnsutil/...`，斷言所有 sub-benchmark 的 allocs/op == 0；ns/op 相對舊實作應顯著下降（特別是 Subdomain case，預期至少 50% 下降）
- [x] [P] 把 before/after benchmark 數字以表格形式寫入後續 commit message 草稿（保存於 `.local/dnspyre/report/eliminate-isinzone-alloc-bench.md`）

## 4. 回歸測試與既有套件

- [x] [P] **(Risks: 邊界檢查 edge case)** 跑 `make test`（含 `-race -count=1`），重點看 `internal/zone/parser_test.go`（zone parse 走 IsInZone）、`internal/alias/detect_test.go`（hot path）、`internal/api/server_test.go`（ephemeral API 走 IsInZone）三套是否全綠
- [x] [P] 跑 `make lint`，確保 golangci-lint 無新 warning
- [x] 跑 `make smoke`，確認 `--dry-run` 不 panic

## 5. 部署到 ns2

- [x] 跑 `VERSION="0.0.0-eliminate-isinzone-alloc" make deb`，確認 `shadowdns_0.0.0~eliminate-isinzone-alloc_amd64.deb` 產出於 repo root
- [x] 將 `.deb` 檔案 `scp` 至 `bench-ns2:/tmp/`
- [x] `ssh bench-ns2 "sudo dpkg -i /tmp/shadowdns_0.0.0~eliminate-isinzone-alloc_amd64.deb"`，確認返回 0；接受 dpkg 對既有 `0.0.0~pool-response-msg` 的 alphabetical 比較警告
- [x] 安裝成功後刪除 ns2 與本機的 `.deb` 檔案
- [x] `ssh bench-ns2 "systemctl is-active shadowdns"` 確認服務 active
- [x] 等待 ≥12 min warm-up：以 `dig @198.18.0.8 +tries=1 +time=2 +short A cname-banner.games.example.com` 連續成功 3 次（回傳 NOERROR + answer）作為「服務已準備好」訊號

## 6. dnspyre 壓測（兩 workload × 2 輪）

- [x] [P] 從 ns1 跑 CNAME 測試 run #1：`ssh bench-ns1 "dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/cname-domains.txt"`，輸出存 `.local/dnspyre/report/raw-198.18.0.8-cname-ns1-<timestamp>-eliminate-isinzone-alloc.txt`
- [x] [P] 從 ns1 跑 A 紀錄測試 run #1：同 flags，測資 `@/tmp/a-domains.txt`，輸出存 `.local/dnspyre/report/raw-198.18.0.8-adomains-ns1-<timestamp>-eliminate-isinzone-alloc.txt`
- [x] **(Risks: 同 binary run-to-run variance 達 4.4%)** 重啟 ns2 shadowdns 取得 fresh warm-up window，再等 ≥12 min warm-up
- [x] [P] CNAME run #2：同上 flags，輸出存 `<timestamp>-eliminate-isinzone-alloc-run2.txt`
- [x] [P] A run #2：同上，輸出帶 `-run2` 後綴

## 7. pprof before/after 對比

- [x] **(Goals: 驗證 concatstring2 + memmove cum < 5%)** 在 ns1 上 dnspyre 壓測進行中（取一輪 CNAME 跑到 ~15s 時），從 ns1 抓 30s CPU profile：`ssh bench-ns1 "curl -sf -o /tmp/cpu-after-<timestamp>.pprof http://198.18.0.8:9153/debug/pprof/profile?seconds=30"`
- [x] 同步抓 allocs profile：`curl -sf -o /tmp/allocs-after-<timestamp>.pprof http://198.18.0.8:9153/debug/pprof/allocs`
- [x] 把兩 profile 從 ns1 scp 回本機 `.local/dnspyre/pprof/`
- [x] 跑 `go tool pprof -top -cum -nodecount=20 .local/dnspyre/pprof/cpu-after-<timestamp>.pprof`，記錄前 10 hot function 與其 cum %
- [x] 跑 `go tool pprof -base .local/dnspyre/pprof/cpu-20260509-104840.pprof -top -cum .local/dnspyre/pprof/cpu-after-<timestamp>.pprof`，產出 before/after diff，記錄 `runtime.concatstring2`、`runtime.memmove`、`alias.Detect`、`IsInZone` 四個 function 的 cum delta

## 8. 解析與比較報告

- [x] [P] 解析 dnspyre 4 份 raw 輸出（`tr -d '\r' | sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' | grep -v Progress | tail -40`），抓 QPS、總請求數、NOERROR/NXDOMAIN/REFUSED 絕對值與 rate、min/mean/sd/p50/p75/p90/p95/p99/max
- [x] **(Goals: dnspyre QPS +30% 門檻、+50% 主目標)** 撰寫 `.local/dnspyre/report/compare-baseline-vs-eliminate-isinzone-alloc.md`，比對對象 `.local/dnspyre/report/baseline-shadowdns-pre-eliminate-udp-double-pack.md`（CNAME 10,538 / A 10,741）；含 dnspyre 表格、pprof before/after 對比、是否達 +30% 門檻 / +50% 主目標的判定
- [x] 在比較報告中明確記錄結論的 3 種可能與下一步建議：(a) 達 +50% 主目標 → 等候 user 同意 commit、規劃 follow-up B 系列；(b) +30%–+50% 達門檻未達主目標 → commit、評估是否做 alias.Detect loop 重構；(c) < +30% → commit（hot-path 0 alloc 是無 regress 的進步），但要規劃 follow-up profile-after 為導向的 change

## 9. 請使用者驗證並決定 commit

- [x] 請使用者檢視 `compare-baseline-vs-eliminate-isinzone-alloc.md` 結果，並決定是否 commit
- [ ] 若使用者同意 commit：用 `git add internal/dnsutil/dnsutil.go internal/dnsutil/dnsutil_test.go` 加上 `openspec/changes/eliminate-isinzone-alloc/` 目錄，commit message 用 `perf(dnsutil): eliminate string concat in IsInZone hot path` 並附 benchmark before/after 表格 + dnspyre 結果摘要
- [ ] 若使用者不同意 commit：保留 working tree 改動但不 commit；告知 user 如何 discard（`git restore` 與 `rm -rf openspec/changes/eliminate-isinzone-alloc`）
