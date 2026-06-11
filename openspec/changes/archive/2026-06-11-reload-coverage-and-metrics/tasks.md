<!--
Each task description MUST state:
- the behavior or contract being delivered (what is observably true when the
  task is complete), and
- the verification target that proves completion (test, CLI invocation,
  analyzer check, manual assertion, or content review).

File paths are supporting context for locating the work, never the task
itself. "Edit file X" is not a valid task — it is missing both behavior and
verification.
-->

## 1. 新增 Reload Metrics 宣告與 GeoIP gauge 差分更新（`internal/metrics/metrics.go`）（reload metrics 宣告位置）（GeoIP DB 重載的失敗語義與 gauge 殘留清理）

- [x] [P] 1.1 在 `Metrics` 結構中新增 `reloadTotal *prometheus.CounterVec`（labels: `result`）與 `lastReloadSuccessTimestamp prometheus.Gauge` 兩個欄位（unexported，與既有欄位命名慣例一致），滿足「Reload outcome is tracked by a total counter」與「Last-reload-success timestamp is exposed as a gauge」及「Last-reload-success timestamp is updated on successful reload」的宣告合約（metric 合約）。驗證方式：`go test ./internal/metrics/ -run TestReloadMetrics` 確認兩個 metric 的名稱、label、初始值符合 spec。

- [x] 1.2 在 `New()` 中建立並向 custom registry 註冊 `shadowdns_reload_total`（CounterVec, label `result`）與 `shadowdns_config_last_reload_success_timestamp_seconds`（Gauge, no labels；`_seconds` 單位後綴比照 Prometheus 生態慣例），並以 `WithLabelValues("success")` / `WithLabelValues("failure")` 預初始化 counter 的兩個 label 組合，使 `GET /metrics` 從啟動起即輸出兩條值為 0 的 series（「Counter is present at startup with value zero」scenario）。驗證方式：`go test ./internal/metrics/ -run TestReloadMetrics` 斷言未做任何觀測前，兩條 counter series 與 gauge 均出現於 `Gather()` 結果且值為 0。

- [x] 1.3 新增 `RecordReload(result string)` 方法（呼叫 `m.reloadTotal.WithLabelValues(result).Inc()`）與 `SetLastReloadSuccess(t time.Time)` 方法（呼叫 `m.lastReloadSuccessTimestamp.Set(float64(t.Unix()))`）作為 `reload()` 的 metric 合約呼叫入口。兩個方法 MUST 以 nil receiver no-op 實作（`if m == nil { return }`，與 `querylog.Logger.Log` 的 nil-safe 模式一致），使 metrics 停用（`srv.Metrics == nil`）時呼叫端不需特判（`reload()` 取得 `*metrics.Metrics` 的方式）。驗證方式：`go test ./internal/metrics/ -run TestReloadMetrics` 呼叫這兩個方法並斷言 counter/gauge 值正確遞增/更新，且 nil receiver 呼叫不 panic。

- [x] 1.4 將 `SetGeoIPInfo` 改為差分更新並補上 nil receiver guard：在 `Metrics` 結構新增 `prevGeoIPLabels map[string]string`（database → 上次設定的 build_time），`SetGeoIPInfo` 設定新 `(database, build_time)` 組合為 1 後，對 database 相同但 build_time 不同的舊組合呼叫 `DeleteLabelValues`，保證任何時刻每個 database label 至多一條 build_time series（「GeoIP db_info gauge updated after successful reload」scenario 的 stale-label 條款；模式比照 `SetZoneCounts` 的 prevZoneViews）。方法開頭 MUST 加入 `if m == nil { return }`——reload 路徑會在 metrics 停用時呼叫它，現行實作沒有 guard 會 panic（「`reload()` 取得 `*metrics.Metrics` 的方式」設計決策的 nil-safe 清單）。驗證方式：`go test ./internal/metrics/ -run TestSetGeoIPInfo` 連續以不同 build_time 呼叫兩次後，`Gather()` 結果中每個 database 僅存在最新 build_time 的 series；nil receiver 呼叫不 panic。

## 2. 擴充 `Server` 結構的 RRL 與 QueryLog 欄位為原子指標（`internal/server/server.go`）（Server.RateLimiter 與 Server.QueryLog 的執行緒安全替換）

