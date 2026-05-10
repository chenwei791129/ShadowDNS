# Tasks

## 1. 改寫 addrFromRemote 為 type-switch fast path

- [x] 1.1 修改 `internal/server/handler.go` 的 `addrFromRemote` 函式 body，依 design.md 決策 1-4 實作 type switch（`*net.UDPAddr` / `*net.TCPAddr` / default）。
  - 行為合約：簽章 `(dns.ResponseWriter) (netip.Addr, error)` 不變；UDP / TCP arm 用 `netip.AddrFromSlice(.IP)` + `.Unmap()` 構造 `Addr`；`AddrFromSlice` 的 `ok=false` 回顯式 error；default arm 完整保留 `SplitHostPort + ParseAddr` 邏輯不變。
  - 修改範圍：僅 `internal/server/handler.go` line 526-540（函式 body）。函式以外文件其他內容、其他函式、其他檔案皆不動。
  - 驗證：`go build ./...` 成功；既有測試 `go test -race -count=1 ./internal/server/...` 不 regress（既有 `recordingWriter` 用 `*net.UDPAddr` 命中 fast path）。

## 2. 新增單元測試覆蓋 5 個 case

- [x] 2.1 [P] 在 `internal/server/handler_test.go` 新增 `TestAddrFromRemote` 表格驅動測試，覆蓋 design.md 驗收條件列出的 5 個 case。
  - 行為合約：測試表格至少含以下 5 行：
    1. `*net.UDPAddr` with `IP=net.IPv4(1,2,3,4).To4()`（4-byte），expect `(netip.MustParseAddr("1.2.3.4"), nil)`，且 `result.Is4() == true`。
    2. `*net.UDPAddr` with `IP=net.IPv4(1,2,3,4)`（Go std lib 預設 16-byte v4-in-v6 form），expect 與 case 1 相同 `Addr`（驗證 `Unmap` canonicalization）。
    3. `*net.TCPAddr` with `IP=net.IPv4(5,6,7,8).To4()`，expect `(netip.MustParseAddr("5.6.7.8"), nil)`。
    4. `addr == nil` stub（自定 ResponseWriter 回 nil），expect `(netip.Addr{}, error)`，error 訊息含 "nil remote addr"。
    5. default fallback：自定 ResponseWriter 回一個非 `*net.UDPAddr` / `*net.TCPAddr` 的 stub `net.Addr`，其 `String()` 回 `"9.10.11.12:5000"`（含 port，以走通 default arm 的 `SplitHostPort + ParseAddr`），expect `(netip.MustParseAddr("9.10.11.12"), nil)`；驗證 default arm 不 panic 且邏輯等價於舊路徑。
  - 修改範圍：`internal/server/handler_test.go` 新增測試函式；不動既有測試。
  - 驗證：`go test -race -count=1 ./internal/server/... -run 'TestAddrFromRemote'` 全綠。

## 3. 完整測試與靜態檢查

- [x] 3.1 跑 `make test`、`make lint`、`make smoke` 並全綠。
  - 行為合約：所有單元測試（含 race detector + count=1）通過；`golangci-lint run` 無 finding；`shadowdns --dry-run` 載入既有測試 config 無 error。
  - 驗證：三個 make target 各自 exit code 0；輸出無 FAIL/ERROR 字樣。

## 4. 本地 build deb 並部署到 bench-ns2

- [x] 4.1 用 release-shadowdns skill local-change mode build 並部署。
  - 行為合約：`shadowdns_0.0.0~eliminate-client-addr-string-alloc_amd64.deb` 安裝在 bench-ns2，`dpkg -l shadowdns` 顯示 `0.0.0~eliminate-client-addr-string-alloc`；`systemctl is-active shadowdns` 回 `active`；`/var/log/shadowdns/shadowdns.log` 在 30s 觀察窗內無 error/fatal/panic 關鍵字。
  - 執行：`make deb` with `VERSION=0.0.0-eliminate-client-addr-string-alloc` → `scp` → `dpkg -i` → `systemctl restart` → `restart_and_watch.sh`。
  - 驗證：skill 的 `restart_and_watch.sh` exit code 0；ssh 上 `dpkg -l shadowdns` 顯示新版本字串。

## 5. dnspyre 壓測 + pprof 比對

- [x] 5.1 從 bench-ns1 跑 dnspyre 與 30s pprof，產出比較報告。
  - 行為合約：產生比較報告 `.local/dnspyre/report/compare-baseline-recheck-vs-eliminate-client-addr-string-alloc.md`，含 (1) QPS 對照表（baseline = `migrate-geoip-to-mmdb-v2` post-commit 31,250.6 QPS，或重抓 baseline-recheck 取較近的）、(2) NXDOMAIN/REFUSED rate 變化（必須 ≤ 0.05pp）、(3) pprof 中 `addr.String` / `SplitHostPort` / `ParseAddr` 是否消失/下降、(4) 達標判定（QPS +1-2%、行為等價）。
  - 執行：`ssh bench-ns1 "dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/cname-domains.txt > /tmp/dnspyre-cname-<TS>.txt 2>&1"` 配 `ssh bench-ns2 "curl -s -o /tmp/cpu-after-<TS>.pprof http://127.0.0.1:9153/debug/pprof/profile?seconds=30"`；報告依 `local-dnspyre-benchmark` skill 模板。
  - 驗證：報告檔案存在；報告明確標示是否達標、是否行為等價（NXDOMAIN/REFUSED rate 對照）。

- [x] 5.2 根據 5.1 結果更新 plan §4 Tier B1 實測收益欄位。
  - 行為合約：`.local/plans/2026-05-06-qps-regression-vs-bind9-fix.md` §4 Tier B1 段落附上實測 QPS Δ% 與行為等價驗證結論，使下次重排 B 系列優先級時有對照基準。
  - 驗證：grep 該檔案能找到 `eliminate-client-addr-string-alloc` 字串與本次實測結果。
