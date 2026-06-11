## 1. dnsutil ECS 驗證基礎

- [x] 1.1 [P] 在 internal/dnsutil/ecs.go 實作 ECS option 分類函式（輸入 *dns.EDNS0_SUBNET，輸出 valid / opt-out / malformed 分類與 geo 查詢用 netip.Addr）與回應 echo option 建構函式（FAMILY、SOURCE PREFIX-LENGTH、ADDRESS 與查詢相同，SCOPE 由呼叫端指定），落實 design「ECS 驗證與 FORMERR 邊界行為」的 handler 層規則，malformed 檢查優先於 opt-out 分類：非零 query SCOPE 或 prefix 外非零位（prefix = 0 時整個 ADDRESS 都算 prefix 之外）→ malformed；通過 malformed 檢查且 SOURCE PREFIX-LENGTH 0（FAMILY 0/1/2 皆然，含 dig +subnet=0 的 FAMILY 0 形式）→ opt-out。分類函式須為 total function（default-deny）：非預期 FAMILY、超界 prefix 等列舉外輸入一律判 malformed，mask 前自行 bounds check，不依賴 library unpack 不變式。驗證：internal/dnsutil/ecs_test.go 以 table-driven 測試覆蓋 spec「Malformed ECS options are rejected with FORMERR when enabled」validation matrix 中「Rejected by = handler」（含兩列 malformed-beats-opt-out）與 valid/opt-out 的所有列，外加 default-deny 分支（如 FAMILY 3 的直接建構輸入判 malformed），go test ./internal/dnsutil 全綠

## 2. view.Matcher 雙位址解析

- [x] 2.1 [P] 將 view.Matcher.Resolve 簽名改為 (srcIP, geoIP netip.Addr)，實作 design「雙位址 Resolve：geo 規則用 ECS、ACL 規則用來源 IP」——country/ASN 規則以 geoIP 查 mmdb，any/IP/CIDR 規則以 srcIP 評估，落實 spec「Resolve client IP to a view using first-match semantics」「Evaluate country match via MaxMind GeoLite2-Country」「Evaluate ASN match via MaxMind GeoLite2-ASN」「Evaluate IP and CIDR rules without external lookup」的修訂語意（geo 位址永不滿足 IP/CIDR 規則）。同時把 internal/view/matcher_test.go 既有的全部單參數 m.Resolve(...) 呼叫點（約 30 處）遷移為雙參數——預設兩參數傳同一位址，逐案確認該測試斷言的語意屬 srcIP 還是 geoIP。驗證：matcher_test.go 新增雙位址分流案例（同一次 Resolve 中 CIDR 用 srcIP、country 用 geoIP；geoIP 落在 CIDR 內仍不命中），go test ./internal/view 全綠
- [x] 2.2 更新 Resolve 的兩個呼叫點：internal/server/handler.go 一般查詢路徑與 zone transfer 路徑（transfer 兩參數均傳來源 IP，AXFR/IXFR 不受 ECS 影響）。驗證：go build ./... 編譯通過，make test 既有測試全綠（雙參數傳相同位址時行為與改動前一致）

## 3. handler ECS 整合