- [x] [P] 2.1 將 `Server.RateLimiter` 欄位型別從 `*ratelimit.Limiter` 改為 `atomic.Pointer[ratelimit.Limiter]`，實作「Server.RateLimiter 與 Server.QueryLog 的執行緒安全替換」設計決策，使 handler hot path 讀取（`.Load()`）與 reload 寫入（`.Store()`）之間無 torn read，滿足「Rate limiter is rebuilt atomically on SIGHUP」需求中的原子性。同步更新 `cmd/shadowdns/main.go` 啟動時的賦值為 `.Store()`。驗證方式：`go build ./...` 通過，且 `go test ./internal/server/ -run TestRateLimiter` 確認原子讀寫行為。

- [x] 2.2 更新 `internal/server/handler.go` 中取用 `srv.RateLimiter` 的所有 call site，從直接欄位存取改為單次 `.Load()` 後使用區域變數，確保 DNS query handler 讀取原子指標而非裸指標。驗證方式：`go build ./...` 通過；`go test ./internal/server/ -run TestRateLimiter` 確認 nil limiter（`Load()` 返回 nil）時查詢不被限流。

- [x] 2.3 將 `Server.QueryLog` 欄位型別從 `*querylog.Logger` 改為 `atomic.Pointer[querylog.Logger]`，確保 reload goroutine（SIGHUP handler）替換 logger 時與 DNS handler 並發讀取之間不產生 data race（與 `Server.state` 和 `Server.RateLimiter` 的模式一致）。同步更新 `internal/server/handler.go` 中所有讀取 `srv.QueryLog` 的 call site 改為 `.Load()`，以及 `cmd/shadowdns/main.go` 啟動時的賦值 `srv.QueryLog = qlLogger` 改為 `srv.QueryLog.Store(qlLogger)`。（註：SIGUSR1 handler 持有的是 `*logging.ReopenSink` 而非 `srv.QueryLog`，其改寫屬 task 6.4。）驗證方式：`go build ./...` 通過；`make test`（含 race detector）無 DATA RACE 輸出。

## 3. 擴充 `reload()` 函式簽章、步驟順序與 metrics 植入（`cmd/shadowdns/main.go`）（`reload()` 函式新簽章（概念性））（Reload 步驟順序：所有 fallible 步驟先於 SwapState）

- [x] 3.1 引入 `geoipRuntime` holder 結構（`country`/`asn` current 欄位＋`prevCountry`/`prevASN` 延遲關閉槽，見「舊 GeoIP DB handle 的延遲一代關閉（geoipRuntime holder）」設計決策）並擴充 `reload()` 參數列表為 design.md「`reload()` 函式新簽章（概念性）」所定義的合約：`geo *geoipRuntime` 與 `qlState *atomic.Pointer[queryLogState]`；metrics 經由既有的 `srv.Metrics` 取得，不新增參數（「`reload()` 取得 `*metrics.Metrics` 的方式」設計決策）。`ctx` 參數由 SIGHUP dispatch goroutine 傳入 task 8.1 派生的**子 context**（而非父 ctx）——reload 衍生的 NOTIFY goroutine 隨 shutdown 序列一併取消（「SIGHUP goroutine 的 join 與 shutdown 清理順序」設計決策）。`run()` 啟動時初始化 `geo` 的 current 欄位（goroutine 啟動前，無需 atomic），SIGHUP handler 呼叫點同步更新。驗證方式：`go build ./...` 通過；`make smoke` 通過。

- [x] 3.2 依「Reload 步驟順序」設計決策重排 `reload()`：步驟 0 先關閉上一代延遲關閉的 GeoIP DB（`geo.prev*`，nil 則略過）；所有 fallible 步驟（named.conf parse → shadowdns config load → GeoIP open → BuildState → limiter 建構 → query-log sink open）一律在 `srv.SwapState` 之前完成；swap 之後僅剩不會失敗的安裝步驟（Store limiter/logger/qlState、GeoIP handle 輪替進 prev 槽——不立即關閉、更新 gauge、Close 被取代的舊 query-log sink、ephemeral clear、NOTIFY、metrics），實作「All fallible reload steps precede the state swap」需求。任一 fallible 步驟失敗時，清理該次已建立的新資源（已開的新 GeoIP DB、新 sink）後返回 error，舊狀態全數不動。驗證方式：`go test ./cmd/shadowdns/ -run TestReload` 斷言各失敗情境下 `srv.CurrentState()`、`srv.RateLimiter.Load()`、`srv.QueryLog.Load()` 均維持 reload 前的值。

