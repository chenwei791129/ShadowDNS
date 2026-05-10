# Tasks

## 1. LookupKey fast-path

- [x] 1.1 修改 `internal/dnsutil/dnsutil.go`：在 `LookupKey` 函式內部加 fast-path（全 ASCII 小寫 && 尾 `.` → return name），並新增私有 helper `isAlreadyLookupKey(s string) bool`。
  - 行為合約：簽章 `LookupKey(name string) string` 不變；fast-path 命中 0 alloc / return == 輸入字串；fast-path miss 退回原 `ToLower + TrimSuffix + "."` 路徑；helper 為 byte-loop ASCII range check（`c >= 'A' && c <= 'Z'`），不用 `unicode` package。
  - 修改範圍：`internal/dnsutil/dnsutil.go` 第 37-42 行（LookupKey body）+ 加 helper（建議放在 `IsInZone` 後或 `LookupKey` 前）。函式以外文件其他內容、其他函式皆不動。
  - 驗證：`go build ./...` 成功；`go test -race -count=1 ./internal/dnsutil/...` 既有測試不 regress。

## 2. LookupKey 測試與 benchmark

- [x] 2.1 [P] 在 `internal/dnsutil/dnsutil_test.go` 新增 `TestLookupKey_FastPath` 表格驅動測試 + `BenchmarkLookupKey_FastPath` + `BenchmarkLookupKey_SlowPath`。
  - 行為合約：測試表格至少 5 個 case：(1) `""` → `""`；(2) `"example.com."` (lookup form) → `"example.com."`；(3) `"example.com"` (no trailing dot) → `"example.com."`；(4) `"Example.COM."` (mixed case) → `"example.com."`；(5) `"εxample.com."` (non-ASCII) → `"εxample.com."`。Benchmark：fast-path 必須 `0 allocs/op`，slow-path baseline 紀錄當下 alloc 數。
  - 修改範圍：`internal/dnsutil/dnsutil_test.go` 新增測試 + benchmark 函式；不動既有測試。
  - 驗證：`go test -race -count=1 ./internal/dnsutil/... -run 'TestLookupKey_FastPath'` 全綠；`go test -bench BenchmarkLookupKey_FastPath -benchmem ./internal/dnsutil/...` 顯示 fast-path 0 allocs/op。

## 3. RewriteName boundary check 改 index 數學

- [x] 3.1 修改 `internal/alias/rewrite.go` 的 `RewriteName` 函式 body（line 45-60 附近）：移除 `suffix := "." + root` concat，改用 `lower[ll-rl-1] == '.'` + `lower[ll-rl:] == root` 雙條件。
  - 行為合約：簽章 `RewriteName(n, root, backup string) string` 不變；對 root suffix match：`prefix + "." + backup`（1 alloc）；對 root exact match：`backup`（0 alloc beyond ToLower）；對 no match：return `n`（0 alloc beyond ToLower）；對 boundary 缺 `.` 前綴（如 `"XXalias.com."`、root=`"alias.com."`）：return `n` unchanged。
  - 修改範圍：僅 `internal/alias/rewrite.go` line 45-60（RewriteName body）。不動 `RewriteQName`、`RewriteNameAnywhere`、`RewriteRR` 與其他函式。
  - 驗證：`go test -race -count=1 ./internal/alias/...` 既有測試不 regress（`recordingWriter` 等既有 fixture 行為不變）。

## 4. RewriteName 測試與 benchmark

