本 tasks 對應 `design.md` 各項決策（括號為決策標題）。

## 1. TDD baseline — 補強現有 zone test

- [x] **(用 `dnsutil.IsInZone` 而非 inline boundary-check)** 檢視 `internal/zone/zone_test.go` 既有的 `LookupWildcard` 與 `FollowCNAME` test 覆蓋率，確認以下 case 都有 fixture（沒有則補上）：(a) `parent` / `target` 等於 `z.Origin`、(b) 嚴格子網域（HasSuffix true）、(c) 後綴像 zone 但邊界不對（例如 origin="foo.com."、target="barfoo.com."）、(d) parent / target 完全無關（HasSuffix false）
- [x] [P] 跑 `go test ./internal/zone/...`，確認既有 + 補上的 fixture 在舊實作下全 pass（建立 TDD baseline）

## 2. 實作改動

- [x] **(LookupWildcard 的 `parent == z.Origin` 早 break 保留)** 改寫 `internal/zone/zone.go:127, 143`（`LookupWildcard`）：移除 `originSuffix := "." + z.Origin` 變數，將 `if !strings.HasSuffix(parent, originSuffix)` 改為 `if !dnsutil.IsInZone(parent, z.Origin)`，保留前面的 `if parent == z.Origin { break }`
- [x] **(FollowCNAME 的 `target != z.Origin && ...` 折疊成單一 IsInZone)** 改寫 `internal/zone/zone.go:243, 252`（`FollowCNAME`）：移除 `originSuffix := "." + z.Origin` 變數，將 `if target != z.Origin && !strings.HasSuffix(target, originSuffix)` 改為 `if !dnsutil.IsInZone(target, z.Origin)`
- [x] **(Constraints)** 在 `internal/zone/zone.go` import block 新增 `"github.com/chenwei791129/ShadowDNS/internal/dnsutil"`，確認 `strings` 仍有其他使用點未廢棄（如有則保留 import）— 對應 design.md Constraints「不得引入循環依賴」與「兩個函式對外行為 byte-equivalent」
- [x] 跑 `go test ./internal/zone/...`，確認 §1 fixture 在新實作下全 pass（語義不變）

## 3. 回歸與既有測試

- [x] [P] **(Risks: dnsutil 循環引用)** 跑 `go build ./...` 確認新 import 不觸發循環依賴錯誤
- [x] [P] **(Risks: 邊界 edge case)** 跑 `make test`（含 `-race -count=1`），重點看 `internal/zone/zone_test.go` 全綠、下游 `internal/server/handler_test.go`（呼叫 LookupWildcard / FollowCNAME 的整合 test）全綠
- [x] [P] 跑 `make lint`，確保 golangci-lint 無新 warning
- [x] 跑 `make smoke`，確認 `--dry-run` 不 panic

## 4. 部署到 ns2

- [x] 跑 `VERSION="0.0.0-eliminate-zone-origin-concat" make deb`，確認 `shadowdns_0.0.0~eliminate-zone-origin-concat_amd64.deb` 產出於 repo root
- [x] `scp shadowdns_0.0.0~eliminate-zone-origin-concat_amd64.deb bench-ns2:/tmp/`
- [x] `ssh bench-ns2 "sudo dpkg -i /tmp/shadowdns_0.0.0~eliminate-zone-origin-concat_amd64.deb"`，確認返回 0
- [x] 安裝成功後刪除 ns2 與本機的 `.deb` 檔案
- [x] `ssh bench-ns2 "systemctl restart shadowdns && systemctl is-active shadowdns"` 確認服務 active
- [x] 等待 ≥15 min warm-up：以 `dig @198.18.0.8 +tries=1 +time=2 +short A cname-banner.games.example.com` 連續成功 3 次（回傳 NOERROR + answer）作為「服務已準備好」訊號

## 5. dnspyre 壓測（兩 workload × 2 輪）

- [x] [P] 從 ns1 跑 CNAME 測試 run #1：`ssh bench-ns1 "dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/cname-domains.txt"`，輸出存 `.local/dnspyre/report/raw-198.18.0.8-cname-ns1-<timestamp>-eliminate-zone-origin-concat.txt`
- [x] [P] 從 ns1 跑 A 紀錄測試 run #1：同 flags，測資 `@/tmp/a-domains.txt`，輸出存 `.local/dnspyre/report/raw-198.18.0.8-adomains-ns1-<timestamp>-eliminate-zone-origin-concat.txt`
- [x] 重啟 ns2 shadowdns 取得 fresh warm-up window，再等 ≥15 min warm-up（重複 §4 最後一步 readiness probe）
- [x] [P] CNAME run #2：同上 flags，輸出存 `<timestamp>-eliminate-zone-origin-concat-run2.txt`
- [x] [P] A run #2：同上，輸出帶 `-run2` 後綴