- [x] 3.3 在 `reload()` 的成功返回路徑（`return nil` 前）呼叫 `srv.Metrics.RecordReload("success")` 與 `srv.Metrics.SetLastReloadSuccess(time.Now())`；在每個 `return err` 路徑（parse 失敗、GeoIP 失敗、zone build 失敗、limiter 建構失敗、query-log sink 失敗）呼叫 `srv.Metrics.RecordReload("failure")`（建議以單一出口或 defer 確保每次 SIGHUP 恰好遞增一次），實作「Reload metrics are emitted on every reload attempt」與「Last-reload-success timestamp is updated on successful reload」需求（metric 合約）。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadMetrics` 斷言成功 reload 後 success counter +1 且 gauge 更新；失敗路徑下 failure counter +1 且 success counter 與 gauge 不變。

- [x] 3.4 在 `run()` 的 metrics 啟用區塊（建立 `m` 之後、metrics HTTP server 啟動之前）呼叫 `m.SetLastReloadSuccess(time.Now())`，將 last-reload-success gauge 初始化為啟動初始載入成功的時刻——比照 Prometheus 自身的啟動行為，使 `time() - <gauge>` 型 staleness 告警在從不 reload 的主機上不誤報，實作「Last-reload-success timestamp is exposed as a gauge」需求的 startup-initialisation 條款（「Gauge is initialised at startup」scenario；reload metrics 宣告位置設計決策）。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadMetrics` 子測試以 `ReadyCh` 同步後、未送任何 SIGHUP 前，斷言 gauge 已為啟動時刻（非 0、與當下時間差 < 2s）。

## 4. GeoIP mmdb 重載（`cmd/shadowdns/main.go`）（GeoIP DB 重載的失敗語義與 gauge 殘留清理）（舊 GeoIP DB handle 的延遲一代關閉（geoipRuntime holder））（GeoIP 重載合約）

- [x] 4.1 在 `reload()` 中，於 `server.BuildState` 前呼叫 `view.LoadGeoIP(cfg.Options.GeoIPDirectory, logger)` 取得新的 `newCountry *view.CountryDB` 與 `newASN *view.ASNDB`，並以新 DB 建構 state，實作「GeoIP databases are reloaded on SIGHUP」需求。呼叫前 MUST 比照啟動路徑驗證 `cfg.Options.GeoIPDirectory` 非空——空值回明確的設定錯誤（spec 對 empty `geoip-directory` 的條款），而非相對路徑開檔的混淆錯誤。若 `LoadGeoIP` 返回 error，立即 `return fmt.Errorf("reloading GeoIP: %w", err)`（舊 DB 保留、failure counter 由 task 3.3 機制遞增），實作「GeoIP reload failure preserves existing state」scenario。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadGeoIP` 斷言成功 reload 後 `geo.country` 與 `geo.asn` 指向新 DB 物件；GeoIP 目錄不可用時 reload 返回 error 且舊 DB 保留；`geoip-directory` 為空時 reload 返回設定錯誤。

- [x] 4.2 在 `reload()` 成功的安裝階段（SwapState 後）執行 GeoIP handle 輪替：`geo.prev* = geo.current*`、`geo.current* = new`，**不關閉**剛被換下的 DB（「舊 GeoIP DB handle 的延遲一代關閉（geoipRuntime holder）」設計決策——swap 後立即關閉是 use-after-munmap，最壞情況不可 recover 的 SIGSEGV）；呼叫 `srv.Metrics.SetGeoIPInfo(map[string]uint{"country": newCountry.Metadata().BuildEpoch, "asn": newASN.Metadata().BuildEpoch})` 更新 `shadowdns_geoip_db_info` gauge（task 1.4 的差分刪除保證舊 build_time series 被移除；nil receiver no-op 保證 metrics 停用時不 panic），實作「GeoIP db_info gauge updated after successful reload」與「Superseded GeoIP handles are closed deferred, never immediately after the swap」兩個 scenario（「GeoIP databases are not reloaded」限制性需求已由本 change 移除，重載為新的必要行為）。`run()` 中現有的 `defer country.Close()` / `defer asn.Close()` 改為對 `geo` 全部存活 handle（current＋prev）的關閉，其與 reload goroutine join 的順序屬 task 8.1。注意 early-return 陷阱：`LoadGeoIP` 之後、`Serve` 之前的提前返回路徑（BuildState／limiter 建構／query-log open／listen-address 解析／BindMany 失敗、`--dry-run`）也 MUST 關閉 handle——建議以註冊於 `LoadGeoIP` 後、關閉 `geo` 全部存活 handle 的單一 defer 統一收尾（defer 在 body 的 join 序列之後執行，順序天然安全，見 design 決策 12），不要只在 Serve 返回後顯式關閉。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadGeoIP` 斷言 gauge 值更新為新 DB 的 build_time 且無舊 build_time series；reload 後舊 handle 仍可 `Lookup`（未被關閉）且位於 prev 槽；第二次 reload 後第一代 handle 已被關閉；`make test` 無 data race。

