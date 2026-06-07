## 1. config-loader：logging{} 區塊解析

- [x] 1.1 [P] 於 internal/config/logging.go 實作 ParseLogging 與 QueryLogConfig（design Decision 1: logging{} 區塊解析進 internal/config，產出 QueryLogConfig），滿足 spec「Parse named.conf logging block for query logging」：production 形狀區塊（含 versions/size → RotationIgnored）解析正確、相對 file 路徑與 options directory join（與 zone file 路徑解析一致）、category queries 多 channel 取第一個 file channel 並 warning、未知 channel 參數與 category 以 warning 略過、括號不成對回報 fatal error 含行號。驗證：internal/config/logging_test.go 的 TestParseLogging 系列（含 production 形狀、相對路徑、多 channel、語法錯誤案例）通過 go test ./internal/config。
- [x] 1.2 在 ParseLogging 補齊停用情境判斷，滿足 spec「Disable query logging for unsupported logging configurations」：無 logging 區塊／無 category queries／null 與內建 channel／非 file channel（warning）／severity 嚴於 info（warning）皆回傳 nil 且不失敗；severity info、debug（含 level）、dynamic 視為啟用。驗證：TestParseLogging_DisableMatrix 依 spec 的 disable condition matrix 表逐列斷言。
- [x] 1.3 將 internal/config/zones.go 的 top-level dispatch 從「logging 區塊靜默跳過」改為呼叫 ParseLogging 並掛上 Config.QueryLog 欄位；更新 internal/config/zones_test.go 既有「logging block silently ignored」測試為新行為。驗證：go test ./internal/config 全綠。

## 2. internal/querylog：formatter 與寫入器

- [x] 2.1 [P] 建立 internal/querylog/querylog.go 的 Entry 值型別與 append-based 行 formatter（design Decision 2: internal/querylog 套件手寫 formatter，不走 zap），滿足 spec「Emit BIND9-compatible query log lines」：qname 去尾點保留大小寫（root 查詢輸出 .、presentation form escape 原樣輸出）、class/qtype 用 miekg/dns 助記符與 RFC 3597 fallback、@0x 合成 token 取 atomic 計數器小寫 hex 無補零（token 值以參數傳入 formatter，測試可注入）、行尾本機 IP 不含 port、段間單一空白。驗證：internal/querylog/querylog_test.go 以固定 Entry、time.FixedZone 固定時區的固定時間、注入的固定 token 斷言整行 byte-exact 等於預期字串（含 spec Example 的完整行）。
- [x] 2.2 formatter 支援 print 選項，滿足 spec「Honor print-time, print-category, and print-severity」：print-time 的 yes/local/iso8601/iso8601-utc/no 五種 layout、print-category 與 print-severity 的省略不留殘餘空白。驗證：TestFormat_PrintOptions 涵蓋 spec Example 表的四種組合與 iso8601 兩變體。
- [x] 2.3 實作 flags 欄位輸出，滿足 spec「Render the query flags field as the supported BIND9 subset」：+/-（RD）、E(version)、T、D、C、K（COOKIE option 存在）依序無分隔輸出，永不輸出 S 與 V。驗證：TestFormat_Flags 以 spec Example 的 flag field values 表逐列斷言。
- [x] 2.4 實作 Logger 寫入器：sync.Pool buffer、單次 Write 落檔、以 logging.OpenReopenSink 開檔（design Decision 3: 同步單次 Write，重用 ReopenSink，不做非同步批次），nil Logger 的 Log 呼叫安全短路。驗證：TestLogger_WritesLine 確認落檔內容；BenchmarkLog 以 in-memory sink（不落實體檔案）量測 Log 全路徑，go test -bench=BenchmarkLog -benchmem 確認穩態 0 allocs/op（含時間戳與 hex token 寫入，皆不豁免）。

## 3. dns-server：hot path 發出點

