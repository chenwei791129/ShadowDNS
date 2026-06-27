## Why

目前 codebase 有三份近乎相同的「在 goroutine 裡跑 `http.Server`、等 context 取消、再帶 5 秒 deadline graceful shutdown」實作（ephemeral API、Prometheus metrics、DoH），三者的逾時常數、連線 hardening、停機觸發方式都不一致 —— 其中 ephemeral API 在「DNS listener 中途死亡」這條退出路徑上根本不會 graceful drain，in-flight 連線會被硬切。這是 DoH endpoint code review 時反覆浮現、被刻意延後的技術債（Issue #5）。

## What Changes

- 抽出共用的背景 HTTP server lifecycle 原語（新 package `internal/httpserver`），負責「單台 `http.Server` 的 serve、hardened 連線逾時、單一來源的 graceful-shutdown deadline、回傳第一個非 `ErrServerClosed` 錯誤」。
- metrics、ephemeral API、DoH 三台 server 改走此原語；DoH 特有的雙 listener（HTTPS + ACME HTTP-01）與憑證續期協調，疊在共用原語之上，不再各自重抄 serve/drain 核心。
- graceful-shutdown deadline 由三處（`api.ShutdownTimeout`、`main.go` 內嵌的 `5 * time.Second`、`doh.shutdownTimeout`）收斂為單一來源。
- hardened 連線逾時（read / idle / header）統一套用到三台 server：metrics 從「完全無逾時」、ephemeral API 從「只有 `ReadHeaderTimeout`」，補齊為與 DoH 一致的逾時組。**WriteTimeout 例外**：metrics server 同時掛載 `--pprof-enable` 的 `/debug/pprof/profile`、`/debug/pprof/trace` 串流端點（預設 CPU profile 30s），固定的 WriteTimeout 會把 profile 截斷，故 metrics server 不套用 WriteTimeout（或設為不會截斷串流的值）。
- 修正 `cmd/shadowdns/main.go` 內 ephemeral API server 的啟動／停機編排，使其在「signal 驅動停機」與「DNS listener 中途死亡」兩條退出路徑上都能 graceful drain（比照現有 DoH 的 own-cancel + defer-wait 模式）。metrics server 既有的 `defer Shutdown` 已涵蓋兩條退出路徑，**不**改動其編排，僅套用上述 timeout / deadline 收斂。

## Non-Goals (optional)

<!-- design.md 將建立，Non-Goals 記於 design.md 的 Goals/Non-Goals 區塊。 -->

## Capabilities

### New Capabilities

- `background-http-lifecycle`: 所有背景 HTTP server（metrics、ephemeral API、DoH）共用的執行與停機合約 —— 統一的 hardened 連線逾時、單一來源的 graceful-shutdown deadline，以及在 signal 驅動停機與 listener 中途死亡兩條退出路徑上的 bounded graceful drain。

### Modified Capabilities

(none)

## Impact

- Affected specs: 新增 `background-http-lifecycle`
- Affected code:
  - New: internal/httpserver/server.go、internal/httpserver/server_test.go
  - Modified: internal/api/server.go、internal/doh/server.go、internal/doh/server_test.go（`TestHardenedServer_HasNonZeroTimeouts` 改引用 `internal/httpserver`）、cmd/shadowdns/main.go
  - Removed: `internal/api` 的 exported `ShutdownTimeout` 常數、`internal/doh` 的 `shutdownTimeout` 常數與 `newHardenedServer` helper（皆改引用 `internal/httpserver`）；`internal/doh` 的 `shutdownServer` helper 視情況改為呼叫 `internal/httpserver` 的共用 per-server drain（見 design.md Decision 2）
  - Reviewed-no-change: cmd/shadowdns/pprof.go（不改 code，但其 `/debug/pprof/profile`、`/debug/pprof/trace` 串流端點掛在 metrics mux 上，決定了 metrics server 的 WriteTimeout 例外，見 design.md Decision 3）
