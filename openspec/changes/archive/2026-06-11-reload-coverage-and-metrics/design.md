## Context

ShadowDNS 的 `reload()` 函式（`cmd/shadowdns/main.go`）負責接收 SIGHUP、重新讀取 named.conf、建構新的 server state、並原子替換舊狀態。截至本 change 前，`reload()` 有下列已知限制（均已記錄於 spec）：

1. GeoIP mmdb 在啟動時讀取一次，SIGHUP 不重載 → MaxMind 月更需重啟
2. RRL `*ratelimit.Limiter` 在啟動時建立一次，SIGHUP 不重建 → rate-limit 參數變更需重啟
3. `logging{}` query log 設定在啟動時套用一次，SIGHUP 不重新套用 → 路徑/格式變更需重啟
4. `reload()` 成功或失敗均無 Prometheus metric → 靜默失敗時無法被監控系統偵測

這四個問題都位於同一段 reload → state-build 路徑，合併修改可共享測試夾具，且無法獨立部署（例如 GeoIP 重載需要新的 DB 指標更新）。

現有程式碼約束（實作時必須面對的事實）：

- `Server.RateLimiter` 欄位：`*ratelimit.Limiter`，在 `run()` 啟動時由 `ratelimit.NewLimiter(cfg.Options.RateLimit)` 建構並賦值，之後不再更改；metrics 啟用時另以 `rateLimiter.SetRecorder(m)` 接上 recorder
- `Server.QueryLog` 欄位：`*querylog.Logger`，在 `run()` 啟動時初始化
- `Server.Metrics` 欄位已存在：metrics 啟用（`--metrics-addr` 非空）時 `run()` 會執行 `srv.Metrics = m`；停用時保持 nil。`reload()` 已接收 `srv`，因此**不需要新增 metrics 參數**
- `querylog.New(path, cfg)` 回傳 `(*querylog.Logger, *logging.ReopenSink, error)` 三值；**`Logger` 不保存 FilePath 也不保存原始 `Config`**（print 選項在建構時已 resolve 成內部欄位），所以「目前生效的 query-log 設定」無法從 logger 讀回，必須另行追蹤
- `querylog.Logger` 沒有 Close 方法；關閉 sink 的入口是 `(*logging.ReopenSink).Close()`。`ReopenSink` 內部有 `sync.Mutex`，`Write` / `Reopen` / `Close` 之間互相序列化
- SIGUSR1 handler 是獨立 goroutine，在啟動時把 `opts.LogReopener` 與 query-log 的 `qlReopener` 捕進固定的 `reopeners` slice——reload 換掉 sink 後這份 slice 不會跟著更新；且啟動時兩者皆 nil 的話 handler 根本不會安裝
- `run()` 的正常 shutdown 路徑**目前完全不關閉 query-log sink**——`qlReopener.Close()` 只存在於 dry-run 提前返回的路徑，正常結束時 sink 由 process 退出隱式回收。因此「shutdown 時關閉 qlState 的 sink」是本 change **新增**的行為，不是改寫既有關閉點
- GeoIP DB（`country *view.CountryDB`, `asn *view.ASNDB`）作為 `reload()` 的參數傳入，由 `run()` 在外部持有並 `defer Close()`
- `Metrics.SetGeoIPInfo` 對 GaugeVec 只新增 `(database, build_time)` label 組合，不刪除舊組合（對照 `SetZoneCounts` 有 prevZoneViews 差分刪除）；且**沒有 nil receiver guard**——現行程式碼只在 metrics-enabled 區塊內呼叫它，所以從未踩到
- `maxminddb.Reader.Close()`（v2.2.0）會 **munmap** buffer 並把內部 buffer 設為 nil；Close 後的並發 `Lookup` 是 use-after-munmap——在 Go 中讀取已 unmap 的記憶體觸發的 SIGSEGV 是**不可 recover 的 fatal error**，handler 的 `defer recover()` 救不了。每個 DNS query 都會經 `st.Matcher.Resolve` → `Country.Lookup`/`ASN.Lookup` 打 mmdb，且 `SwapState` 明文允許 in-flight 查詢以舊 state snapshot 跑完——舊 state 的 Matcher 持有舊 DB 參照
- `ReopenSink.Reopen` 目前對已 `Close` 的 sink **不會回 error**：它會重開 `s.path` 並安裝新 fd（把 sink「復活」），造成無人持有、無人關閉的 fd 洩漏；對照 `Write` 在 closed 時正確回 `os.ErrClosed`
- SIGHUP dispatch goroutine 沒有 join：ctx 取消後 `run()` 直接返回並執行 defers，若 reload 正在進行中，reload 對共享變數的寫入與 shutdown 清理（close GeoIP DB / sink）之間沒有任何 happens-before 關係
- `srv.Serve`（`internal/server/listener.go`）有**兩條返回路徑**：ctx 取消（graceful shutdown），或任一 listener 發生 fatal error（`firstErr = <-errCh`）——後者返回時 **ctx 並未取消**。SIGHUP dispatch goroutine 目前只靠 `ctx.Done()` 退出，因此任何「Serve 返回後等待該 goroutine」的設計都必須讓 goroutine 在 listener 錯誤路徑上也能退出，否則 join 會永久阻塞
- 把 nil 的 `*metrics.Metrics` 塞進 `ratelimit.Recorder` interface 會形成 typed-nil（non-nil interface 包 nil 指標），`Limiter.record` 的 `l.metrics != nil` 檢查擋不住，之後每次 RRL 決策都會對 nil receiver 呼叫 `RecordRateLimit` 而 panic——`SetRecorder` 的呼叫必須以 `srv.Metrics != nil` 為前提

## Goals / Non-Goals

**Goals:**