- [x] 4.3 同步更新與舊行為綁定的兩處註解（「既有註解同步」設計決策）：`reload()` doc comment 中「GeoIP databases are reused from startup」改為描述每次 SIGHUP 重開 mmdb 並延遲一代關閉的新行為；`run()` metrics 區塊中「databases are not reloaded on SIGHUP, so these values remain stable」改為說明 gauge 會隨成功 reload 更新。驗證方式：content review——`grep -n "reused from startup\|not reloaded on SIGHUP" cmd/shadowdns/main.go` 無殘留舊敘述。

## 5. RRL Limiter SIGHUP 重建（`cmd/shadowdns/main.go`）（RRL limiter 重建時是否保留 credit table）（RRL Limiter 重建合約）

- [x] 5.1 在 `reload()` 的 fallible 階段（SwapState 前）依 `cfg.Options.RateLimit` 呼叫 `ratelimit.NewLimiter(cfg.Options.RateLimit)` 建構新 limiter，並**僅在 `srv.Metrics != nil` 時**以 `newLimiter.SetRecorder(srv.Metrics)` 沿用同一 recorder（「RRL metrics recorder is preserved across limiter rebuild」scenario）——此 guard 不可省略：把 nil 的 `*metrics.Metrics` 塞進 `Recorder` interface 會形成 typed-nil，`Limiter.record` 的 `l.metrics != nil` 檢查擋不住，之後每次 RRL 決策都會 panic（「`reload()` 取得 `*metrics.Metrics` 的方式」設計決策）。實作「Rate limiter is rebuilt atomically on SIGHUP」需求；依「RRL limiter 重建時是否保留 credit table」設計決策重置 credit table（不保留）。若 `NewLimiter` 返回 error，以 `return fmt.Errorf("rebuilding rate limiter: %w", err)` 失敗 reload。若 `cfg.Options.RateLimit` 為 nil，新 limiter 為 nil（停用 RRL）。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadRateLimiter` 斷言新 limiter 設定生效且 credit table 為空；config 為 nil 時 `Load()` 返回 nil；建構失敗時 reload 返回 error 且舊 limiter 不變。

- [x] 5.2 在安裝階段透過 `srv.RateLimiter.Store(newLimiter)` 原子安裝新 limiter（或 nil）。舊 limiter 指標在 Store 後自然被 GC 回收（不需顯式 close）。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadRateLimiter` 斷言 `srv.RateLimiter.Load()` 返回新 limiter，且 RRL metrics recorder 為同一個 `*metrics.Metrics` 實例（`shadowdns_dns_rate_limit_total` 持續遞增無中斷）。

## 6. Query Log SIGHUP 重新套用與 SIGUSR1 整合（`cmd/shadowdns/main.go`）（Query log 目前生效設定與 sink 的追蹤（queryLogState holder））（SIGHUP 與 SIGUSR1 的職責分界與 handler 改寫）