- [x] 3.1 Server 結構新增 QueryLog 可選欄位，並在 ServeDNS 於 view 解析成功後、zone 匹配前發出 query log（design Decision 4: 發出點在 view 解析成功之後，與 BIND 語意一致），滿足 spec「Log emission point matches BIND9 semantics」的 in-view 部分：zone 外 REFUSED 仍記錄；no-view、CHAOS、FORMERR、NOTIMP 不記錄。驗證：internal/server/handler_test.go 的 TestQueryLog_EmissionPoints 對各路徑斷言行數與內容。
- [x] 3.2 handleTransfer 於內部 view 解析成功後發出 query log（allow-transfer ACL REFUSED 的請求在 view 解析前返回、不記錄，維持既有 ACL 先於 view 解析的順序），qtype 渲染為 AXFR/IXFR 且 flags 含 T（TCP 時；IXFR over UDP 無 T）。驗證：TestQueryLog_Transfer 斷言 AXFR 行的 query 段與 flags，以及 ACL REFUSED 不產生行。
- [x] 3.3 確認 QueryLog 為 nil 時 hot path 行為與現行完全相同，滿足 spec「Disabled query logging leaves DNS behavior unchanged」。驗證：既有 internal/server 全部測試在未設定 QueryLog 下不需修改即通過（go test ./internal/server）。

## 4. daemon 佈線（cmd/shadowdns/main.go）

- [x] 4.1 啟動流程：Config.QueryLog 非 nil 時建立 querylog.Logger 並注入 Server；開檔失敗即中止啟動並回報路徑與錯誤，滿足 spec「Startup fails loudly when the query log file cannot be opened」。驗證：cmd/shadowdns 測試以不存在目錄的路徑斷言啟動失敗訊息。
- [x] 4.2 SIGUSR1 handler 擴充為同時 reopen main log 與 queries log 兩個 sink（design Decision 5: SIGUSR1 同時 reopen 兩個 sink，rotation warning 與 dry-run 摘要），query log 啟用而 --log-file 未設時仍安裝 handler，單邊失敗不影響另一邊，滿足 spec「Query log file participates in SIGUSR1 reopen」。驗證：TestSIGUSR1_ReopensQueryLog 模擬 rename 後發訊號斷言新檔案續寫。
- [x] 4.3 RotationIgnored 為 true 時啟動（含 --dry-run）印一則 warning 告知不實作 BIND 內建 rotation，無 versions/size 時不印，滿足 spec「Warn at startup when BIND rotation parameters are ignored」。驗證：測試斷言 warning 恰好一則／零則。
- [x] 4.4 --dry-run 摘要納入 query log 狀態（啟用：解析後路徑與 print 選項生效值；停用：原因，涵蓋全部五種停用情境字串），滿足 spec「Dry-run summary reports query log status」。驗證：TestDryRun_QueryLogSummary 斷言啟用與停用兩種輸出（smoke fixture 不含 logging{}，enabled 分支由本單元測試覆蓋）。
- [x] 4.5 確認 SIGHUP reload 行為滿足 spec「SIGHUP reload does not re-apply logging configuration」：reload 重跑 LoadNamedConf 後新解析的 QueryLogConfig 被丟棄、既有 sink fd 不變；reload 的 named.conf 含 logging{} 語法錯誤時該次 reload 失敗並保留舊狀態（與 options{} 解析錯誤語意一致）。驗證：TestReload_QueryLogUntouched 斷言 reload 前後 query log 續寫同一檔案、變更的 logging{} 不生效。

## 5. 文件與整體驗證

- [x] 5.1 更新使用者可見文件：README.md 的 feature 對照表將 query logging 由 Planned 改為已支援（英文）；packaging/named.conf.example 補上 logging{} 區塊範例（channel + category queries）。驗證：內容審閱——README 描述與 spec 行為一致、example 可被 ParseLogging 解析為啟用。
- [x] 5.2 全套品質關卡通過：make test（含 race）、make lint、make smoke 全綠。
- [x] 5.3 請使用者以 release-shadowdns skill 的 local-change mode 部署到 bench-ns2，在 named.conf 加入 production 形狀的 logging{} 區塊後重啟（log 路徑置於 /var/log/shadowdns/ 下，或先以 systemd override 擴充 ReadWritePaths），確認 queries log 逐行格式、startup rotation warning，並以 logrotate + SIGUSR1 驗證 rotate 後續寫不中斷。