- reload 成功/失敗均記錄 Prometheus metric（`shadowdns_reload_total{result}`，label 預初始化為 0）
- `shadowdns_config_last_reload_success_timestamp_seconds` gauge 於啟動初始載入成功時初始化為啟動時刻，並在每次 reload 成功時更新
- SIGHUP 後重新開啟 GeoIP mmdb，使 MaxMind 月更無需重啟即生效；`shadowdns_geoip_db_info` 更新且不殘留舊 build_time series；被換下的舊 DB handle 延遲一代關閉，與 in-flight 查詢之間無 use-after-munmap
- SIGHUP 後依新設定重建 RRL limiter（重置 credit table）
- SIGHUP 後重新套用 query log 路徑與 print 選項；若設定未變則 reuse 既有 file handle
- SIGUSR1 reopen 在 reload 換 sink / 新增 sink 後仍然正確
- 所有可能失敗的 reload 步驟在 `SwapState` 前完成，失敗不留下半套用狀態
- metrics 停用（`--metrics-addr ""`）的組態下，reload 全路徑（成功與失敗）不 panic、不 crash
- shutdown 與進行中的 reload 之間無 data race、無 double-close、無資源洩漏（`make test` race detector 可驗證）

**Non-Goals:**

- 不重啟 UDP/TCP listener（此為現有 spec 的硬性約束）
- 不保留 RRL limiter 重建後的舊 credit table 狀態（重置為可接受行為，詳見下方 Decisions）
- 不在 reload 期間動態更改 `--reload-verify`、`--metrics-addr`、`-pprof-enable` 等 process-lifetime sticky flag
- 不實作 query log 的原子切換（在新 sink 開啟失敗時，保留舊 sink 繼續服務即可）
- 不為 GeoIP reload 失敗加上重試（失敗則保留舊 DB 並返回 error，讓 reload 整體失敗）
- 不更動主 log（zap）的 reload 行為；本 change 只涉及 query log

## Decisions

### 1. Reload metrics 宣告位置

**決策**：新增 `reloadTotal *prometheus.CounterVec`（labels: `result`）與 `lastReloadSuccessTimestamp prometheus.Gauge` 至 `internal/metrics/metrics.go` 的 `Metrics` 結構（unexported，與既有欄位命名慣例一致），並提供 `RecordReload(result string)` 與 `SetLastReloadSuccess(t time.Time)` 兩個方法。`New()` 註冊時以 `WithLabelValues("success")` / `WithLabelValues("failure")` 預初始化兩個 label 組合，使 `/metrics` 從啟動起即輸出值為 0 的兩條 series。

Gauge 的對外名稱為 `shadowdns_config_last_reload_success_timestamp_seconds`——`_seconds` 單位後綴比照 Prometheus 生態慣例（Prometheus 自身的同義 metric 即為 `prometheus_config_last_reload_success_timestamp_seconds`），promtool lint 對無單位的 timestamp gauge 會警告。

Gauge 的**啟動語義**同樣比照 Prometheus：`run()` 的 metrics 啟用區塊在初始設定載入成功後（建立 `m` 之後、metrics HTTP server 啟動之前）即呼叫 `SetLastReloadSuccess(time.Now())` 把 gauge 初始化為啟動時刻。Prometheus 自身在啟動初次成功載入 config 時就會設定該 timestamp——若沿用名字卻維持「第一次 SIGHUP 前恆為 0」，operator 照抄生態系常見的 `time() - <gauge> > X` staleness 告警樣板，會在從不 reload 的主機上永遠觸發。metrics HTTP listener 在同一區塊稍後才啟動，因此 0 值對外不可觀察。

**理由**：與現有其他 metric（`RecordRequest`、`RecordResponse`、`RecordRateLimit`）的宣告模式一致，避免散落定義。預初始化讓 `increase(...{result="failure"}[5m])` 類告警不需處理 metric 缺席。

**替代方案**：在 `main.go` 定義 package-level counter — 否決，因為測試難以 assert 且違反 metrics 套件的封裝原則。

### 2. `reload()` 取得 `*metrics.Metrics` 的方式

**決策**：**不新增參數**。`reload()` 已接收 `srv *server.Server`，而 `Server.Metrics` 欄位已存在（metrics 啟用時由 `run()` 賦值，停用時為 nil），reload 直接呼叫 `srv.Metrics.RecordReload(...)` / `srv.Metrics.SetLastReloadSuccess(...)`。**reload 路徑會呼叫到的所有 `*metrics.Metrics` 方法——`RecordReload`、`SetLastReloadSuccess`、以及既有的 `SetGeoIPInfo`——一律以 nil receiver no-op 實作**（`func (m *Metrics) RecordReload(...) { if m == nil { return } ... }`，與 `querylog.Logger.Log` 的 nil-safe 模式一致），讓 metrics 停用與單元測試情境不需特判。`SetGeoIPInfo` 現行**沒有** nil guard，若不補上，metrics 停用組態下第一次 SIGHUP 就會在無 recover 的 SIGHUP goroutine 中 panic、帶垮整個 daemon。

唯一的例外是 `newLimiter.SetRecorder(srv.Metrics)`：`Recorder` 是 interface，傳入 nil 的 `*metrics.Metrics` 會形成 typed-nil（見 Context 約束），nil-safe receiver 救不了 interface 內的 nil 指標。此呼叫 MUST 包在 `if srv.Metrics != nil { ... }` 條件內。

**理由**：避免為已經可達的依賴增加參數與 caller 改動；`m` 目前宣告在 `run()` 的 `if opts.MetricsAddr != ""` 區塊內，走參數方案還得 hoist。

