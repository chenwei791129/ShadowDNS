## 0. 壓測 baseline（實作前置，使用者驗證）

- [x] 0.1 在開始任何 RRL 程式碼實作之前，使用 release-shadowdns skill 的 local-change 模式把當前 working tree（尚未含 RRL）建置部署至 ns2，再以 local-dnspyre-benchmark skill、用 ns1 作為 dnspyre client 對 ns2 取得 QPS baseline，報告存入專案 dnspyre 報告目錄。記錄使用的 dnspyre 參數（並發、查詢集、時長），group 6 必須沿用相同參數與部署方式以利 apples-to-apples 比較。驗證：報告檔產生並含 median/p99 QPS 與 throughput；參數已記錄供後續重用。

## 1. 配置解析：rate-limit 區塊（config-loader）

- [x] 1.1 為「解析 options 內的 rate-limit 區塊與相容性處理」撰寫 table-driven 測試 `TestParseRateLimit`（internal/config/ratelimit_test.go），鎖定 "Parse the rate-limit block in the options block"：全欄位解析、BIND 預設、per-category 預設回退到 responses-per-second、越界值 fatal、absent 與 all-zero 可區分。測試先紅。驗證：`go test ./internal/config -run TestParseRateLimit` 在未實作時失敗。
- [x] 1.2 新增 RateLimitConfig 型別與 OptionsBlock.RateLimit *RateLimitConfig 欄位，於 internal/config/ratelimit.go 實作巢狀 block 解析器並在 ParseOptions 接上 `rate-limit` case，交付「合法區塊解析出正確欄位與 BIND 預設」之行為使 1.1 轉綠。驗證：`go test ./internal/config -run TestParseRateLimit` 通過。
- [x] 1.3 交付「Warn and ignore unsupported rate-limit constructs」行為：`qps-scale` 子選項 warn 並忽略、`view` 內 `rate-limit` warn 並忽略且非 fatal。以 zap observer 撰寫 `TestRateLimitWarnIgnore` 斷言 warning 訊息與不中斷解析。驗證：`go test ./internal/config -run TestRateLimitWarnIgnore` 通過。

## 2. ratelimit 核心演算法（response-rate-limiting）

- [x] 2.1 [P] 交付「從 dns.Msg 推導回應類別」：實作 ratelimit.ClassifyResponse（internal/ratelimit/classify.go），對應 "Classify each response into a rate-limit category" 的 rcode×answer 對照表。以 `TestClassifyResponse` 逐列驗證 responses/nodata/nxdomains/errors 映射。驗證：`go test ./internal/ratelimit -run TestClassifyResponse` 通過。
- [x] 2.2 交付「token-bucket（credit）帳本演算法」：對應 "Account credit accounting over a rolling window"，實作 credit 帳戶（注入單調時鐘）、每秒回補、window×rate 上限、rate=0 停用、all-per-second 聚合閘。以 `TestCreditAccounting` 驗證超限判定與回補。驗證：`go test ./internal/ratelimit -run TestCreditAccounting` 通過。
- [x] 2.3 交付「bucket key 的 name imputation 規則」：對應 "Account key construction with name imputation"，responses 用 qname、nxdomains/nodata 用 zone origin、errors 用空 name、client 位址依 ipv4/ipv6-prefix-length 遮罩。以 `TestAccountKey` 驗證聚合與遮罩（含 random-subdomain 聚進單帳戶）。驗證：`go test ./internal/ratelimit -run TestAccountKey` 通過。
- [x] 2.4 交付「帳本資料結構與容量管理」：對應 "Account table capacity is bounded"，實作分片帳本（internal/ratelimit/table.go），達 max-table-size 時 LRU 淘汰、不阻塞熱路徑、總量介於 min/max-table-size。以 `TestTableCapacity` 驗證淘汰行為。驗證：`go test ./internal/ratelimit -run TestTableCapacity` 通過。
- [x] 2.5 整合為 Limiter.Decide(clientIP, category, name) Action，交付「exempt-clients 豁免名單」（對應 "Exempt clients bypass rate limiting" 的前置短路、不扣分）與「log-only 試運轉模式」（對應 "Log-only mode records but does not enforce" 的照算但放行並記事件）。以 `TestLimiterDecide` 驗證豁免與 log-only。驗證：`go test ./internal/ratelimit -run TestLimiterDecide` 通過。

## 3. 限流動作與 ResponseWriter 整合

