## 1. internal/cookie 套件

- [x] 1.1 新增 SipHash-2-4 相依（design 決策四：SipHash-2-4 採用 github.com/dchest/siphash 相依）：`go get github.com/dchest/siphash` 後 `go.mod`/`go.sum` 含該模組且 `make build` 成功
- [x] 1.2 實作 `internal/cookie` 套件的 Generate（design 決策三：server cookie 採 RFC 9018 格式、每次重新計算、不做驗證）：128-bit secret 於建構時注入（與 design 介面契約一致），Generate 輸入 client cookie bytes、client IP（netip.Addr）與 Unix timestamp，輸出 24-byte 完整 cookie（client cookie echo + version 1 + reserved 0 + timestamp + SipHash-2-4 hash），滿足 spec「Server cookie uses the RFC 9018 interoperable format」；SipHash key 拆分依 design 決策四的 little-endian 約束。驗證：`go test ./internal/cookie` 通過 RFC 9018 Appendix A 的 IPv4 與 IPv6 測試向量，以及 byte layout 斷言（byte 0 = 0x01、bytes 1-3 = 0、bytes 4-7 = big-endian timestamp）
- [x] 1.3 為 Generate 建立微基準 BenchmarkGenerate：`go test -bench=Generate -benchmem ./internal/cookie` 可執行並輸出 ns/op 與 allocs/op，數據記錄於 change 目錄備查

## 2. handler — OPT 解析統一與 OPT echo

- [x] 2.1 在 `internal/server/handler.go` 實作 queryOpt struct 單次解析（design 決策五：OPT 解析統一為 queryOpt struct 單次解析），解析位置在 ServeDNS 最前端、早於 opcode 與 question-count 檢查；多個 COOKIE option 只取第一個（RFC 7873 §5.2 first-wins）；`buildQueryEntry` 改收解析結果、不再自行迭代 `opt.Option`，滿足 spec「Query log output is unchanged by cookie processing」（K 旗標照舊、永不輸出 V）。驗證：`make test` 全綠且 `internal/querylog` 既有測試零修改
- [x] 2.2 實作 OPT echo（design 決策一：OPT echo 集中於單一回應組裝點，回應 OPT 廣告 1232 bytes），滿足 spec「Responses echo an EDNS0 OPT record when the query carries one」：涵蓋全部四個回應組裝點——`replyWithAnswer`、`replyRcode`（含 handleTransfer 的 pre-transfer REFUSED，queryOpt 穿線傳入）、`negativeReply`（NXDOMAIN/NODATA）、panic-recovery SERVFAIL（就地以 req.IsEdns0() 判斷）。帶 OPT 的查詢回應 version 0、UDPSize 1232 的 OPT，無 OPT 查詢回應不含 OPT；UDP 與 TCP 行為一致（滿足 spec scenario「OPT echo over TCP」，TCP 回應不截斷維持現狀）。驗證：`internal/server` 新增整合測試斷言成功、NXDOMAIN/NODATA、錯誤 rcode（CHAOS REFUSED）、被拒 AXFR 四種情境的 OPT 存在性與欄位值，無 OPT 查詢的 OPT 不存在，以及同一 EDNS 查詢經 TCP 的 OPT 欄位與 UDP 一致
- [x] 2.3 實作 BADVERS（design 決策二：EDNS 版本 > 0 回 BADVERS），滿足 spec「Unsupported EDNS version receives BADVERS」：version > 0 的查詢回 BADVERS extended rcode、OPT version 欄位為 0、echo question section、Answer 為空、優先於 cookie 處理（回應不含 COOKIE option）；注意 miekg/dns 對 Rcode > 15 且無 OPT 的訊息 Pack 會回 ErrExtendedRcode 導致回應靜默不送出，BADVERS 必須經 attachOPT 組裝。驗證：整合測試以 version 1 查詢斷言 extended rcode、question section 存在與空 Answer；以 version 1 + 7-byte COOKIE 查詢斷言回 BADVERS 而非 FORMERR 且無 COOKIE option
- [x] 2.4 確保截斷保留 OPT，滿足 spec「OPT record persists through UDP truncation and counts toward the size budget」：`truncateForUDP` 打包尺寸含 OPT、僅丟 Answer RR、截斷後回應仍含 OPT 且 TC=1。驗證：整合測試構造超出 buffer 的 EDNS UDP 回應，斷言最終封包 ≤ 預算、TC=1、OPT 仍在

## 3. handler — cookie 整合