**替代方案**：擴充 `reload()` 參數列表加入 `m *metrics.Metrics` — 否決，`srv.Metrics` 已提供同一物件，新參數是多餘的管線。

### 3. RRL limiter 重建時是否保留 credit table

**決策**：重建時**重置** credit table，不保留舊狀態。

**理由**：RRL limiter 的 credit table 是揮發性的運作狀態（rolling window），不是設定。保留舊狀態需要跨設定版本的 key 相容性（prefix length 可能改變），且可能讓舊的限流決策污染新設定的窗口。重置屬於 BIND9 本身重啟後的行為，在 v0.x.x 實驗階段可接受。重置後的短暫過渡期（最多一個 window，預設 15 秒）對生產流量影響有限。

**替代方案**：序列化 credit table 並嘗試遷移 — 否決，實作複雜度高且在設定參數改變時語義不明確。

### 4. Server.RateLimiter 與 Server.QueryLog 的執行緒安全替換

**決策**：使用 `atomic.Pointer[ratelimit.Limiter]` 取代裸指標欄位 `*ratelimit.Limiter`，使用 `atomic.Pointer[querylog.Logger]` 取代裸指標欄位 `*querylog.Logger`，讓 `reload()` 可在不持鎖的情況下原子替換兩者。handler hot path 透過 `Load()` 讀取，`reload()` 透過 `Store()` 替換。

**理由**：與現有 `atomic.Pointer[serverState]`（`Server.state`）的模式完全一致，無需額外 mutex。`Server.QueryLog` 的替換發生在 SIGHUP goroutine，DNS handler 在熱路徑中並發讀取，若使用裸指標會產生 data race（`go test -race` 可偵測）。hot path 僅多一次 atomic load，不引入鎖。

**替代方案**：使用 `sync.RWMutex` 保護欄位 — 否決，在 DNS query hot path 上額外的 lock 開銷不必要。

### 5. GeoIP DB 重載的失敗語義與 gauge 殘留清理

**決策**：reload 在 parse 後 MUST 比照啟動路徑（`run()` 對 `cfg.Options.GeoIPDirectory == ""` 的顯式檢查）驗證 `geoip-directory` 非空——空值回明確的設定錯誤，而非讓 `LoadGeoIP` 以相對路徑開檔產生混淆的 file-not-found。若 `view.LoadGeoIP()` 在 reload 期間回傳 error，`reload()` 立即返回該 error（`return fmt.Errorf("reloading GeoIP: %w", err)`），整個 reload 失敗，舊 state 保留，failure counter 遞增。舊的 `*CountryDB`/`*ASNDB` 繼續使用直到下次成功 reload。成功 reload 後被換下的舊 DB handle 的關閉時機見決策 10（延遲一代關閉）——**不得在 swap 後立即關閉**。

同時把 `Metrics.SetGeoIPInfo` 改為**差分更新**：在 `Metrics` 結構新增 `prevGeoIPLabels map[string]string`（database → 上次的 build_time），`SetGeoIPInfo` 設定新 label 組合後，刪除（`DeleteLabelValues`）database 相同但 build_time 不同的舊組合。否則 reload 換新 mmdb 後 `/metrics` 會同時出現新舊 build_time 兩條值為 1 的 series，打壞「依 build_time 判斷 DB 是否過期」的告警模式。此模式比照 `SetZoneCounts` 的 prevZoneViews 差分刪除。

**理由**：與現有「reload 失敗保留舊 state」的語義一致（spec: "Reload failure preserves existing state"）。GeoIP 是 state-build 的關鍵輸入，部分失敗會導致 state 不一致。

**替代方案**：GeoIP 失敗時 fallback 舊 DB 繼續 reload — 否決，可能造成新 named.conf 的 view 設定搭配舊 GeoIP 運作，不一致風險更高。

### 6. Query log 目前生效設定與 sink 的追蹤（queryLogState holder）

**問題**：`querylog.Logger` 不保存 FilePath 與原始 print 選項，無法用來比對「設定是否變更」；`Logger` 也沒有 Close 方法，關閉舊 sink 必須透過建構時取得的 `*logging.ReopenSink`。因此 reload 需要的「目前設定」與「目前 sink」都必須在 logger 之外追蹤。

**決策**：在 `cmd/shadowdns/main.go` 引入：

```go
// queryLogState tracks the currently active query-log configuration and its
// file sink. Logger itself does not retain either, so reload comparison and
// sink close/reopen go through this record.
type queryLogState struct {
    cfg  *config.QueryLogConfig // nil when query logging is disabled
    sink *logging.ReopenSink    // nil when query logging is disabled
}
```

以 `qlState atomic.Pointer[queryLogState]` 共享於 `run()`、SIGHUP reload goroutine 與 SIGUSR1 goroutine 之間（`run()` 啟動時 Store 初始狀態；只有 SIGHUP goroutine 會改寫）。設定比對規則：新舊 `cfg` 都為 nil → 無操作；單邊 nil → 增/減 sink；都非 nil 時比較 `FilePath`、`PrintTime`、`PrintCategory`、`PrintSeverity`、`RotationIgnored` 五欄，全同 → reuse（不開不關），任一不同 → 開新 sink、`srv.QueryLog.Store(newLogger)`、`qlState.Store(新狀態)`、最後 `oldSink.Close()`。

比對的實作 SHOULD 直接用 struct 相等（`*oldCfg == *newCfg`）：`config.QueryLogConfig` 目前恰好只有上述五個 comparable 欄位，struct 相等與五欄逐比等價，且未來對 `QueryLogConfig` 新增欄位時自動納入比對、不會 silent miss。若實作選擇逐欄比較，新增欄位時 MUST 同步加入比對清單（query-logging spec 對「比對涵蓋設定值的全部欄位」有明文要求）。

