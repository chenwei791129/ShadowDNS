## Why

目前 SIGHUP reload 在三個面向存在靜默的功能缺口：GeoIP mmdb 不重載（需重啟才能讓 MaxMind 月更生效）、RRL limiter 不重建（rate-limit 設定變更在重啟前無法套用）、query log 設定不重新套用（logging{} 路徑/選項更動需重啟）。此外，reload 本身沒有任何 Prometheus metric，一旦靜默失敗（`Errorw("reload failed")` 後繼續服務），運維人員無從透過監控察覺。本 change 把這四個問題合併為一個修改批次，因為它們全部落在同一段 `reload()` → state-build 程式路徑，互相依賴且無法獨立部署。

## What Changes

- **新增** `shadowdns_reload_total{result}` counter（result label：`success` / `failure`），在 `reload()` 成功與失敗分支各自遞增；兩個 label 組合於註冊時預先初始化（值 0），讓告警表達式不需處理 metric 缺席
- **新增** `shadowdns_config_last_reload_success_timestamp_seconds` gauge（Unix 秒；`_seconds` 單位後綴比照 Prometheus 自身的 `prometheus_config_last_reload_success_timestamp_seconds` 命名慣例）；啟動初始載入成功時即初始化為啟動時刻（同樣比照 Prometheus 自身行為，讓 `time() - <gauge>` 型 staleness 告警在從不 reload 的主機上不誤報），之後僅在 reload 成功時更新
- **修改** `reload()` 函式：透過既有的 `srv.Metrics`（`*metrics.Metrics`，metrics 停用時為 nil，方法需 nil-safe）在成功路徑呼叫 `RecordReload("success")` 與 `SetLastReloadSuccess(time.Now())`；在失敗路徑（`return err`）呼叫 `RecordReload("failure")`
- **修改** `reload()` 函式：重新開啟 GeoIP mmdb（呼叫 `view.LoadGeoIP`），以新 `*CountryDB` / `*ASNDB` 取代舊者，並在重載後更新 Prometheus geoip_db_info gauge（同時刪除舊 build_time label 的殘留 series）；被換下的舊 DB handle **不立即關閉**（`maxminddb.Reader.Close()` 會 munmap，與仍持有舊 state 的 in-flight 查詢構成 use-after-munmap 崩潰風險），改為延遲到下一次 reload 開始或 process shutdown 時才關閉（延遲一代關閉）
- **修改** `reload()` 函式：依新設定重建 `*ratelimit.Limiter`；重建時重置 credit table（不保留舊狀態，為可接受行為）；新 limiter 透過 `srv.RateLimiter` 原子更新
- **修改** `reload()` 函式：重新套用 query log 設定（path / print 選項）——因 `querylog.Logger` 不保存原始設定，目前生效的 query-log 設定與 `*logging.ReopenSink` 另以共享 holder 追蹤；設定比對涵蓋 `FilePath`、三個 print 選項與 `RotationIgnored` 五欄（rotation 參數 versions/size 的增刪也算變更，避免 rotation-only 變更被誤判為 unchanged 而漏發警告）；無變化則 reuse 既有 sink；任一欄變更則 open 新 sink（路徑可能不變）並關閉舊者；SIGUSR1 reopen 職責不受影響（只 reopen，不重建 logger），且 SIGUSR1 handler 改為每次從 holder 讀取目前 sink，使 reload 換路徑或新增 query log 後 reopen 仍然正確；reload 套用的新 logging{} 設定若帶有 BIND rotation 參數（versions/size），比照啟動路徑重發 rotation-ignored 警告
- **修改** `internal/logging/reopen.go`：`ReopenSink.Close` 改為終態——`Reopen` 對已關閉的 sink 回傳 `os.ErrClosed`（比照 `Write` 的行為），不再重開路徑「復活」sink；杜絕 SIGUSR1 與 reload 換 sink 競態視窗下的 fd 洩漏
- **修改** `run()`：SIGHUP dispatch goroutine 以 `sync.WaitGroup` 追蹤並改為監聽 `run()` 派生的子 context；`srv.Serve` 返回後（不論因 ctx 取消或 listener fatal error——後者父 ctx 仍存活）依序 `signal.Stop` → cancel 子 context → join → 關閉 query-log sink 與 GeoIP DB，消除「reload 進行中 vs shutdown 清理」的 data race 與 double-close，且 listener 錯誤路徑不死鎖；SIGUSR1 goroutine 同樣納入同一 WaitGroup 並改聽子 context（join 成本近零，徹底封住 handler 在 `run()` 返回後仍執行 Reopen＋logging 的殘餘視窗），listener 錯誤路徑上不殘留 goroutine；SIGHUP dispatch goroutine 呼叫 `reload()` 時傳入該子 context，使 reload 衍生的 NOTIFY goroutine 隨 shutdown 序列一併取消
- **明定** reload 順序合約：所有可能失敗的步驟（parse、GeoIP open、state build、limiter 建構、query-log sink open）一律在 `SwapState` 之前完成；swap 之後只剩不會失敗的安裝步驟，保證失敗的 reload 不會留下半套用狀態
- **修改** `internal/metrics/metrics.go`：新增 reload counter 與 last-success gauge 的宣告與方法；`SetGeoIPInfo` 改為差分更新（刪除過期 build_time label，比照 `SetZoneCounts` 的 prevZoneViews 模式），並補上 nil receiver guard——reload 路徑會在 metrics 停用（`srv.Metrics == nil`）時呼叫它，現行實作會 panic 並帶垮無 recover 的 SIGHUP goroutine
- **修改** `internal/server/server.go`：`Server` 結構的 `RateLimiter` 與 `QueryLog` 欄位改為 `atomic.Pointer`，允許 reload 時安全替換

