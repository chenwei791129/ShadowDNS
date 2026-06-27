## Context

ShadowDNS 目前有三台背景 HTTP server，各自實作「在 goroutine 跑 `http.Server`、等 context 取消、再 graceful shutdown」的 lifecycle：

- **Ephemeral API**（`internal/api` 的 `Server.Serve`）：goroutine serve，`ctx.Done()` 時以 `api.ShutdownTimeout` 呼叫 `Shutdown`，再 drain `errCh`。只設了 `ReadHeaderTimeout: 10s`，其餘連線逾時沿用 net/http 預設（無上限）。
- **Prometheus metrics**（`cmd/shadowdns/main.go`）：goroutine serve，以 `defer Shutdown` 帶內嵌 `5 * time.Second` 字面值，serve 錯誤 inline log，不 drain `errCh`。`http.Server` 完全未設任何連線逾時。
- **DoH**（`internal/doh` 的 `runWith`）：協調 HTTPS + ACME HTTP-01 兩個 listener 加上憑證續期 goroutine，有自己的 `errCh`、`WaitGroup`、`doh.shutdownTimeout`，並用 `newHardenedServer` 設好完整的 read/write/idle/header 逾時。

這三份實作的差異比表面上更不對稱：graceful-shutdown deadline 有三個來源（`api.ShutdownTimeout`、`main.go` 內嵌字面值、`doh.shutdownTimeout`）；連線 hardening 從「完整」（DoH）、「只有 header」（API）到「完全沒有」（metrics）三種程度；停機觸發方式也三種（API 只靠 parent ctx、metrics 靠 defer、DoH 靠 own-cancel）。

此技術債在 DoH endpoint 的 code review 中反覆浮現並被刻意延後（Issue #5），因為正確修法會跨到已出貨的 API 與 metrics server，超出該 change 的 scope。

## Goals / Non-Goals

**Goals:**

- 抽出單一共用原語，承載「單台 `http.Server` 的 serve / hardened 逾時 / bounded graceful drain / 過濾 `ErrServerClosed`」核心，供三台 server 共用。
- graceful-shutdown deadline 收斂為單一來源。
- 三台 server 統一套用完整的 hardened 連線逾時。
- 三台 server 在「signal 驅動停機」與「DNS listener 中途死亡」兩條退出路徑上都能 bounded graceful drain（補齊目前缺漏的 ephemeral API）。
- 不改變任何 server 的對外協定（endpoint、回應格式、設定欄位、CLI flag 皆不動）。

**Non-Goals:**

- **不**處理 Issue #5 Notes 列的「相關 duplication」：whole-value config 比較器（`dohConfigEqual`、`queryLogConfigEqual`）與 reload-drift advisory 區塊（DNS listen-address、query-log、DoH）。這些屬 config 比較與 SIGHUP reload 建議邏輯，與 HTTP server lifecycle 無耦合，混入會撐大 diff；建議獨立另開 change。
- **不**把 DoH 的多 listener + 憑證續期協調下放到共用原語 —— 共用原語只管「一台」server。
- **不**新增任何設定欄位或 CLI flag 來調整逾時／deadline；沿用既有的固定值（spec 記載的 up-to-5s drain）。
- **不**改變 metrics / API / DoH 任一端點的 handler 行為或路由。

## Decisions

### Decision 1：共用原語放在新 package `internal/httpserver`，只負責單台 server

新增 `internal/httpserver`，提供：

- 一個建構 hardened `*http.Server` 的 helper（套用統一的 read / idle / header 逾時；WriteTimeout 因 pprof 串流例外，見 Decision 3）。
- 一個 serve 函式，封裝「在 goroutine 啟動 serve → `select` on `ctx.Done()` / `errCh` → 以共用 deadline `Shutdown` → drain `errCh` → 過濾 `ErrServerClosed` 回傳第一個真實錯誤」。**啟動方式由 caller 以 serve-start closure（`func() error`）注入**，使同一個原語能涵蓋三種啟動模式：API 的 `srv.Serve(ln)`（已 bound listener）、metrics 的 `srv.ListenAndServe()`（addr）、DoH HTTPS 的 `srv.ListenAndServeTLS("", "")`（TLS + `TLSConfig.GetCertificate`）。原語本身不在意 plaintext / TLS / listener / addr 的差異，只擁有 select / Shutdown / drain / `ErrServerClosed` 核心（避免列舉式簽章漏掉 TLS 模式）。
- graceful-shutdown deadline（單一來源）與 hardened 逾時常數的單一來源。