`RotationIgnored` 必須列入比對欄位：operator 若**只**在 file clause 增刪 `versions`/`size`（rotation 參數），`FilePath` 與三個 print 選項全部不變——若只比四欄，這種變更會被誤判為 unchanged 走 reuse 路徑，rotation-ignored 警告（見下）就永遠不會重發，且 `qlState.cfg` 留著過期的 `RotationIgnored` 值。把它列為第五欄後，rotation-參數-only 的變更走 replace 路徑：對同一路徑重開 sink（O_APPEND，無資料遺失，代價只是一次多餘的 fd 輪替）並重發警告。

替換順序固定為：**open 新 sink（可失敗，失敗即 return err 不動現狀）→ Store 新 logger → Store 新 qlState → Close 舊 sink**。Close 排在最後，使 SIGUSR1 goroutine 取到舊 sink 的殘餘窗口只會對「仍開著的」sink 操作；`ReopenSink` 內部 mutex 保證 `Reopen` 與 `Close` 互斥，搭配決策 11（`Reopen` 對已關閉 sink 回傳 `os.ErrClosed`），最壞情況是對已關閉 sink 呼叫 Reopen 得到 error 並記 log，不影響新 sink。注意：**現行的 `Reopen` 並非如此**——它會重開路徑把已關閉的 sink「復活」並洩漏 fd，所以決策 11 是本決策安全論證的前置條件，不可省略。

另外，reload 套用的新 `logging{}` 設定若帶有 BIND rotation 參數（`cfg.QueryLog.RotationIgnored == true`）且設定有變更（reuse 路徑不重發），比照啟動路徑發出 rotation-ignored 警告——否則 operator 在 reload 時新加 versions/size 參數不會得到任何提示。

**理由**：把「設定真相」放在 logger 之外唯一一處，同時解決比對、關閉、reopen 三個需求；避免在 reload 時不必要地重建 file handle（inode 切換），防止短暫的 log 遺失。

**替代方案**：
- 讓 `querylog.Logger` 保存原始 Config 並提供 getter — 可行但把「設定快照」職責塞進熱路徑物件，且仍解決不了 sink 關閉與 SIGUSR1 reopen 的追蹤問題，否決。
- 每次 reload 都重建 logger — 否決，會導致 logrotate 已 rename 的情況下意外重建而非 reopen，與 SIGUSR1 語義衝突。

### 7. SIGHUP 與 SIGUSR1 的職責分界與 handler 改寫

- **SIGUSR1**：reopen 現有 sink（logrotate 後讓新 inode 被寫入）。不改變設定、不改變 FilePath、不重建 logger 物件
- **SIGHUP**：重新套用 named.conf 中的 `logging{}` 設定，可能替換 FilePath/print 選項；若 FilePath 不變則不做 reopen（SIGUSR1 負責 reopen）

兩者不衝突：SIGHUP 管「要寫到哪裡、怎麼寫」，SIGUSR1 管「已知路徑的 file handle 是否需要重開」。

**handler 改寫**：現行 SIGUSR1 handler 把 reopener 捕進啟動時固定的 slice，reload 換 sink 後會 reopen 到舊 sink；且啟動時無任何 file sink 的話 handler 不會安裝，reload 後新增的 query log 將無法 reopen。改為：

1. 只要主 log 為 file-backed（`opts.LogReopener != nil`）**或 server 支援 SIGHUP reload（query log 可能在任何一次 reload 後出現）**，就安裝 SIGUSR1 handler——實務上即 daemon 模式恆安裝
2. handler 每次收到訊號時動態組 reopen 清單：固定的 `opts.LogReopener`（主 log 不參與 reload，啟動時決定）＋ `qlState.Load().sink`（可能為 nil，nil 則略過）
3. `run()` 的 shutdown 路徑改為關閉 `qlState.Load().sink`（而非啟動時捕獲的 `qlReopener`），確保最後一次 reload 開啟的 sink 被關閉

### 8. Reload 步驟順序：所有 fallible 步驟先於 SwapState

**決策**：`reload()` 的步驟順序固定為：

0. 關閉上一代延遲關閉的 GeoIP DB（`geo.prevCountry` / `geo.prevASN`，見決策 10；首次 reload 時為 nil 即略過。距上次 swap 已隔整個 reload 間隔，任何曾持有該代 state 的 in-flight 查詢早已完成，關閉安全）
1. `config.LoadNamedConf`（可失敗）
2. `shadowdnscfg.Load`（可失敗）
3. `view.LoadGeoIP` 開新 GeoIP DB（可失敗；`LoadGeoIP` 自身已保證 ASN 開檔失敗時關閉已開的 country handle）
4. `server.BuildState`（以新 GeoIP DB 建構；可失敗，失敗時關閉新 GeoIP DB 後返回）
5. `ratelimit.NewLimiter` 建新 limiter（可失敗，同上清理後返回）
6. query-log：依決策 6 比對，需要時 open 新 sink（可失敗，同上清理後返回）
7. `srv.SwapState(state)` —— **此後不再有任何可失敗步驟**
8. 安裝：`srv.RateLimiter.Store`、`srv.QueryLog.Store` + `qlState.Store`、GeoIP handle 輪替（`geo.prev* = geo.current*`、`geo.current* = new`，**不關閉**剛被換下的 DB——交由下一次 reload 的步驟 0 或 shutdown 處理，見決策 10）＋ `SetGeoIPInfo`（nil-safe，見決策 2）；關閉被取代的舊 query-log sink（Close 錯誤記 log，不影響 reload 結果）
9. ephemeral store clear、NOTIFY dispatch、`RecordReload("success")` + `SetLastReloadSuccess`

