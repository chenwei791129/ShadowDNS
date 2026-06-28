## 1. JSON 查詢解析（internal/doh/dnsjson.go）

- [x] 1.1 在 internal/doh/dnsjson.go 實作 JSON 查詢參數解析，將 `name`（必填非空、正規化為結尾點 FQDN 但保留 on-wire 大小寫）與 `type`（省略時預設 `A`；mnemonic 大小寫不敏感；數值以 16-bit 範圍解析）組成設有 RD bit 的單一 question *dns.Msg 交給共用查詢路徑（design「JSON 查詢參數解析：name 必填且保留大小寫、type 大小寫不敏感與 0–65535 數值範圍」；spec「application/dns-json queries are parsed from name and type parameters」）。驗證：internal/doh/dnsjson_test.go 涵蓋 type 預設、大小寫不敏感對照表、name 大小寫保留與 RD=true 的案例通過。
- [x] 1.2 在 internal/doh/dnsjson.go 於 dispatch 前以 isZoneTransferQuery 判斷 AXFR/IXFR 並回 REFUSED（Status 5、空 Answer），不進 streaming transfer 路徑（design「AXFR/IXFR 在 JSON 路徑比照 wire 路徑一律 REFUSED」；spec「application/dns-json refuses zone-transfer query types」）。驗證：dnsjson_test.go 中 type=AXFR 回 HTTP 200 + Status 5 且無 stream 的案例通過。
- [x] 1.3 在 internal/doh/dnsjson.go 解析 `edns_client_subnet`（省略 prefix 時 IPv4 /24、IPv6 /56），先以 source prefix 遮罩 host bits 再建為 SCOPE 0 的 dns.EDNS0_SUBNET 並以 SetEdns0 注入，重用 internal/dnsutil/ecs.go 的 family/mask 邏輯（design「edns_client_subnet 遮罩 host bits 後注入 EDNS0_SUBNET option 重用既有 ECS 路徑」；spec「application/dns-json edns_client_subnet parameter injects an EDNS Client Subnet option」）。驗證：dnsjson_test.go 中 host-bit-set 值（如 198.51.100.5/24）經遮罩後不觸發 FORMERR、family 預設 prefix 表與注入 option 欄位斷言通過。

## 2. JSON 回應序列化（internal/doh/dnsjson.go）

- [x] 2.1 在 internal/doh/dnsjson.go 將 ServeDNS 產生的 *dns.Msg 序列化為 Google Public DNS schema：Status/TC/RD/RA/AD/CD、Question、Answer（`data` 以剝除 record header 的 presentation format 取得），RD 為真、CD 為偽，回應 OPT 含伺服器回填 ECS 時附 `<network>/<source-prefix>/<scope-prefix>` 的 edns_client_subnet（scope 為 source echo），並比照 wire 路徑設 Cache-Control: max-age（design「JSON 回應對齊 Google Public DNS schema：RD 為真、CD 為偽、data 剝除 header、Cache-Control 與 ECS scope echo」；spec「application/dns-json responses follow the Google Public DNS schema」）。驗證：dnsjson_test.go 對 A/AAAA/TXT/CNAME/MX/SOA 的 `data`、RD=true/CD=false、Cache-Control 與單筆 TXT example（欄位語意而非 byte-exact）斷言通過。

## 3. HTTP 格式協商與錯誤處理（internal/doh/server.go）

- [x] 3.1 在 internal/doh/server.go 的 GET handler 實作格式協商：帶 `?dns=` 一律走 wire（不論 Accept），否則 Accept 列出 application/dns-json 時走 JSON 路徑並設 Content-Type: application/dns-json；POST 維持 wire 且不加 Accept 分支（design「以 Accept header 進行格式協商，?dns= 優先，JSON 僅支援 GET」；spec「DoH endpoint serves application/dns-json over GET via Accept negotiation」）。驗證：dnsjson_test.go 涵蓋 JSON 選取、`?dns=` 優先、POST 維持 wire 的案例，且既有 wire-format 測試（internal/doh/server_test.go）維持綠燈。
- [x] 3.2 在 internal/doh/server.go 的 JSON 路徑實作錯誤語意：缺/空 `name`、`type` 非 mnemonic 且非 0–65535 數值、`edns_client_subnet` 無法解析回 HTTP 400；dispatch 後無捕獲回應回 HTTP 500；DNS 結果（含 REFUSED/NXDOMAIN/空答）一律 HTTP 200 並反映於 Status（design「malformed 查詢回 HTTP 400、內部無回應回 HTTP 500、DNS 層結果一律 HTTP 200」；spec「application/dns-json request errors return HTTP 400, internal failures return HTTP 500, and DNS-level results return HTTP 200」）。驗證：dnsjson_test.go 的 400（缺 name、type=65537/notatype）、500（空捕獲）、200/REFUSED 案例通過。
- [x] 3.3 在 JSON 路徑容忍並忽略 `cd`（不報錯且不設 req.CheckingDisabled，使回應 CD 維持為偽）、不理會 `do` 與 `ct` 且皆不報錯（design「cd 容忍忽略且不設 CD bit，do 與 ct 不支援」；spec「application/dns-json path tolerates cd and ignores do and ct」）。驗證：dnsjson_test.go 帶 `cd=1` 仍正常解析、不報錯且回應 CD=false 的案例通過。

## 4. 測試

- [x] 4.1 補齊 internal/doh/dnsjson_test.go，涵蓋查詢解析、zone-transfer 拒絕、ECS host-bit 遮罩與注入、JSON 序列化（含 RD/CD/Cache-Control/scope/各型別 data）、Accept 協商與 `?dns=` 優先、HTTP 400/500/200，並確認既有 internal/doh wire-format 測試不受影響。驗證：go test -race ./internal/doh/...（即 make test 範圍）全數通過。

## 5. 文件（中英手冊同步）

- [x] 5.1 [P] 更新 docs/guides/doh.md（英文）新增 application/dns-json 格式段落：參數（name/type/edns_client_subnet，cd 忽略）、`?dns=` 優先的協商規則、curl + jq 範例、回應 schema（含 scope echo 語意）、AXFR 拒絕、ECS 模擬驗證與未開 --ecs-enable 時的行為。驗證：make docs-build（--strict）通過。
- [x] 5.2 [P] 同步更新 docs/guides/doh.zh.md（繁中），內容與 5.1 對齊、連結指向 base .md 路徑並使用中文標題 anchor。驗證：make docs-build（--strict）通過且雙語 nav 一致。

## 6. 整合驗證與回歸

- [x] 6.1 執行 make lint 與 make test 確認無 lint 問題且測試全綠。驗證：兩項指令結束碼皆為 0。
- [x] 6.2 請使用者執行 Perf-Guard（本 change 改動 internal/doh hot-path 入口，屬 must-run）：baseline → 部署 → post-change 三段比較。驗證：.local/dnspyre/report/ 產出 perfguard 報告且判定為 PASS（無 QPS/p99 回歸）。