- [x] 6.1 引入 `queryLogState` 結構（`cfg *config.QueryLogConfig` + `sink *logging.ReopenSink`，見 design 決策 6）與 `qlState atomic.Pointer[queryLogState]`：`run()` 啟動時 Store 初始狀態（含無 query log 時的 nil-欄位狀態），作為「目前生效的 query-log 設定與 sink」的唯一真相來源——因 `querylog.Logger` 不保存 FilePath 與原始 print 選項，比對與關閉皆不可能透過 logger 完成。驗證方式：`go build ./...` 通過；`go test ./cmd/shadowdns/ -run TestReloadQueryLog` 的後續子測試以 `qlState.Load()` 斷言狀態。

- [x] 6.2 實作「Query log configuration is re-applied on SIGHUP」需求的「無變化 reuse」邏輯：在 `reload()` 中讀取 `qlState.Load().cfg` 與新 `cfg.QueryLog` 比對 `FilePath`、`PrintTime`、`PrintCategory`、`PrintSeverity`、`RotationIgnored` 五欄；全同（或兩者皆 nil）→ 跳過所有 file 操作，保留既有 `*querylog.Logger`、sink 與 qlState（query log 重新套用合約 unchanged 列）。`RotationIgnored` 不可省略——operator 只增刪 versions/size 時其餘四欄全同，漏比此欄會把 rotation-only 變更誤判為 unchanged，警告永不重發（設計決策 6）。比對實作建議直接用 struct 相等（`*oldCfg == *newCfg`；`config.QueryLogConfig` 目前恰好只有這五個 comparable 欄位，且 spec 要求比對涵蓋設定值全部欄位）；若採逐欄比較，未來新增欄位 MUST 同步加入比對清單。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadQueryLog/Unchanged` 斷言 `srv.QueryLog.Load()` 與 `qlState.Load()` 指標均不變且無 file open/close 呼叫。

- [x] 6.3 實作設定變更的替換邏輯，順序固定為「open 新 sink（fallible 階段）→ SwapState 後 `srv.QueryLog.Store(newLogger)` → `qlState.Store(新狀態)` → `oldSink.Close()`」：涵蓋 path/print/rotation-參數 變更（path-changed 列，含 path 不變僅 `RotationIgnored` 改變的情形）、由有變無（`srv.QueryLog.Store(nil)`、qlState 存 nil-欄位狀態、close 舊 sink）、由無變有（open 後 Store）三類轉換，完成 query log 重新套用合約狀態轉換表所有行。open 失敗時 `return fmt.Errorf("opening new query log: %w", err)` 且舊 logger / qlState 保留（「Failed sink open preserves existing logger」scenario）。「SIGHUP reload does not re-apply logging configuration」舊限制性需求已由本 change 移除，本任務實作其替代行為。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadQueryLog/PathChanged`、`/Removed`、`/Added`、`/FailedOpen`。

- [x] 6.4 改寫 SIGUSR1 handler（「SIGHUP 與 SIGUSR1 的職責分界與 handler 改寫」設計決策）：(a) 安裝條件改為「`opts.LogReopener != nil` 或 server 為支援 SIGHUP reload 的 daemon 模式」（query log 可能在任何一次 reload 後出現）；(b) handler 每次收到訊號時動態組 reopen 清單——固定的 `opts.LogReopener` 加上 `qlState.Load().sink`（nil 則略過），不再使用啟動時捕獲的固定 slice，實作「SIGUSR1 reopen is unaffected by SIGHUP reconfigure」、「Query log introduced by reload is reopenable via SIGUSR1」與 MODIFIED 需求「Query log file participates in SIGUSR1 reopen」的「Handler registration does not depend on startup sinks」scenario。競態視窗下對剛被 reload 關閉的舊 sink 呼叫 `Reopen` 會得到 `os.ErrClosed`（task 7.1 的終態語義），handler 記 log 即可。驗證方式：`go test ./cmd/shadowdns/ -run TestSigusr1AfterReload` 斷言 reload 換路徑後 SIGUSR1 reopen 的是新路徑 sink；啟動無 query log、reload 新增後 SIGUSR1 仍能 reopen。