任一 fallible 步驟失敗：清理該次已建立的新資源、`RecordReload("failure")`、return err，舊 state／limiter／logger／GeoIP 全數不動（步驟 0 已關閉的上一代 DB 除外——它本就不再被任何 state 引用）。

**理由**：spec「Reload failure preserves existing state」要求失敗的 reload 不得留下半套用狀態。若 limiter 或 sink open 排在 SwapState 之後失敗，會出現「新 zone state 已生效＋舊 limiter/logger」的混合狀態，無法對外解釋。把失敗面集中在 swap 之前，swap 之後全是純賦值與 Close。

### 9. 既有註解同步

`reload()` 的 doc comment（「GeoIP databases are reused from startup」）與 `run()` metrics 區塊的註解（「databases are not reloaded on SIGHUP, so these values remain stable」）描述的是被本 change 移除的舊行為，實作時一併更新，避免註解與行為矛盾。

`README.md` 也有兩處綁定舊行為的敘述需同步（依語言規範維持英文）：features 清單中 query logging 條目的「Settings take effect at startup only — SIGHUP reload does not re-apply `logging{}` changes」改為描述 SIGHUP 會重新套用 `logging{}` 的新行為；View Matcher 段落的「GeoIP lookups use MaxMind's mmdb format read directly into memory at startup」補上 mmdb 隨 SIGHUP 重載的敘述。

### 10. 舊 GeoIP DB handle 的延遲一代關閉（geoipRuntime holder）

**問題**：`maxminddb.Reader.Close()` 會 munmap。`SwapState` 後仍可能有 in-flight 查詢持有舊 state snapshot、尚未完成 `Matcher.Resolve` 的 mmdb `Lookup`——swap 後立即關閉舊 DB 是 use-after-munmap，最壞情況觸發**不可 recover 的 SIGSEGV** 帶垮整個 daemon，`-race` 測試也會間歇性偵測到 `Close` 對內部 buffer 的裸寫入競態。

**決策**：被換下的舊 DB handle **不在 swap 後立即關閉**，改為「延遲一代關閉」：在 `cmd/shadowdns/main.go` 引入

```go
// geoipRuntime owns the live GeoIP handles plus the generation swapped out
// by the most recent reload. The swapped-out generation is closed at the
// start of the next reload (or at shutdown), never immediately after the
// state swap — in-flight queries may still resolve views against it.
type geoipRuntime struct {
    country *view.CountryDB
    asn     *view.ASNDB
    prevCountry *view.CountryDB // generation deferred for close; nil before first reload
    prevASN     *view.ASNDB
}
```

寫入者只有兩個且互相有 happens-before：startup（goroutine 啟動前初始化 `current` 欄位）與 SIGHUP goroutine（reload 時輪替）；shutdown 讀取發生在 SIGHUP goroutine join 之後（決策 12），因此**不需要 atomic 或 mutex**。生命週期規則：

- reload 步驟 0：關閉 `prev*`（若非 nil）並清空——距上次 swap 至少隔了一整個 reload 間隔，舊 state 的 in-flight 查詢（µs–ms 級）早已完成
- reload 步驟 8：`prev* = current*`、`current* = 新 DB`——只輪替，不關閉
- shutdown（join 後）：關閉 `prev*` 與 `current*` 全部存活 handle，Close 錯誤記 log

**理由**：關閉時機與「還有誰可能在用」解耦——關閉永遠只發生在「該代 DB 已確定無人引用」的時點（下次 reload 開頭或 join 後的 shutdown），不依賴 sleep、引用計數或對 query 時長的假設。代價是兩代 mmdb 並存至下次 reload（GeoLite2 country + ASN 約多佔 ~20MB mmap，多為 page cache 共享，可接受）。

**殘餘假設（誠實記錄）**：「距上次 swap 已隔整個 reload 間隔」在連續快速 SIGHUP 下不成立——dispatch loop drain 之後立刻又收到 SIGHUP 時，`SwapState(N)` 到 reload N+1 步驟 0 的間隔可縮短到 reload N 的尾段（listen-addr drift 檢查＋logging＋loop turnaround，ms 級）。安全性此時依賴「mmdb `Lookup` 是 µs 級、且每個 query 只在 `Matcher.Resolve` 期間觸碰 DB」：ms 級的最小間隔仍比 lookup 時長高出多個數量級，實務安全。理論上的失敗情境是一個被重度 deschedule（CPU 飢餓、STW）的 goroutine 恰好卡在 `Resolve` 內跨越整個間隔——機率極低，v0.x.x 實驗階段接受此殘餘風險，不為它引入引用計數或最小 reload 間隔限制。

**替代方案**：
- swap 後立即關閉（原設計）— 否決，use-after-munmap 崩潰風險，理由如上。
- `time.AfterFunc` 寬限期關閉 — 否決，寬限期長短是對 query 時長的猜測，且多出一個需要管理生命週期的 timer goroutine。
- 對 ServerState 做引用計數 — 否決，為微秒級的窗口引入侵入式的 hot-path 計數成本，過度設計。

### 11. ReopenSink Close 為終態：Reopen 對已關閉 sink 回傳 os.ErrClosed

**問題**：現行 `ReopenSink.Reopen`（`internal/logging/reopen.go`）沒有 closed 檢查：對已 `Close` 的 sink 呼叫 `Reopen` 會重開 `s.path` 並安裝新 fd，把 sink「復活」成一個無人持有、永不關閉的 fd（並 pin 住舊路徑 inode）。決策 6/7 的競態安全論證（「對已關閉 sink reopen 得到 error」）依賴的行為實際上不存在。