## 6. pprof before/after 對比

- [x] **(Goals: 驗證 concatstring2 cum < 1%)** 在 ns1 上 dnspyre 壓測進行中（取 run #2 CNAME 跑到 ~15s 時），從 ns1 抓 30s CPU profile：`ssh bench-ns1 "curl -sf -o /tmp/cpu-after-<timestamp>.pprof 'http://198.18.0.8:9153/debug/pprof/profile?seconds=30'"`
- [x] 同步抓 allocs profile：`curl -sf -o /tmp/allocs-after-<timestamp>.pprof http://198.18.0.8:9153/debug/pprof/allocs`
- [x] 把兩 profile 從 ns1 scp 回本機 `.local/dnspyre/pprof/`，命名為 `cpu-after-<timestamp>-zone-origin-concat.pprof` 等
- [x] 跑 `go tool pprof -top -cum -nodecount=20 .local/dnspyre/pprof/cpu-after-<timestamp>-zone-origin-concat.pprof`，記錄前 10 hot function 與其 cum %
- [x] 跑 `go tool pprof -peek concatstring2 .local/dnspyre/pprof/cpu-after-<timestamp>-zone-origin-concat.pprof`，記錄 `concatstring2` cum % 與剩餘 callers 分布；確認 `zone.FollowCNAME` 與 `zone.LookupWildcard` 已不在 callers 列表
- [x] 跑 `go tool pprof -base .local/dnspyre/pprof/cpu-after-20260509-120844-run2.pprof -top -cum .local/dnspyre/pprof/cpu-after-<timestamp>-zone-origin-concat.pprof`，產出 before/after diff，記錄主要 function 的 cum delta

## 7. 解析與比較報告

- [x] [P] 解析 dnspyre 4 份 raw 輸出（`tr -d '\r' | sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' | grep -v Progress | tail -40`），抓 QPS、總請求數、NOERROR/NXDOMAIN/REFUSED 絕對值與 rate、min/mean/sd/p50/p75/p90/p95/p99/max
- [x] **(Goals: dnspyre QPS +2% 門檻、+4% 主目標)** 撰寫 `.local/dnspyre/report/compare-isinzone-vs-eliminate-zone-origin-concat.md`，比對對象 `.local/dnspyre/report/compare-baseline-vs-eliminate-isinzone-alloc.md` 的 Run #2 數據（CNAME 31,619 / A 32,886 QPS）；含 dnspyre 表格、pprof before/after 對比、是否達 +2% 門檻 / +4% 主目標的判定
- [x] **(Open Question: NXDOMAIN +1.58 pp 是否消失)** 在比較報告中明確記錄 A workload NXDOMAIN rate vs run #2 的 ~2.55% 是否仍然一致（驗證本 change 未誤改變行為），預期仍 ~2.55%
- [x] 在比較報告中明確記錄結論的 3 種可能與下一步建議：(a) 達 +4% 主目標 → 等候 user 同意 commit、規劃 #2 view.CountryDB.Lookup；(b) +2%–+4% 達門檻未達主目標 → commit、評估剩餘 concatstring2 source；(c) < +2% → commit（仍 0 alloc 進步）但要評估是否轉攻 #2

## 8. 請使用者驗證並決定 commit

- [x] 請使用者檢視 `compare-isinzone-vs-eliminate-zone-origin-concat.md` 結果，並決定是否 commit
- [ ] 若使用者同意 commit：用 `git add internal/zone/zone.go internal/zone/zone_test.go` 加上 `openspec/changes/eliminate-zone-origin-concat/` 目錄，commit message 用 `perf(zone): eliminate "."+origin concat in LookupWildcard/FollowCNAME` 並附 dnspyre + pprof 結果摘要
- [ ] 若使用者不同意 commit：保留 working tree 改動但不 commit；告知 user 如何 discard（`git restore` 與 `rm -rf openspec/changes/eliminate-zone-origin-concat`）