- [x] 6.5 在 `run()` 的 shutdown 路徑**新增**透過 `qlState.Load().sink` 關閉 query-log sink 的步驟（現行程式碼的正常 shutdown 路徑完全不關閉此 sink——`qlReopener.Close()` 只存在於 dry-run 提前返回的路徑，sink 原本靠 process 退出隱式回收），確保最後一次 reload 所開啟的 sink 在進程結束時被關閉、不洩漏 fd；關閉動作 MUST 排在 reload goroutine join 之後（順序屬 task 8.1，「SIGHUP goroutine 的 join 與 shutdown 清理順序」設計決策）。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadQueryLog` 之 teardown 無洩漏報告；`make test` 通過。

- [x] 6.6 在 reload 的設定變更路徑（reuse 路徑除外）比照啟動路徑檢查 `cfg.QueryLog.RotationIgnored`，為 true 時發出相同的 rotation-ignored 警告（BIND versions/size 參數被忽略、改用外部 rotation 工具＋SIGUSR1），實作「Rotation-parameters warning is re-emitted when reload applies a changed config」scenario；設定無變化時不重發。「只增刪 versions/size、其餘設定全同」的變更因 task 6.2 把 `RotationIgnored` 列入比對欄位而落入變更路徑，警告必須照發。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadQueryLog/RotationWarning` 斷言變更路徑（含 rotation-only 變更）發出警告、unchanged 路徑不發。

## 7. ReopenSink Close 終態語義（`internal/logging/reopen.go`）（ReopenSink Close 為終態：Reopen 對已關閉 sink 回傳 os.ErrClosed）

- [x] [P] 7.1 修改 `(*logging.ReopenSink).Reopen()`：在持鎖檢查 `s.f == nil`（已 Close）時直接回傳 `os.ErrClosed`，不開檔、不安裝新 fd——與 `Write` 對 closed sink 的行為一致，使 `Close` 成為終態，實作「Reopen of a sink closed by reload returns an error instead of resurrecting it」scenario。現行行為會把已關閉的 sink「復活」並洩漏無人持有的 fd，這是 design 決策 6/7 競態安全論證的前置修正（「ReopenSink Close 為終態」設計決策）。對存活 sink 的 Reopen 行為不變。驗證方式：`go test ./internal/logging/ -run TestReopenSinkClosedTerminal`。

- [x] 7.2 在 `internal/logging/` 新增 `TestReopenSinkClosedTerminal` 測試函式：開 sink → `Close()` → `Reopen()` 斷言回傳 `os.ErrClosed` 且 sink 內部 fd 仍為 nil（未開新檔）；同時斷言存活 sink 的 `Reopen` 仍正常換 fd（既有行為迴歸保護），覆蓋「ReopenSink Close 終態合約」。驗證方式：`go test ./internal/logging/ -run TestReopenSinkClosedTerminal -v` 全部 pass。

## 8. SIGHUP goroutine join 與 shutdown 清理順序（`cmd/shadowdns/main.go`）（SIGHUP goroutine 的 join 與 shutdown 清理順序）

- [x] 8.1 以 `sync.WaitGroup` 追蹤 SIGHUP dispatch goroutine 與 SIGUSR1 goroutine（各自 `Add(1)` 於啟動前、goroutine 內 `defer Done()`），兩個 goroutine 皆改為監聽 `run()` 以 `context.WithCancel(ctx)` 派生的**子 context**，SIGHUP handler 呼叫 `reload()` 時傳入該子 context（task 3.1——reload 衍生的 NOTIFY goroutine 隨 shutdown 取消），並把 shutdown 清理順序固定為：`srv.Serve` 返回（不論因 ctx 取消或 listener fatal error）→ `signal.Stop(sighupCh)` → cancel 子 context → `wg.Wait()`（等待進行中的 reload 跑完、SIGUSR1 handler 退出）→ 關閉 `qlState.Load().sink` → 關閉 `geo` 全部存活 GeoIP handle（current＋prev），實作「Shutdown 順序合約」。子 context 不可省略：`srv.Serve` 在任一 listener 死亡時帶 error 返回而**父 ctx 並未取消**，goroutine 若只靠父 `ctx.Done()` 退出，這條路徑上 `wg.Wait()` 會永久死鎖（「SIGHUP goroutine 的 join 與 shutdown 清理順序」設計決策）。此 join 消除 reload 寫入 `geo`/`qlState` 與 shutdown 清理之間的 data race、double-close 與資源洩漏。`signal.Stop` → cancel → `wg.Wait()` MUST 顯式寫在 body；sink 與 GeoIP 的關閉建議放 defer（在 join 之後執行、且覆蓋 early-return 路徑，見 design 決策 12 與 task 4.2）。SIGUSR1 goroutine 的 join 不可省略：僅改聽子 context 時，cancel 前已入 channel 的訊號可能讓 handler 在 `run()` 返回後才執行 `Reopen` 並透過 logger 記錄，task 8.2(b) 的 in-process 測試會在測試結束後觸發 log 而故障（「Log in goroutine after test has completed」類 panic）。驗證方式：`go build ./...` 通過；`make test`（race detector）無 DATA RACE。