**為何放新 package 而非塞進既有 package**：metrics server 住在 `cmd/shadowdns/main.go`，DoH 已 import `internal/server`，API 自成一格。放進任一既有 package 會造成 import 方向問題（main 要 import doh 只為了借 lifecycle helper 並不合理）。獨立的 leaf package 讓三方都能乾淨依賴。

**為何只管一台**：見 Decision 2。

### Decision 2：DoH 的多 listener 與憑證續期協調，疊在共用原語之上

`runWith` 真正的複雜度在於「HTTPS listener + ACME HTTP-01 listener + 憑證續期 loop 三者，在**同一個** `shutdownTimeout` 預算內並行 drain」—— 若序列 drain 會疊成約 10s，超出 `main.go` 的 defer 與外層 systemd `TimeoutStopSec` 所預算的時間。這是 DoH 特有的協調需求。

若硬要讓共用原語支援「N 台共用一個 deadline 預算」，介面會被 DoH 的特例污染，而 metrics / API 用不到那層複雜度（過度設計）。因此共用原語只提供 per-server 的 serve / drain 核心；DoH 在其上層保留自己的雙 listener 啟動、憑證續期 goroutine、以及並行 drain 協調。

**明確保留的不只是 drain，還有 failure-propagation 不變量**：DoH 目前以 `runCtx`/`cancel`（`server.go` 的 `runWith`）確保「任一 listener bind/serve 失敗時，連帶取消憑證續期 loop」，否則 `cm.run()` 只會在 parent ctx 結束才退出、`wg.Wait()` 會 deadlock 並持續打 ACME directory。共用原語是 per-server、阻塞在自己 ctx 上的，看不到 sibling listener 的失敗；因此這個 cancel-on-serve-error 的跨 listener 失敗傳播**必須**保留在 DoH 上層，不能因為「改走共用原語」而退化成只剩 drain 協調。

**`shutdownServer` 的處置**：DoH 的 `shutdownServer`（per-listener `Shutdown(共用 deadline) + Warn`）是其並行 drain goroutine 呼叫的 primitive，無法被阻塞式的 serve 原語取代。為避免「drain 行為（deadline + warn 格式）」又在 DoH 與共用原語各留一份而日後漂移，共用 package 額外導出一個小型 per-server drain helper（`Shutdown(共用 deadline) + Warn`），DoH 的並行 drain goroutine 改呼叫它；DoH 僅保留「要 drain 哪幾個 listener、如何並行、如何湊在一個 deadline 預算內」的協調邏輯。

**替代方案**：讓原語接受 `[]*http.Server` 並回傳第一個錯誤。否決理由 —— DoH 還需要協調憑證續期 goroutine（非 `http.Server`）的並行 drain 與 failure-propagation，`[]*http.Server` 介面表達不了，最後仍得在 DoH 上層補協調碼，等於介面複雜度白付。

### Decision 3：graceful-shutdown deadline 與 hardened 逾時收斂為單一來源

`api.ShutdownTimeout`、`main.go` 的內嵌 `5 * time.Second`、`doh.shutdownTimeout` 三處同值，收斂到 `internal/httpserver` 的單一常數。hardened 連線逾時以 DoH 既有那組（read 10s / write 10s / idle 120s / header 5s）為統一預設。

這對 metrics（原本零逾時）與 API（原本只有 `ReadHeaderTimeout: 10s`）是**可觀察的行為變更**：兩者將獲得 slow-loris / idle 連線防護。此變更為刻意納入 scope 的修正方向，並在 spec 中以正規需求記載。