## Non-Goals

（design.md 將說明詳細設計決策，Non-Goals 記錄於 design.md）

## Capabilities

### New Capabilities

（無新 capability：本 change 僅修改既有 capability 的需求）

### Modified Capabilities

- `sighup-reload`：新增 reload metrics、GeoIP 重載、「fallible 步驟先於 state swap」三項需求；移除「GeoIP databases are not reloaded」限制性需求
- `prometheus-metrics`：新增 `shadowdns_reload_total`（含 label 預初始化）與 `shadowdns_config_last_reload_success_timestamp_seconds` 兩個 metric 的需求
- `response-rate-limiting`：新增 limiter 隨 SIGHUP 原子重建的需求（此需求由本 capability 單一持有，sighup-reload 不另持副本）
- `query-logging`：移除「SIGHUP reload does not re-apply logging configuration」需求，以 reload 重新套用 query log 設定的新需求取代（此需求由本 capability 單一持有，sighup-reload 不另持副本），並涵蓋 SIGUSR1 對 reload 後新 sink 的 reopen 行為、rotation 參數警告在 reload 變更路徑的重發、以及對已關閉 sink 的 Reopen 回傳 error（Close 終態）三個 scenario；另修改既有「Query log file participates in SIGUSR1 reopen」需求——daemon 模式恆註冊 SIGUSR1 handler（不再以啟動時存在 file sink 為條件）、reopen 清單於訊號時動態組成

## Impact

- Affected specs: `sighup-reload`, `prometheus-metrics`, `response-rate-limiting`, `query-logging`
- Affected code:
  - Modified: `cmd/shadowdns/main.go`（`reload()` 簽章與步驟順序、GeoIP handle 的 `geoipRuntime` holder 與延遲一代關閉、query-log 設定/sink holder、SIGUSR1 handler 動態讀取 sink、SIGHUP/SIGUSR1 goroutine WaitGroup join、shutdown 關閉順序、metrics 啟用區塊啟動時初始化 last-reload-success gauge、metrics 註解更新）
  - Modified: `internal/metrics/metrics.go`（reload metrics 宣告與方法、`SetGeoIPInfo` 差分刪除＋nil receiver guard）
  - Modified: `internal/server/server.go`（`RateLimiter` / `QueryLog` 欄位改 `atomic.Pointer`）
  - Modified: `internal/server/handler.go`（`RateLimiter` / `QueryLog` call site 改 `.Load()`）
  - Modified: `internal/logging/reopen.go`（`Reopen` 對已關閉 sink 回傳 `os.ErrClosed`，Close 為終態）
  - Modified: `README.md`（兩處與舊行為綁定的敘述：「SIGHUP reload does not re-apply `logging{}` changes」與「GeoIP … read directly into memory at startup」，同步為本 change 的新行為）
  - New: （無新檔案）