- [x] 8.2 在 `cmd/shadowdns/` 新增 `TestShutdownDuringReload` 測試函式，覆蓋「Shutdown 順序合約」的兩條觸發路徑：(a) 觸發 reload 的同時取消父 ctx 觸發 shutdown，斷言清理完成後無 data race（race detector）、GeoIP handle 與 query-log sink 各被關閉恰好一次、reload 開啟的新資源不洩漏；(b) 模擬 listener fatal error 的返回路徑——父 ctx 保持存活、僅由 shutdown 序列 cancel 子 context——斷言清理序列在有限時間內完成（不死鎖）且資源同樣恰好關閉一次（實作上可把 goroutine 啟動＋shutdown 序列抽成 `run()` 與測試共用的 helper 以便注入此情境）。驗證方式：`go test -race ./cmd/shadowdns/ -run TestShutdownDuringReload -v` 全部 pass 且無 DATA RACE 輸出。

## 9. 單元測試

- [x] 9.1 在 `internal/metrics/` 新增 `TestReloadMetrics` 測試函式，測試 `RecordReload("success")`、`RecordReload("failure")`、`SetLastReloadSuccess()` 的 counter/gauge 值變化、註冊後未觀測前兩條 counter series 即存在且為 0（label 預初始化），並確認 nil receiver 不 panic，覆蓋「Reload outcome is tracked by a total counter」與「Last-reload-success timestamp is exposed as a gauge」需求。驗證方式：`go test ./internal/metrics/ -run TestReloadMetrics -v` 全部 pass。

- [x] 9.2 在 `internal/metrics/` 新增（或擴充既有）`TestSetGeoIPInfo` 測試函式，以不同 build_time 連續呼叫 `SetGeoIPInfo` 兩次，斷言每個 database label 僅存在最新 build_time 的 series（差分刪除生效），並斷言 nil receiver 呼叫不 panic。驗證方式：`go test ./internal/metrics/ -run TestSetGeoIPInfo -v` 全部 pass。

- [x] 9.3 在 `cmd/shadowdns/` 新增 `TestReloadGeoIP` 測試函式，使用 test fixture mmdb 目錄，斷言：reload 後 `geo.country`/`geo.asn` 更新、被換下的舊 handle 進入 prev 槽且**仍可 Lookup**（延遲關閉，未被立即 Close）、第二次 reload 後第一代 handle 已關閉、geoip_db_info gauge 更新且無舊 build_time series，以及 GeoIP 失敗時 reload 返回 error、舊 DB 保留、failure counter 遞增。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadGeoIP -v` 全部 pass。

- [x] 9.4 在 `cmd/shadowdns/` 新增 `TestReloadRateLimiter` 測試函式，斷言：RRL config 有效時 `srv.RateLimiter.Load()` 返回新 limiter（credit table 空、recorder 為同一 `*metrics.Metrics`）；RRL config 為 nil 時 limiter 為 nil；`NewLimiter` 失敗時 reload 返回 error 且舊 limiter 不變。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadRateLimiter -v` 全部 pass。