**決策**：`Reopen` 在 `s.f == nil`（已 Close）時直接回傳 `os.ErrClosed`，不開檔——與 `Write` 對 closed sink 的行為一致，`Close` 成為終態。SIGUSR1 handler 收到此 error 時記 log 即可（殘餘競態窗口的預期結果），無資源洩漏。

**理由**：修法最小（一個 guard），使 Close/Reopen/Write 三者語義一致；SIGUSR1 的正當使用情境（logrotate 後 reopen）永遠發生在 sink 存活時，不受影響。

**替代方案**：維持現狀並在 caller 端避免競態 — 否決，caller（SIGUSR1 goroutine）與 reload goroutine 本質上就是並發的，從 qlState 取出 sink 到呼叫 Reopen 之間的窗口無法在 caller 端消除。

### 12. SIGHUP goroutine 的 join 與 shutdown 清理順序

**問題**：SIGHUP dispatch goroutine 沒有 join：ctx 取消後 `srv.Serve` 返回、`run()` 的 defers 開跑，但 goroutine 可能正在 reload 中段。reload 對 `geo` holder 的寫入與 shutdown 的讀取＋Close 之間沒有 happens-before（data race、double-close、或洩漏 reload 剛開好的新 DB / 新 sink）。本 change 讓 reload 開始寫入這些共享資源，使這個原本不存在的競態成為現實。

**決策**：`run()` 以 `sync.WaitGroup` 追蹤 SIGHUP dispatch goroutine 與 SIGUSR1 goroutine（各自 `Add(1)` 於啟動前、goroutine 內 `defer Done()`），且兩個 goroutine 皆改為監聽由 `run()` 以 `context.WithCancel(ctx)` 派生的**子 context**（而非直接用父 ctx）。SIGHUP dispatch goroutine 呼叫 `reload()` 時 MUST 傳入該子 context（而非父 ctx）：reload 內 `maybeDispatchNotifies` 衍生的 NOTIFY goroutine（10 秒 deadline、fire-and-forget、不在 WaitGroup 內）以此 ctx 派生 timeout——傳子 context 使 shutdown 序列 cancel 時，最後一刻 reload 所發出的 NOTIFY 立即中止；若傳父 ctx，在 listener-error 路徑（父 ctx 存活）上這些 goroutine 會活過 `run()` 返回最多 10 秒，與本決策要消除的 goroutine 殘留同型。shutdown 清理順序固定為：`srv.Serve` 返回（**不論因 ctx 取消或 listener fatal error**）→ `signal.Stop(sighupCh)`（停止新訊號注入，避免 shutdown 期間連續 SIGHUP 無限延長關機）→ **cancel 子 context**（保證 goroutine 在 listener 錯誤路徑——父 ctx 仍存活——也會退出）→ **`wg.Wait()`（等待進行中的 reload 跑完、SIGUSR1 handler 退出）** → 關閉 `qlState.Load().sink` → 關閉 `geo` 的全部存活 GeoIP handle。`signal.Stop` → cancel → `wg.Wait()` 三步 MUST 以顯式呼叫寫在 `srv.Serve` 返回後的函式 body（而非 defer LIFO）；sink 與 GeoIP handle 的關閉則建議放在資源建立後即註冊的 defer——defer 在 body 的 join 序列之後才執行，正常路徑上「join 先於 close」自動成立，且 `LoadGeoIP` 之後、`Serve` 之前的所有 early-return 路徑（BuildState／limiter 建構／query-log open／listen-address 解析／BindMany 失敗、`--dry-run`）也被同一個 defer 覆蓋，不會洩漏 handle。若改採顯式關閉，early-return 路徑 MUST 另行收尾，且 `geo` holder 的關閉 helper MUST 在關閉後清空欄位（`view.CountryDB.Close` 對 nil 安全、對重複關閉不安全），使 defer 與顯式呼叫並存時不 double-close。SIGUSR1 goroutine 只讀 `qlState`（atomic）且不持有可關閉資源的唯一參照，但 MUST 同樣納入同一 WaitGroup 並改聽子 context——現行只聽父 ctx，在 listener fatal error 路徑（父 ctx 未取消）上 `run()` 返回後永不退出，生產上 process 隨即結束無妨，但 in-process 的 listener-error 測試（task 8.2 路徑 b）會殘留 goroutine，測試結束後再觸發 log 即故障（「Log in goroutine after test has completed」類 panic）。注意**僅改聽子 context 而不 join 並不足夠**：SIGUSR1 在 cancel 前已寫入 channel 時，select 可能先取訊號，handler 在 `run()` 返回後才執行 `Reopen` 並透過 logger 記錄結果——同型故障；WaitGroup 既已存在，join 第二個 goroutine 成本近零，把這個殘餘視窗徹底封住。

**為什麼需要子 context**：`srv.Serve` 在任一 listener 死亡時會帶著 error 返回，而此時父 ctx 並未取消（見 Context 約束）。若 goroutine 只靠父 `ctx.Done()` 退出，這條路徑上 `wg.Wait()` 會永久阻塞——daemon 不會結束，只能等 systemd 超時 SIGKILL。子 context 讓「Serve 已返回」成為 goroutine 的退出訊號，兩條返回路徑行為一致。

**理由**：join 之後，shutdown 對 `geo`／`qlState` 的存取與 reload 寫入之間有明確 happens-before，`-race` 乾淨；reload 是秒級操作，shutdown 多等它跑完不影響 graceful shutdown 的語義。

**替代方案**：
- 把 `geo` 欄位全部改成 `atomic.Pointer` — 否決，原子化只消除 torn read，無法解決「shutdown 關閉 vs reload 使用中」的語義競態，join 才是正解。
- goroutine 維持只監聽父 ctx — 否決，listener 錯誤路徑上 join 永久死鎖（如上）。
- 不 join、僅靠 atomic 保護 — 否決，無法保證 reload 開到一半的新資源（新 GeoIP DB、新 sink）被關閉。