- [x] 3.1 交付「slip 決策：drop 與 TC=1 截斷的交替」：對應 "Slip behavior chooses between dropping and truncating over-limit responses"，實作 slip=0 全 drop、slip=1 全截斷、slip=n 每第 n 個截斷其餘 drop；截斷清空 Answer/Ns/Extra（保留 OPT echo）、設 TC、保留 rcode 與 question。以 `TestSlipAction` 驗證序列與截斷形狀。驗證：`go test ./internal/ratelimit -run TestSlipAction` 通過。
- [x] 3.2 交付「在 ResponseWriter wrapper 的 WriteMsg 收斂點套用限流」與「僅對 UDP 套用速率限制，TCP 一律放行」：實作 ratelimit.ResponseWriter（internal/ratelimit/writer.go），對應 "Apply response rate limiting only to UDP responses"——TCP 直接委派零扣分、UDP 經 Decide、drop 不呼叫底層 WriteMsg、slip 寫截斷。以 `TestRateLimitWriter` 驗證三種路徑。驗證：`go test ./internal/ratelimit -run TestRateLimitWriter` 通過。

## 4. Server 接線

- [x] 4.1 交付端到端整合：於 internal/server/server.go 由 RateLimitConfig 建構 *ratelimit.Limiter（未配置為 nil），於 ServeDNS 進入點在 metrics wrapper 之內、真實 writer 之外掛上 ratelimit.ResponseWriter（wrapper 僅持有 *Limiter，clientIP 與推定 name 於 WriteMsg 當下自 RemoteAddr 與回應訊息導出，不在建構時注入）。以 `TestServerRateLimitWiring`（internal/server/handler_ratelimit_test.go）驗證 UDP 洪水被限、TCP 不被限、早期錯誤回應（FORMERR/NOTIMP）路徑亦走限流且不 panic、未配置時行為與現狀一致。驗證：`go test ./internal/server -run TestServerRateLimitWiring` 通過。
- [x] 4.2 交付 named.conf 端到端生效：於 cmd/shadowdns/main.go 將 RateLimitConfig 傳入 server 建構路徑。驗證：以含 `rate-limit { responses-per-second 10; }` 的設定執行 `make smoke`（--dry-run）啟動無錯。

## 5. Metrics 與文件

- [x] 5.1 [P] 交付「新增 RRL Prometheus 計數器」：對應 "Expose response rate limiting counters"，於 internal/metrics 新增依 category×action（dropped/slipped/exempted/logonly_would_drop）標籤之計數器並由限流路徑遞增；allowed-且未超限不遞增。以 `TestRateLimitMetrics` 驗證各標籤遞增。驗證：`go test ./internal/metrics -run TestRateLimitMetrics` 通過。
- [x] 5.2 [P] 交付文件一致性：更新 README.md 將 Response Rate Limiting 由 Planned 移除、特性對照表 ShadowDNS 欄改為 Yes，並載明 v1 範圍（僅 options 全域、qps-scale 不支援、referrals 不適用）。驗證：人工審閱 README 特性表與 Planned 段落已無 RRL 殘留且範圍描述正確。
- [x] 5.3 交付回歸保證：確認 RRL 變更未破壞既有行為且通過 lint。驗證：`make test` 與 `make lint` 退出碼皆為 0。

## 6. 效能回歸壓測（部署後，使用者驗證）

- [x] 6.1 交付部署：使用 release-shadowdns skill 的 local-change 模式，將含 RRL 的本地建置部署至 ns2。驗證：ns2 安裝版本標記為本 change 名稱、systemd 服務啟動無誤、`/var/log/shadowdns/shadowdns.log` 與 journal 無啟動錯誤。
- [x] 6.2 交付情境一（未啟用 RRL）QPS 非回歸：named.conf 不含 `rate-limit` 區塊，使用 local-dnspyre-benchmark skill 以 ns1 為 client、沿用 0.1 的 dnspyre 參數對 ns2 壓測並與 baseline 比較，驗證 wrapper-未掛載路徑零成本。驗證：median QPS ≥ baseline 的 95%（回歸 <5%、落在 run-to-run 變異內），報告存入 dnspyre 報告目錄。
- [x] 6.3 交付情境二（啟用 RRL 但上限極大不觸發限流）QPS 非回歸：named.conf 含 `rate-limit { responses-per-second 1000000; nxdomains-per-second 1000000; errors-per-second 1000000; }`，使限流熱路徑（classify + key 建構 + 帳本查詢）全程啟用但永不命中，以 ns1 為 client、沿用 0.1 參數對 ns2 壓測。驗證：median QPS ≥ baseline 的 95%，且壓測期間 RRL `dropped`/`slipped` 計數器維持為 0（確認確實未限流），報告存入 dnspyre 報告目錄。