- [x] 3.1 落實 design「ECS 解析併入 queryOpt 單次 OPT 迭代」：parseQueryOpt 既有 option 迴圈中以 first-wins 擷取第一個 dns.EDNS0_SUBNET 存入 queryOpt 新欄位，不新增第二次 opt.Option 迭代。驗證：internal/server/handler_ecs_test.go 斷言多個 ECS option 時僅第一個生效；code review 確認迴圈數不變
- [x] 3.2 在 ServeDNS 實作啟用時的 ECS 行為，位置依 design「ECS 驗證與 FORMERR 邊界行為」釘死：緊接 COOKIE 區塊之後、CHAOS／AXFR dispatch／addrFromRemote 之前。handler 可達的 malformed ECS（非零 query SCOPE、prefix 外非零位）→ 經 replyRcode 回 FORMERR（含 OPT、不含 ECS option，對含 AXFR 在內所有通過前置檢查的查詢一體適用）；valid ECS → geo 查詢位址改用 ECS ADDRESS 並設定回應 echo（SCOPE = SOURCE PREFIX-LENGTH，落實 design「SCOPE 寫回採 echo source prefix length」與 spec「Responses echo the ECS option with a scope equal to the source prefix length」，no-view 的 REFUSED 同樣 echo；更早出口與 panic-recovery、transfer 串流不 echo）；opt-out（含 FAMILY 0）→ 以來源 IP 選 view 且 echo 原 FAMILY、SCOPE 0，落實 spec「Opt-out ECS (source prefix length 0) is honored」；attachOPT 仿 respCookie 模式 append 回應 ECS option。驗證：handler_ecs_test.go 端到端斷言各情境的 rcode 與回應 option 內容
- [x] 3.3 撰寫 handler 端到端測試矩陣（以直接呼叫 ServeDNS 構造，因 EDNS0_SUBNET.pack 拒絕 wire 層違規形式，library 層拒絕的列不在 handler 測試範圍）：啟用/停用 × {無 ECS、valid IPv4、valid IPv6、opt-out FAMILY 1、opt-out FAMILY 0、非零 SCOPE、prefix 外非零位、prefix 0 + 非零 ADDRESS（malformed 優先於 opt-out，spec「Source prefix length 0 with non-zero address bits is malformed, not opt-out」scenario）} 的回應 ECS option 與 rcode；停用時非零 SCOPE 等 handler 可達 malformed 不觸發 FORMERR 且回應永不帶 ECS option；geo DB 查無 ECS 位址時 country 規則 no-match 且不以來源 IP 重查 geo 規則（spec「Valid ECS address drives geo rule evaluation when enabled」全數 scenario）；「偽造 ECS 不得命中 CIDR 受限 view」案例為防引數對調的 load-bearing 防線，必須包含且不可刪。驗證：go test -race ./internal/server 全綠

## 4. 啟用開關與啟動行為

- [x] 4.1 落實 design「啟用開關 --ecs-enable flag、預設 false」與 spec「ECS support is disabled by default and gated by the --ecs-enable flag」：cmd/shadowdns/main.go 在既有 CLI 選項 struct 註冊 --ecs-enable flag（預設 false），於 server.NewServer 之後設定 internal/server/server.go 的 Server 新導出布林欄位（不動 internal/config 的 OptionsBlock、不經 internal/server/build.go）；啟動時在 --dry-run 早退之前記一行 info log 標明 ECS 啟用／停用狀態（兩種狀態都記）；預設關閉時 handler 完全忽略查詢 ECS 且回應不帶 ECS option（行為與改動前位元級一致）。驗證：cmd/shadowdns/main_test.go 斷言 flag 存在與預設值，make smoke 通過，dry-run 輸出含 ECS 狀態行

## 5. 文件

- [x] 5.1 [P] 落實 design「文件更新：功能比較表 Planned → 支援」：README.md（功能比較表與計畫功能清單）、docs/index.md、docs/index.zh.md 的 ECS 列由 Planned 改為已支援並標注 opt-in（--ecs-enable，預設關閉）；docs/reference/cli.md 與 docs/reference/cli.zh.md 的 flag 參考表新增 --ecs-enable 列（預設值、SIGHUP 不重讀、行為摘要）。驗證：make docs-build 通過（該目標已內含 --strict），內容審閱五處文件一致

## 6. 整體驗證

- [x] 6.1 全套自動驗證：make test（race）、make lint、make smoke 全數通過。驗證：命令輸出零失敗
- [x] 6.2 請使用者手動 e2e 驗證：對測試主機部署後以 dig +subnet 確認啟用前後行為（停用時無 ECS echo；啟用時 echo 與 scope 正確、geo view 隨 ECS 位址切換；dig +subnet=0 的 FAMILY 0 opt-out 探測回 scope 0 而非 FORMERR）。驗證：使用者確認結果符合預期