## Implementation Contract

### Metric 合約

完成實作後，透過 `GET /metrics` 可觀察到以下新 metric：

- `shadowdns_reload_total{result="success"}` 與 `shadowdns_reload_total{result="failure"}` — 啟動即存在且為 0（label 預初始化）；每次 SIGHUP reload 依結果對應遞增
- `shadowdns_config_last_reload_success_timestamp_seconds` — 最近一次成功設定載入的 Unix 秒數（float64）；啟動初始載入成功時即初始化為啟動時刻（決策 1，比照 Prometheus 自身行為），之後隨每次成功 reload 更新；0 值對外不可觀察（metrics HTTP listener 在初始化之後才啟動）

metrics 停用（`--metrics-addr ""`，`srv.Metrics == nil`）時：reload 的成功與失敗路徑均正常完成，所有 metrics 呼叫（`RecordReload`、`SetLastReloadSuccess`、`SetGeoIPInfo`）為 nil receiver no-op，不 panic；`SetRecorder` 不被呼叫（typed-nil guard，見決策 2）。

驗證方式：`go test ./internal/metrics/ -run TestReloadMetrics` 與 `go test ./cmd/shadowdns/ -run TestReloadMetrics`；metrics 停用情境由 `go test ./cmd/shadowdns/ -run TestReloadNoMetrics` 覆蓋。

### GeoIP 重載合約

`reload()` 成功返回後，`run()` 持有的 `geo.country`/`geo.asn` 指向新 `*CountryDB`/`*ASNDB`，並且 Prometheus `shadowdns_geoip_db_info` gauge 已更新為新 DB 的 `BuildEpoch`（傳入 `SetGeoIPInfo`，nil-safe），且舊 build_time 的 series 已被刪除（每個 database label 任何時刻至多一條 build_time series）。被換下的舊 DB handle 進入 `geo.prevCountry`/`geo.prevASN` 延遲關閉槽，**在 swap 後保持可用**（in-flight 查詢仍可能透過舊 state 的 Matcher 對它做 Lookup），實際關閉發生在下一次 reload 的步驟 0 或 shutdown（決策 10、12）。`run()` 啟動時的 `defer country.Close()` / `defer asn.Close()` 改為對 `geo` 全部存活 handle 的關閉，且 MUST 同時覆蓋兩類路徑：(a) 正常 shutdown——關閉發生在 reload goroutine join 之後（決策 12）；(b) `LoadGeoIP` 之後、`Serve` 之前的所有 early-return 路徑（BuildState／limiter 建構／query-log open／listen-address 解析／BindMany 失敗、`--dry-run`）——只在 Serve 返回後顯式關閉會讓這些提前返回洩漏 handle。建議實作為註冊於 `LoadGeoIP` 後的單一 defer（見決策 12 的順序論證），確保不論經過幾次 reload 都不洩漏 handle。

驗證方式：`go test ./cmd/shadowdns/ -run TestReloadGeoIP`（斷言 reload 後舊 handle 仍可 Lookup、進入 prev 槽；第二次 reload 後第一代 handle 已關閉；shutdown 後全部關閉）。

### RRL Limiter 重建合約

`reload()` 成功返回後，`srv.RateLimiter.Load()` 返回依據新 `cfg.Options.RateLimit` 建構的 limiter；新 limiter 的 credit table 為空（無舊狀態）；recorder 沿用同一個 `srv.Metrics`（建構後在 `if srv.Metrics != nil` guard 內呼叫 `SetRecorder(srv.Metrics)`——不可無條件呼叫，typed-nil 陷阱見決策 2），`shadowdns_dns_rate_limit_total` 連續無中斷。`srv.RateLimiter` 使用 `atomic.Pointer[ratelimit.Limiter]` 確保替換的原子性。

驗證方式：`go test ./cmd/shadowdns/ -run TestReloadRateLimiter`。

### Query Log 重新套用合約

下表定義 reload 前後狀態與預期結果（比對與關閉均透過 `qlState`，見決策 6）：

| 前狀態 | 後設定 | 預期行為 |
|---|---|---|
| 有 logger（path=A） | 相同 path=A 且 print 選項與 rotation 參數狀態（`RotationIgnored`）皆相同 | 保留既有 logger 與 qlState，無 file 操作 |
| 有 logger（path=A） | 不同 path=B、任一 print 選項不同、或 `RotationIgnored` 改變（含 path 不變僅增刪 versions/size 的情形） | open 新 sink（path 可能等於 A）→ `srv.QueryLog.Store(newLogger)` → `qlState.Store(new)` → close 舊 sink |
| 有 logger（path=A） | 無 logging{} | `srv.QueryLog.Store(nil)` → `qlState.Store(nil 狀態)` → close A |
| 無 logger | 有 logging{} | open 新 sink → `srv.QueryLog.Store(newLogger)` → `qlState.Store(new)` |
| 無 logger | 無 logging{} | 不做任何操作 |

open 新 sink 失敗（例如目錄不存在）時：`reload()` 返回 error，舊 logger 與 qlState 保留，failure counter 遞增。reload 完成後收到 SIGUSR1：handler 透過 `qlState.Load().sink` reopen **目前**的 sink（含 reload 才新增的 sink）。

驗證方式：`go test ./cmd/shadowdns/ -run TestReloadQueryLog` 與 `go test ./cmd/shadowdns/ -run TestSigusr1AfterReload`。

### `reload()` 函式新簽章（概念性）

```
func reload(
    ctx context.Context,
    opts runOptions,
    srv *server.Server,
    geo *geoipRuntime,         // current + prev 兩代 GeoIP handle，見決策 10
    qlState *atomic.Pointer[queryLogState],
    logger *zap.Logger,
) error
```