**WriteTimeout 對 metrics server 的例外**：metrics mux 在 `--pprof-enable` 時掛載 `/debug/pprof/profile`（預設 CPU profile 30s）與 `/debug/pprof/trace`（caller 指定秒數）等**長時間串流**端點（`cmd/shadowdns/pprof.go`）。固定的 `WriteTimeout: 10s` 會在串流中途切斷連線，回傳截斷／損毀的 profile —— 這是本 change 會新引入的 operational regression（metrics server 現況零逾時）。因此 metrics server 套用 read / idle / header 逾時，但**不套用 WriteTimeout**（設為 0／不截斷串流的值）。API 與 DoH 的回應都是短小的（DNS message / 小 JSON），維持 `WriteTimeout: 10s`。hardened 建構 helper 因此需允許 caller 省略／覆寫 WriteTimeout。

**替代方案 1**：保留各 server 既有逾時值不動，只收斂 shutdown deadline。否決理由 —— 會留下「metrics 完全無逾時」這個既存的安全弱點，違背單一來源目標。**替代方案 2**：對 metrics 也套用固定 WriteTimeout。否決理由 —— 直接破壞 pprof profiling，是淨退步。

### Decision 4：在 `main.go` 為 ephemeral API 補上 own-cancel + defer-wait（metrics 已由 defer 涵蓋，不動）

acceptance criteria「每條退出路徑都 graceful drain」的根因在 `main.go` 的編排層，不在原語層：當 DNS listener 中途死亡，`srv.Serve` 會在 parent ctx 仍存活的情況下回傳（既有的 shutdown-order 合約），此時只靠 parent ctx 停機的 server 永遠不會收到停止訊號。

逐台盤點現況：

- **DoH**：已用 `dohCtx/dohCancel + dohDone + defer{cancel; <-done}` 解決，兩條路徑皆 drain。**不動**。
- **metrics**：用 `run()`-scoped 的 `defer func(){ ...; metricsSrv.Shutdown(...) }()`。`defer` 在 `run()` 的**每條** return（含 signal-driven 與 listener-death）都會執行，且 `http.Server.Shutdown` 直接停 server（不依賴 ctx 傳播到 goroutine），故 metrics **已**在兩條路徑 graceful drain。**不動其編排**，僅套用 Decision 3 的 timeout / deadline 收斂。
- **ephemeral API**：目前只用裸 `ctx`（無 own-cancel、無 defer-wait），在 listener-death 路徑上 goroutine 連同其 server 被 process 退出直接砍掉、in-flight 連線硬切。**這是唯一的缺口**。本 change 讓 ephemeral API 比照 DoH 採 `apiCtx/apiCancel + apiDone + defer{cancel; <-done}`。

**為何不把 metrics 也改成 own-cancel + defer-wait**：metrics 既有的 plain `defer Shutdown` 已正確涵蓋兩條路徑；硬改成 own-cancel + channel handshake 只是替能用的程式碼增加 diff 面與回歸風險，零行為收益。

**defer 註冊順序**：`apiCancel`/`apiDone` 的 `defer` 與既有的 metrics、DoH defer 互相獨立（各自 `defer{cancel; <-done}`，不共享狀態），LIFO 順序只影響 drain 先後、不會略過任一台。唯一要求：API 的 own-cancel defer **必須在其 goroutine 啟動之後才註冊**，避免「defer 已註冊但 server 尚未啟動」的空窗。

**替代方案**：在原語內偵測 listener-death 並自行停機。否決理由 —— 原語看不到「parent ctx 仍存活但整個 process 要退」這個事件，該事件的觸發點在 `main.go` 的 `srv.Serve` 回傳處；只有編排層能正確驅動。

## Implementation Contract

**Behavior（可觀察）：**