- [x] 3.1 Server 持有 cookie secret（design 決策六：secret 持有於 Server struct，SIGHUP 不輪替），滿足 spec「Server secret is generated at startup and held in memory only」：NewServer 內以 crypto/rand 產生 16 bytes（Go 1.24+ 不回傳錯誤，無錯誤處理分支、簽名不變），secret 不進 reload 快照。驗證：單元測試斷言 SIGHUP 重載前後，同一 client cookie + IP + 固定 timestamp 產出的 hash 段一致；並斷言兩個獨立 Server 實例（模擬重啟，滿足 spec scenario「Secret changes across restarts」）對同一輸入產出不同 hash 段，且帶舊實例 server cookie 的查詢仍正常回答並獲得新 server cookie
- [x] 3.2 帶 cookie 查詢的回應組裝，滿足 spec「Answer queries carrying a well-formed COOKIE option with a complete server cookie」：8-byte client-only 與 16–40-byte full cookie 查詢都獲得 24-byte COOKIE option（client cookie 原樣 echo + 新算 server cookie）；兩個 COOKIE option 時 first-wins、不回 FORMERR、回應恰含一個 COOKIE option。驗證：整合測試斷言 COOKIE option 長度、client cookie echo、雙 COOKIE option 情境，以及 rcode 與 Answer section 和「帶 OPT 但無 COOKIE」的同題查詢相等
- [x] 3.3 畸形 COOKIE 回 FORMERR，滿足 spec「Malformed COOKIE option is rejected with FORMERR」：長度判斷以 hex decode 後的 raw byte 數為準（miekg/dns 的 Cookie 欄位是 hex 字串、len 為 raw 的兩倍，見 design 決策五），raw 長度非 8 且非 16–40 回 FORMERR、回應含 OPT、不含 COOKIE option。驗證：表格驅動測試覆蓋 raw 長度 7/8/9/15/16/40/41 邊界
- [x] 3.4 無 cookie 行為迴歸保護，滿足 spec「Queries without a COOKIE option are answered unchanged」：不帶 COOKIE option 的查詢（含帶 EDNS 與不帶 EDNS）回應不含 COOKIE option、永不出現 BADCOOKIE。驗證：整合測試斷言回應無 COOKIE option，且全套既有 handler 測試在僅有 OPT echo 差異下通過

## 4. 效能驗證（design 決策七：效能驗收採前後壓測＋微基準雙軌）

- [x] 4.1 `internal/server` 新增 `internal/server/handler_bench_test.go` 三路徑微基準（無 EDNS / 有 EDNS 無 cookie / 有 cookie；design 決策七：效能驗收採前後壓測＋微基準雙軌）：`go test -bench=ServeDNS -benchmem ./internal/server` 可執行，實作前後的 ns/op 與 allocs/op 對照數據記錄於 change 目錄，並監控配置數變化。微基準不設百分比門檻——OPT echo 是修復 RFC 6891 合規缺口的必要新工作，handler 孤立量測必然高於系統層差異；< 2% 的 QPS 門檻歸屬 4.3/4.4 的跨網路系統層壓測（spec「Cookie and OPT processing meet the performance budget」的正式定義）。本機 loopback A/B 壓測已先行驗證系統層 QPS 差異在 run-to-run 噪音內（≈ 0%）
- [x] 4.2 [P] 查證 dnspyre 是否支援發送 COOKIE option（design.md Open Questions）：查 dnspyre --help 與官方文件，結論記錄於 change 目錄；若不支援，註明 SipHash 路徑由 4.1 微基準單獨把關
- [x] 4.3 部署前 baseline 壓測（滿足 spec「Cookie and OPT processing meet the performance budget」的基準取得）：於獨立 client 主機上執行 dnspyre，跨網路對 test nameserver 現裝版本施測，參數與報告格式沿用既定 dnspyre 壓測流程（含 EDNS 參數），多輪實測記錄 QPS/p50/p99，並以多輪 p99 的 max − min 記錄 run-to-run 雜訊幅度，產出 baseline 報告
- [x] 4.4 以本地建置的 deb 套件部署本變更至 test nameserver，啟動日誌無錯誤後，於 client 主機以與 4.3 完全相同的 dnspyre 參數複測，比對結果（滿足 spec scenario「Before/after cross-network load test stays within budget」）：QPS 退化 < 2% 且 p99 ≤ baseline p99 + 雜訊幅度即過關；未達標則回滾至最新 GitHub release 並回到優化迭代

## 5. 文件與收尾

- [x] 5.1 [P] 更新 `README.md`（英文）：DNS Cookies 由 Planned 清單移至已支援功能描述（註明 RFC 7873 answer-only、RFC 9018 server cookie、無強制模式），feature comparison 表 ShadowDNS 欄由 Planned 改為 Yes。驗證：內容審閱與表格一致性檢查
- [x] 5.2 全套驗證通過：`make test`（race detector）、`make lint`、`make smoke` 三者皆綠