- [x] 4.1 [P] 在 `internal/alias/rewrite_test.go` 新增 `TestRewriteName_BoundaryCases` 表格驅動測試 + `BenchmarkRewriteName_SuffixMatch` + `BenchmarkRewriteName_NoMatch`。
  - 行為合約：測試表格至少 6 個 case：(1) `("", root, backup)` → `""`；(2) `("WWW.alias.com.", "alias.com.", "real.com.")` → `"WWW.real.com."`；(3) `("alias.com.", "alias.com.", "real.com.")` → `"real.com."`；(4) `("other.com.", "alias.com.", "real.com.")` → `"other.com."`；(5) `("XXalias.com.", "alias.com.", "real.com.")` → `"XXalias.com."`（boundary 無 `.`）；(6) `("a.alias.com.", "alias.com.", "real.com.")` → `"a.real.com."`（label 邊界正好命中）。Benchmark：suffix-match 必須 `1 allocs/op`（從 baseline 2 降到 1）；no-match baseline 紀錄。
  - 修改範圍：`internal/alias/rewrite_test.go` 新增測試 + benchmark 函式；不動既有測試。
  - 驗證：`go test -race -count=1 ./internal/alias/... -run 'TestRewriteName_BoundaryCases'` 全綠；`go test -bench BenchmarkRewriteName -benchmem ./internal/alias/...` 顯示 suffix-match 1 allocs/op。

## 5. 完整測試與靜態檢查

- [x] 5.1 跑 `make test`、`make lint`、`make smoke` 並全綠。
  - 行為合約：所有單元測試（含 race detector + count=1）通過；`golangci-lint run` 無 finding；`shadowdns --dry-run` 載入既有測試 config 無 error。
  - 驗證：三個 make target 各自 exit code 0；輸出無 FAIL/ERROR 字樣。

## 6. 本地 build deb 並部署到 bench-ns2

- [x] 6.1 用 release-shadowdns skill local-change mode build 並部署。
  - 行為合約：`shadowdns_0.0.0~eliminate-residual-name-concat-allocs_amd64.deb` 安裝在 bench-ns2，`dpkg -l shadowdns` 顯示 `0.0.0~eliminate-residual-name-concat-allocs`；`systemctl is-active shadowdns` 回 `active`；`/var/log/shadowdns/shadowdns.log` 在 30s 觀察窗內無 error/fatal/panic 關鍵字。
  - 執行：`make deb` with `VERSION=0.0.0-eliminate-residual-name-concat-allocs` → `scp` → `dpkg -i` → `systemctl restart` → `restart_and_watch.sh`。
  - 驗證：skill 的 `restart_and_watch.sh` 報告 `is-active: active` 且 error keywords = 0。

## 7. dnspyre 壓測 + pprof 比對

- [x] 7.1 從 bench-ns1 跑 dnspyre 與 30s pprof，產出比較報告。
  - 行為合約：產生 `.local/dnspyre/report/compare-baseline-recheck-vs-eliminate-residual-name-concat-allocs.md`，含 (1) QPS 對照表（baseline = `eliminate-client-addr-string-alloc` post-commit 31,176.1 QPS，或重抓 baseline-recheck）、(2) NXDOMAIN/REFUSED rate 變化（接受 ≤ 0.07pp 為 timing variance）、(3) pprof 中 `concatstring2` 在 `RewriteName` 與 `LookupKey` 子節點的下降量、(4) 達標判定（QPS +1~3%、行為等價）。
  - 執行：`ssh bench-ns1 "dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/cname-domains.txt > /tmp/dnspyre-cname-<TS>.txt 2>&1"` 配 `ssh bench-ns2 "curl -s -o /tmp/cpu-after-<TS>.pprof http://127.0.0.1:9153/debug/pprof/profile?seconds=30"`；報告依 `local-dnspyre-benchmark` skill 模板。
  - 驗證：報告檔案存在；報告明確標示是否達標、是否行為等價（NXDOMAIN/REFUSED rate 對照）。

- [x] 7.2 根據 7.1 結果更新 plan §4 Tier B4 與 §1.6 #3 實測收益欄位。
  - 行為合約：`.local/plans/2026-05-06-qps-regression-vs-bind9-fix.md` §4 Tier B4 與 §1.6 #3 段落附上實測 QPS Δ% 與行為等價驗證結論。
  - 驗證：grep 該檔案能找到 `eliminate-residual-name-concat-allocs` 字串與本次實測結果。