**備註**：原構想的 `country **view.CountryDB` / `asn **view.ASNDB` 雙指標被 `geo *geoipRuntime` holder 取代——延遲一代關閉（決策 10）需要同時追蹤 current 與 prev 兩代 handle，雙指標無法表達。`geo` 不需要 atomic：寫入者只有 startup（happens-before goroutine 啟動）與 SIGHUP goroutine，shutdown 讀取在 join 之後（決策 12）。metrics 經由 `srv.Metrics` 取得，不在參數列。`ctx` 為 `run()` 派生的**子 context**（決策 12）——reload 衍生的 NOTIFY goroutine 隨 shutdown 序列一併取消。

### ReopenSink Close 終態合約

`(*logging.ReopenSink).Close()` 之後：`Write` 回傳 `os.ErrClosed`（既有行為）、`Reopen` 回傳 `os.ErrClosed` 且**不開檔、不安裝新 fd**（新行為，決策 11）。`Close` 可重複呼叫（既有行為，冪等）。對存活 sink 的 `Reopen` 行為不變。

驗證方式：`go test ./internal/logging/ -run TestReopenSinkClosedTerminal`（Close 後呼叫 Reopen 斷言回傳 `os.ErrClosed`，且以 `lsof`-free 方式——例如比對 Reopen 前後 `s.f` 仍為 nil——斷言未開新 fd）。

### Shutdown 順序合約

`srv.Serve` 返回後（**兩條返回路徑——ctx 取消或 listener fatal error——適用同一序列**）：`signal.Stop(sighupCh)` → cancel 子 context → 等待 SIGHUP dispatch 與 SIGUSR1 goroutine 結束（進行中的 reload 跑完、handler 退出）→ 關閉 query-log sink（`qlState.Load().sink`）→ 關閉 `geo` 全部存活 GeoIP handle。整個序列下 `make test`（race detector）無 DATA RACE，不發生 double-close 或 handle 洩漏，且在 listener 錯誤路徑（父 ctx 未取消）上 `run()` 仍能在有限時間內返回——不死鎖。

驗證方式：`go test ./cmd/shadowdns/ -run TestShutdownDuringReload`（shutdown 與 reload 並發觸發，race detector 乾淨、資源全數關閉恰好一次；含「父 ctx 存活、僅子 context 取消」的 listener-錯誤模擬路徑，斷言清理序列完成不卡死）。

## Risks / Trade-offs

- [風險] GeoIP reload 在讀取 mmdb 期間增加了 reload 的延遲（通常 < 50ms）→ 在 reload 路徑已有 named.conf 解析與 zone build，增加 GeoIP open 對整體延遲影響可忽略
- [風險] Query log sink 替換期間（Store 新 → Close 舊）有極短的 log 遺失視窗 → 屬已知且可接受的 tradeoff（SIGUSR1 reopen 同樣有此視窗），在 spec 說明清楚即可
- [風險] SIGUSR1 goroutine 在 reload 換 sink 的瞬間可能取到舊 sink 並 reopen → `ReopenSink` 內部 mutex 序列化 Reopen/Close，且決策 11 保證對已關閉 sink 的 Reopen 回傳 `os.ErrClosed`（不開檔、不洩漏 fd），handler 記 log 即可，新 sink 不受影響；視窗極小且無資料毀損
- [風險] RRL credit table 重置後 15 秒過渡期內，已在觀察窗口的攻擊者會獲得短暫的額外配額 → 在 v0.x.x 實驗階段可接受，且此行為與 BIND9 重啟後語義一致
- [風險] `reload()` 參數增加（qlState、geo holder）提高了 caller 的修改幅度 → 只有一個 caller（`run()` 中的 SIGHUP loop），影響局部
- [風險] 延遲一代關閉使兩代 GeoIP mmdb 並存至下次 reload → GeoLite2 country + ASN 合計約多佔 ~20MB 的 mmap（多為唯讀 page cache，可被 OS 回收），相對崩潰風險可接受；下次 reload 步驟 0 即回收
- [風險] shutdown 需等待進行中的 reload 跑完（決策 12 的 join）→ reload 為秒級操作，graceful shutdown 多等一個 reload 在運維上可接受；換得 race-free 的資源關閉。join 不死鎖的前提是子 context 在 Wait 前被 cancel（決策 12）——listener fatal error 返回時父 ctx 未取消，少了這步 join 會永久阻塞
- [風險] 連續快速 SIGHUP 會把延遲一代關閉的「一代間隔」壓縮到 ms 級（決策 10 的殘餘假設）→ mmdb Lookup 為 µs 級且僅發生在 `Matcher.Resolve` 內，ms 級間隔仍有數量級裕度；理論失敗情境（goroutine 被重度 deschedule 卡在 Resolve 內跨越整個間隔）機率極低，v0.x.x 接受
- [風險] rotation-參數-only 的 logging 變更（path/print 全同）會觸發同路徑的 sink 重開（決策 6 把 `RotationIgnored` 列入比對欄位）→ O_APPEND 下無資料遺失，代價僅一次多餘的 fd 輪替；換得 rotation-ignored 警告必定重發、`qlState.cfg` 不殘留過期值
- [風險] `SwapState` 與 limiter/logger `Store` 之間（安裝步驟的純賦值序列，µs 級）新進查詢可能觀察到「新 zone state＋舊 limiter/logger」的瞬時混合 → 各元件替換本身原子、整組安裝非原子；視窗無持久化效果（下一次 atomic load 即見新值），可接受——spec 的原子性保證為 per-component，不承諾整組 reload 對單一查詢呈現原子