- 三台背景 HTTP server（metrics、ephemeral API、DoH）在收到 signal 驅動的停機（SIGINT/SIGTERM → ctx 取消）時，皆以單一來源的 deadline graceful drain in-flight 連線。
- 三台 server 在「DNS listener 中途死亡導致 process 退出」這條路徑上，也都 graceful drain。先前 ephemeral API 在此路徑會硬切 in-flight 連線（本 change 修正之）；metrics、DoH 現況已 drain（維持）。
- metrics 與 ephemeral API server 套用與 DoH 一致的 read / idle / header 連線逾時（先前 metrics 無逾時、API 僅有 header 逾時）。WriteTimeout：API 套用 10s；metrics **不**套用 WriteTimeout（pprof 串流例外，見 Decision 3）。
- 對外協定不變：所有端點路徑、HTTP 方法、回應格式、`shadowdns.yaml` 欄位、CLI flag 皆與本 change 前相同；`--pprof-enable` 開啟時 `/debug/pprof/profile?seconds=30` 等串流端點回傳完整 profile（不被 WriteTimeout 截斷）。

**Interface / data shape：**

- 新 package `internal/httpserver` 對外導出：
  - 一個 graceful-shutdown deadline 常數（單一來源，值維持 5s）。
  - 一個建構 hardened `*http.Server` 的函式（套用統一 read / idle / header 逾時；允許 caller 省略／覆寫 WriteTimeout，供 metrics 的 pprof 例外使用）。
  - 一個 serve 函式，概念簽章為「吃 `context.Context`、一個已備妥的 `*http.Server`、以及一個 serve-start closure（`func() error`），阻塞至 ctx 取消或 serve-start 回傳，停機時以 fresh `context.WithTimeout(context.Background(), 共用 deadline)` 呼叫 `Shutdown`，回傳第一個非 `http.ErrServerClosed` 的錯誤，正常停機回傳 `nil`」。closure 由 caller 提供 `srv.Serve(ln)` / `srv.ListenAndServe()` / `srv.ListenAndServeTLS("", "")`，故 TLS 模式（DoH HTTPS）亦受支援。
  - 一個小型 per-server drain helper（`Shutdown(共用 deadline) + Warn-on-failure`），供 DoH 的並行 drain goroutine 呼叫，取代其自有的 `shutdownServer`。
- `internal/api` 的 `Serve` / `Run` 對外簽章維持不變，內部改呼叫 `internal/httpserver`。
- `internal/doh` 的 `Run` / `runWith` 對外簽章維持不變，內部以 `internal/httpserver` 取代手刻的 per-server serve/drain，並保留自身的多 listener 啟動、憑證續期 goroutine、跨 listener 的 cancel-on-serve-error failure-propagation 與並行 drain 協調（見 Decision 2）。
- 移除並改引用 `internal/httpserver` 的重複定義：`internal/api` 的 exported `ShutdownTimeout` 常數、`internal/doh` 的 `shutdownTimeout` 常數、`newHardenedServer` helper；`internal/doh` 的 `shutdownServer` 改呼叫共用 drain helper。`internal/doh/server_test.go` 的 `TestHardenedServer_HasNonZeroTimeouts`（現呼叫 `newHardenedServer`）一併改引用 `internal/httpserver` 的 hardened 建構函式，否則 doh 測試 package 無法編譯。

**Failure modes：**

- 任一 server serve 失敗（非 `ErrServerClosed`）時回傳該錯誤；caller（`main.go` 既有 goroutine）維持「log 錯誤、不中止 DNS 服務」的現行語意。
- DoH 任一 listener bind/serve 失敗時，連帶取消憑證續期 loop 與 sibling listener（保留現行 `runCtx`/`cancel` 行為），不 deadlock、不持續打 ACME directory（見 Decision 2）。
- 停機時以 fresh `context.Background()` 衍生的逾時 context 呼叫 `Shutdown`（**不可**用已取消的 parent ctx，否則 `Shutdown` 立即回傳 `context.Canceled`、graceful drain 變成 no-op）。
- graceful drain 超過 deadline 時，記 warning 並放棄等待（維持現行 DoH 的語意），不阻塞 process 退出。

**Acceptance criteria：**

