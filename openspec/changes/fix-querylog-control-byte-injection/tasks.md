## 1. 於 serveJSON 正規化 dns-json name

- [x] 1.1 在 `internal/doh/dnsjson.go` 的 `serveJSON`，於既有 `fqdn := dns.Fqdn(name)` 與 `dns.IsDomainName(fqdn)` 驗證通過之後、`req.SetQuestion(...)` 之前，以堆疊 `var wire [256]byte` 呼叫 `dns.PackDomainName(fqdn, wire[:], 0, nil, false)` 編碼，再以 `dns.UnpackDomainName(wire[:off], 0)` 解碼，取得與 wire 路徑逐字相同的 canonical presentation form 名稱。
- [x] 1.2 以 1.1 的 canonical 名稱（而非原始 `fqdn`）呼叫 `req.SetQuestion`，確保 `req.Question[0].Name` 與 wire 路徑一致；純 ASCII 名稱維持 identity 與 on-wire 大小寫。
- [x] 1.3 對 `PackDomainName`／`UnpackDomainName` 的 error 以防禦性方式處理：`if err != nil` 即以 `http.Error(w, ..., http.StatusBadRequest)` 回 400 並 return，不 dispatch、不回 500；於註解標明此分支對通過 `IsDomainName` 的合法輸入不可達（僅防禦）。

## 2. 回歸測試（僅測本專案自有的 serveJSON 行為）

- [x] 2.1 在 `internal/doh/dnsjson_test.go` 新增測試：送出 `name` 含控制位元組（URL 以 `%0A`/`%0D`/`%00`/`%09`/`%7F` 編碼）的 dns-json GET，斷言 `Question[0].Name` 為 canonical 跳脫形式（`0x0A` → `\010` 等），且逐位元組確認不含任何 `< 0x20` 或 `0x7f` 的原始位元組。
- [x] 2.2 新增跨 transport 一致性測試：同一 on-wire 名稱（含控制位元組）分別經 dns-json（`name=` percent-encoded）與 wire-format（`?dns=` base64url）兩路徑，前者解析 JSON 回應、後者以 `dns.Msg.Unpack` 解 wire 回應，斷言兩者 `Question[0].Name` 逐字相同（皆為 OUR 端點的 HTTP 行為，非測 miekg 本身）。
- [x] 2.3 既有 `TestJSON_NameCasePreserved`（`ExAmple.COM.` 大小寫保留）作為 identity/no-op 回歸守門，確認 round-trip 未破壞正常名稱。

## 3. 驗證與 Perf-Guard

- [x] 3.1 執行 `make test`（race detector）與 `make lint`，確認 `internal/doh/dnsjson_test.go` 全數通過、無 lint 問題，且行為符合 `doh-endpoint` spec 需求「application/dns-json queries are parsed from name and type parameters」底下新增的「control bytes ... escaped to match the wire path」與「the JSON path and the wire-format path yield the same question name」scenarios。
- [ ] 3.2 請使用者確認：本變更檔案為 `internal/**`（Perf-Guard 依檔案分類屬 must-run），惟僅動 dns-json GET 路徑、不觸及 wire UDP hot path；實作與 review chain 完成後仍依 Perf-Guard 在 ns2 跑 baseline → 部署 → 重測，確認 QPS 未下降 > 5% 且 p99 未上升 > 15%（wire benchmark 預期無位移）。