- [x] 9.5 在 `cmd/shadowdns/` 新增 `TestReloadQueryLog` 測試函式，覆蓋 query log 狀態轉換表五個情境（Unchanged / PathChanged / Removed / Added / FailedOpen 子測試）與 RotationWarning 子測試（task 6.6，含「只增刪 versions/size、path 與 print 選項全同」的 rotation-only 變更情境——斷言其落入變更路徑、警告重發、sink 換新），含每個情境下 `qlState.Load()` 的 cfg 與 sink 斷言，確保「Query log configuration is re-applied on SIGHUP」需求的行為邊界正確。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadQueryLog -v` 全部 pass。

- [x] 9.6 在 `cmd/shadowdns/` 新增 `TestReloadMetrics` 測試函式，觸發成功/失敗 reload 路徑，斷言 `shadowdns_reload_total{result="success"}` 與 `shadowdns_reload_total{result="failure"}` 計數以及 `shadowdns_config_last_reload_success_timestamp_seconds` gauge 的正確性（含 task 3.4 的啟動初始化值——ready 後未送 SIGHUP 前即為啟動時刻而非 0），確保「Reload metrics are emitted on every reload attempt」需求的整合覆蓋。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadMetrics -v` 全部 pass。

- [x] 9.7 在 `cmd/shadowdns/` 新增 `TestSigusr1AfterReload` 測試函式，覆蓋：reload 換 query-log 路徑後 SIGUSR1 reopen 作用於新 sink（舊 sink 不再被觸碰）；啟動時無 query log、reload 新增 query log 後 SIGUSR1 可 reopen 新 sink，確保「SIGUSR1 reopen is unaffected by SIGHUP reconfigure」與「Query log introduced by reload is reopenable via SIGUSR1」兩個 scenario 的覆蓋。驗證方式：`go test ./cmd/shadowdns/ -run TestSigusr1AfterReload -v` 全部 pass。

- [x] 9.8 在 `cmd/shadowdns/` 新增 `TestReloadNoMetrics` 測試函式：以 metrics 停用組態（`srv.Metrics == nil`）分別觸發成功與失敗 reload，斷言 reload 語義正常（state swap / 舊狀態保留）且全程不 panic——覆蓋「Reload completes when metrics are disabled」scenario（nil-safe 的 `RecordReload`/`SetLastReloadSuccess`/`SetGeoIPInfo` 與帶 guard 的 `SetRecorder`）。驗證方式：`go test ./cmd/shadowdns/ -run TestReloadNoMetrics -v` 全部 pass。

## 10. 整合驗證

- [x] 10.1 執行 `make test`（包含 race detector）確認所有新增測試通過且無 data race，驗證「Rate limiter is rebuilt atomically on SIGHUP」需求中原子操作的正確性、qlState 的跨 goroutine 安全性、以及 shutdown 與 reload 之間的 join 順序（「SIGHUP goroutine 的 join 與 shutdown 清理順序」設計決策）。驗證方式：`make test` 輸出 `ok` 且無 `DATA RACE` 行。

- [x] 10.2 執行 `make lint` 確認無新增 linter warning，確保所有 nil check、error wrap、atomic pointer 使用符合 golangci-lint 規則。驗證方式：`make lint` 輸出無新增 warning/error。

- [x] 10.3 執行 `make smoke`（`--dry-run`）確認 `reload()` 新簽章與 `run()` caller 的整合無編譯錯誤，驗證「`reload()` 函式新簽章」合約。驗證方式：`make smoke` 正常輸出 dry-run 訊息並以 exit 0 結束。

## 11. 文件同步（`README.md`）（既有註解同步）

- [x] 11.1 更新 `README.md`（依語言規範維持英文）兩處與舊行為綁定的敘述，使其反映本 change 的新行為：(a) features 清單中 query logging 條目的「Settings take effect at startup only — SIGHUP reload does not re-apply `logging{}` changes」改為描述 SIGHUP 會重新套用 `logging{}`（路徑/print 選項變更無需重啟；SIGUSR1 reopen 語義不變）；(b) View Matcher 段落的「GeoIP lookups use MaxMind's mmdb format read directly into memory at startup」補上 mmdb 隨 SIGHUP 重載（MaxMind 月更無需重啟）的敘述；可一併在適當段落提及 `shadowdns_reload_total` 與 `shadowdns_config_last_reload_success_timestamp_seconds` 兩個新 metric。驗證方式：content review——`grep -n "does not re-apply" README.md` 無殘留舊敘述，且 README 對 SIGHUP 行為的描述與 spec 一致。
