## 1. Parser 實作（internal/config/zones.go + zones_test.go）

- [x] 1.1 頂層 zone 解析：依「頂層 zone 重用 parseZone、合成 view 重用既有型別」決策，讓 loadFile 的頂層 `zone` 分支呼叫既有 parseZone 並累積至 Config 的載入期暫存欄位，滿足 spec「Parse view and zone declarations from master.zones」的新行為——頂層 zone 與 view 內 zone 適用相同 zone-body 規則（僅 type master；相對 file 路徑沿用解析期語意，options 在前以 options.directory 為基底；缺 type／缺 file 的容忍、重複 zone 名稱的行為皆與 view 內一致）。先寫失敗測試再實作（tdd）：internal/config/zones_test.go 新增頂層 zone 可解析、`type slave;` 報相同 unsupported-type 錯誤、options 在前時相對路徑解析正確、缺 type／缺 file 與 view 內同樣被容忍四類案例，`go test ./internal/config/` 全綠。
- [x] 1.2 隱含 view 合成：依「在 LoadNamedConf 後處理階段合成 _default view」決策實作 spec「Synthesize implicit _default view for viewless configurations」——零 view 且至少一個頂層 zone 時合成 `View{Name: "_default", MatchClients: [AnyRule], Zones: 頂層 zone 依跨檔宣告順序}`，且後處理必須插在 warnShadowedViews 呼叫之前（維持「shadow 警告看到最終 view 清單」的不變式）；零 view 零 zone 時不合成、回傳空 view 清單；依「不保留 _default 為保留字」決策，顯式宣告 `view "_default"` 視為普通 view；重複的頂層 zone 名稱不 fatal、全部條目保留，但每個重複名稱輸出恰一條 Warn（含 zone 名稱、該名稱所有宣告的 source:line、「服務時以最後一筆宣告為準」說明）。先寫失敗測試再實作：zones_test.go 覆蓋上述情境（含雙 zone 宣告順序斷言、MatchClients 規則值與解析 `match-clients { any; };` 產物相同的型別斷言，以及以 zap observer 斷言重名 Warn 的數量與內容），`go test ./internal/config/` 全綠。
- [x] 1.3 混用偵測：依「混用偵測為宣告順序無關的致命錯誤」決策實作 spec「Reject mixing of top-level zones and view blocks」——任一 view 與任一頂層 zone 並存即回致命錯誤，訊息含第一個頂層 zone 的名稱、source 檔案路徑與行號。先寫失敗測試再實作：zones_test.go 覆蓋 zone 前 view 後、view 前 zone 後、view 與 zone 分屬 include 檔與根檔三種排列，皆斷言錯誤訊息含 source:line，`go test ./internal/config/` 全綠。

## 2. 回歸與整體驗證

- [x] 2.1 全套回歸：既有「全部 zone 在 view 內」設定的解析行為不變（既有測試零修改即通過），`make test`（race）、`make lint`、`make smoke` 全部通過。
- [x] 2.2 無 view 設定端到端 dry-run 驗證：比照 scripts/smoke.sh 的流程在臨時目錄手動組一份無 view、含頂層 zone 的 named.conf fixture——不含 `geoip-directory`、不產生 mmdb（前置 change geoip-optional 已使無 geo 規則設定免 GeoIP，合成的 `_default` 只含 any 規則），執行 shadowdns --dry-run，確認啟動摘要回報 views=1、`geoip_enabled=false`、zone 載入成功、無錯誤日誌；不修改 testdata/integration 既有 fixture。
- [x] 2.3 無 view 查詢路徑整合測試：在 test/integration 新增無 view 情境測試（沿用 geoip-optional task 3.1 建立的「跳過 GeoIP 的 helper 變體」，fixture 不含 geoip-directory 與 mmdb，新檔 test/integration/viewless_test.go），驗證合成 `_default` view 實際走通 matcher 與查詢路徑——對任意來源 IP 的 A 查詢取得權威回應（NOERROR + 正確 RR），補上 dry-run 在 NewServer 前退出而測不到的環節；`go test ./test/integration/` 全綠。

## 3. 手冊與文件

- [x] 3.1 [P] named.conf 手冊頁更新：docs/configuration/named-conf.md 與 docs/configuration/named-conf.zh.md 同步補上「無 view 形態」一節——頂層 zone 寫法（建議 options 區塊置於 zone 宣告之前）、隱含 `_default` view 的名稱與 match-clients any 等同行為、無 view 形態不需 `geoip-directory` 與 mmdb（連結 geoip 設定頁的條件式需求說明，由先行的 geoip-optional 落地）、view 與頂層 zone 混用為啟動錯誤的限制說明、頂層 zone 重名會輸出 Warn 且服務時以最後一筆為準，以及兩個已知表面差異：query log 每行會輸出 `view _default:`（BIND 無 view 設定的查詢日誌不含 view 子句，下游 log parser 需留意）、Prometheus metrics 的 view label 會出現 `_default`；範例僅用 RFC 2606 網域與 RFC 5737 IP，`make docs-build`（strict）通過。
- [x] 3.2 [P] 功能可用性表面檢視：檢查 docs/index.md 比較表與 README.md features/planned 清單是否有「views 必須／GeoIP 分流」相關描述需要反映「view 為選用、無 view 時 BIND 相容地以隱含 _default view 服務」；需要則更新（README 用英文），不需要則在 tasks 勾選時明確記錄「已檢視、無需變更」的結論。
