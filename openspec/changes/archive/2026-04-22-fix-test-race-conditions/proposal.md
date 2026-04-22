# Fix test-side race conditions

## Why

當把 `make test` 的 `go test` 加上 `-race` 後，跑 `go test -race -count=1 ./...` 會冒出 1247 個 race warnings，分布在 `cmd/shadowdns`、`internal/server`、`test/integration` 三個 package（以及它們造成的下游 test failures，例如 `internal/transfer`、`internal/zone` 因為依賴 integration test fixture 而連帶失敗）。

經過 `/spectra-discuss` 的分析，所有 race 收斂到三個 **test infrastructure** 的根因。Production code 不存在 race —— 因為已確認 reload 流程不會重新 bind listener，`Server.listeners` 在啟動後 effectively immutable。

修掉 race 才能讓 race detector 在 CI 與本機測試中真正當作 bug 警示器使用，否則開發者會被迫忽略所有 race 輸出，使其形同虛設。

## What Changes

針對三個 root cause 修對應的 test 檔，不動 production code：

- **Fix 1**：test helper 改成主 goroutine 同步呼叫 `srv.Bind(...)`，再 fork goroutine 跑 `srv.Serve(ctx)`；取代目前 `go func(){ srv.Start(...) }()` + `time.Sleep(20ms)` 後讀 `srv.UDPAddr()` 的 race pattern。
- **Fix 2**：teardown 用 `done` channel 等 `Serve` 真的返回（內含 in-flight handler drain），再呼叫 `country.Close()` / `asn.Close()` 等清理；砍掉 `time.Sleep(20-30ms)` 祈禱式同步。
- **Fix 3**：`cmd/shadowdns/listenon_test.go` 的 logger sink 從共享 `bytes.Buffer` 換成 `go.uber.org/zap/zaptest/observer`；以 `observer.All()` / `observer.FilterMessage(...)` 取代 `buf.String() + strings.Contains`。順便檢查 `pprof_test.go` / `main_test.go` 是否有同 pattern。

## Non-Goals

- **不改 production code**：`internal/server/listener.go` 的 `Server.listeners` 雖在 race trace 中出現，但因 production 不會 runtime 重 bind，啟動後實質 immutable；不引入 atomic.Pointer 或 mutex。
- **不重構 miekg/dns 的 in-flight handler 機制**：仰賴 `dns.Server.Shutdown()` 的內建 drain 行為（透過 `Serve` 返回作為同步點即可）。
- **不改 `Makefile` 的 `test` target**：`-race` flag 已存在，本次只負責讓它真的能跑綠。
- **不引入新的 test framework**：沿用 `testing` + `zap`（zaptest/observer 是 zap 自帶 sub-package，無新依賴）。
- **不處理潛在的 production goroutine leak**（如果有的話）：本次 scope 限於 race detector 報出的 data race，goroutine leak 是另一個 concern。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `integration-test-harness`: 新增兩條 requirement，覆蓋 in-process server harness 的 teardown 同步機制以及測試 logger sink 的 thread safety。

（unit test 端的 internal/server 與 cmd/shadowdns 採相同修法，屬實作 detail，不獨立 spec coverage；詳見 Impact 段）

## Impact

- **Affected specs**: 無
- **Affected code**:
  - `internal/server/server_test.go` — `startTestServer` helper（Fix 1 + Fix 2）
  - `test/integration/helpers_test.go` — `newTestServer` helper（Fix 1 + Fix 2）
  - `cmd/shadowdns/listenon_test.go` — logger sink + log assertion（Fix 3）
  - `cmd/shadowdns/pprof_test.go`、`cmd/shadowdns/main_test.go` — 視檢查結果決定是否同步處理
- **Affected dependencies**: 無新增（`go.uber.org/zap/zaptest/observer` 為 zap 既有 sub-package）
- **Risk**: 低 —— 只動 test code，CI 跑綠即驗證完成