- `internal/httpserver` 有單元測試涵蓋：(a) ctx 取消觸發 graceful shutdown、(b) serve 失敗回傳非 `ErrServerClosed` 錯誤、(c) `ErrServerClosed` 被吸收為 `nil`。
- grep 全 repo 後，graceful-shutdown deadline 只剩 `internal/httpserver` 一處定義（`api.ShutdownTimeout`、`main.go` 內嵌字面值、`doh.shutdownTimeout` 皆不再各自定義）。以 identifier 名稱搜尋，不以 `5 * time.Second` 字面值搜尋（`doh` 的 `readHeaderTimeout` 同為 5s，會誤命中）。
- API server 建構出的 `http.Server` 帶有 read/write/idle/header 逾時；metrics server 帶有 read/idle/header 逾時但 WriteTimeout 為 0（以測試或程式碼審視確認 pprof 串流不被截斷）。
- `--pprof-enable` 下 `/debug/pprof/profile?seconds=15` 之類超過 10s 的串流端點回傳完整 profile（手動或 `pprof_test.go` 既有覆蓋確認不被 WriteTimeout 截斷）。
- 既有的 `internal/api`、`internal/doh` 測試（含改寫後的 `TestHardenedServer_HasNonZeroTimeouts`）在重構後全綠（`make test`）。
- `make lint` 通過。

**Scope boundaries：**

- In scope：`internal/httpserver`（新）、`internal/api/server.go`、`internal/doh/server.go`、`internal/doh/server_test.go`（migrate `TestHardenedServer_HasNonZeroTimeouts`）、`cmd/shadowdns/main.go` 的 ephemeral API lifecycle / 啟動編排重構，及對應單元測試。
- Out of scope：config 比較器與 reload-drift advisory 去重（Issue #5 Notes，另開 change）；任何 handler / 路由 / 設定欄位 / CLI flag 的行為變更；新增可調逾時設定；`cmd/shadowdns/pprof.go` 的程式碼（不改，只因其串流端點決定 metrics 的 WriteTimeout 例外）；metrics server 既有 `defer Shutdown` 編排（已正確涵蓋兩條退出路徑，不改）。

## Risks / Trade-offs

- [metrics / API 新增連線逾時可能切斷原本被容忍的慢速 client] → 逾時值沿用 DoH 既有且寬鬆的組合（read 10s、idle 120s），對正常 client 無影響；且 metrics 與 API 皆為內部、firewall 受限的端點，慢速連線本就不該被無限容忍。納入 spec 正規需求，使行為變更有據可查。
- [metrics 套用 WriteTimeout 會截斷 pprof 串流 profile] → metrics server 不套用 WriteTimeout（見 Decision 3），保留 pprof `/debug/pprof/profile`、`/debug/pprof/trace` 的長時間串流；以 spec 正規需求與 acceptance criteria 守護。
- [重構觸碰三個既有 server 的停機路徑，可能引入回歸] → 以既有 `internal/api`、`internal/doh` 測試套件加上新 `internal/httpserver` 測試守護；停機是難以單元覆蓋的時序邏輯，輔以對「兩條退出路徑」的明確 acceptance criteria。
- [效能回歸風險] → 變更落在 `cmd/shadowdns/main.go` + `internal/api` + `internal/doh`，依 Perf-Guard 屬 must-run；惟三台 server 皆不在 DNS query hot path，預期影響趨近零，仍依規則於實作完成後量測或與使用者確認。

## Migration Plan

純內部重構，無資料遷移、無設定遷移、無對外協定變更。部署即既有的 ShadowDNS 升級流程（`release-shadowdns` 本地 build → 部署 ns2）。回滾即還原前一版 binary。

## Open Questions

- （已解決）共用 serve 函式的簽章 —— 採「caller 注入 serve-start closure（`func() error`）」的形式（見 Decision 1）。API 傳 `srv.Serve(ln)`、metrics 傳 `srv.ListenAndServe()`、DoH HTTPS 傳 `srv.ListenAndServeTLS("", "")`，原語不需區分 plaintext / TLS / listener / addr，同時涵蓋三種 caller。closure 形式與「另開 `ServeTLS` 變體」相比更小、更不易漏掉 TLS 模式，故採前者；此為實作細節，不影響對外行為合約。
