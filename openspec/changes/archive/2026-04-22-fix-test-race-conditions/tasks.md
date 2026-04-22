## 1. 盤點與基線

- [x] 1.1 跑 `go test -race -count=1 ./...` 並把完整輸出存到 `/tmp/race-baseline.txt`，確認當前 race warning 數（baseline = 1247）與失敗 package 清單
- [x] 1.2 grep 全 repo `time.Sleep` 在 `*_test.go` 內的所有出現，分類為「同步用途（要砍）」與「真實時序需求（保留）」並列清單
- [x] 1.3 grep `bytes.Buffer` 在 `cmd/shadowdns/*_test.go` 內當 logger sink 的所有用法（`pprof_test.go`、`main_test.go`、`listenon_test.go`）

## 2. 重構 in-process server harness

- [x] 2.1 [P] 實作 requirement「In-process server harness SHALL synchronize teardown with the server goroutine lifecycle」於 `test/integration/helpers_test.go` 的 `newTestServer`（順手把 `axfr_test.go` 與 `listenon_test.go` 的同 pattern 在地 helper 一起修）：主 goroutine 同步呼叫 `srv.Bind("127.0.0.1:0")`，再 `go func(){ defer close(done); srv.Serve(ctx) }()`；teardown 在 `<-done` 後才呼叫 `country.Close()` / `asn.Close()`；移除所有作為同步用途的 `time.Sleep`
- [x] 2.2 [P] 同樣 pattern 套用到 `internal/server/server_test.go` 的 `startTestServer`（unit test，沒有 mmdb close 步驟，但同樣需要 done channel 等 Serve 返回）
- [x] 2.3 在 `internal/server/server_test.go` 跑 `go test -race -count=1 -run TestSwapState_ConcurrentQueriesConsistent ./internal/server/`，確認該測試 0 race
- [x] 2.4 在 `test/integration/` 跑 `go test -race -count=1 -run TestWildcard_NODATA ./test/integration/`，確認該測試 0 race

## 3. 改寫 logger sink

- [x] 3.1 [P] 實作 requirement「Test helpers SHALL capture server log output through thread-safe sinks」於 `cmd/shadowdns/listenon_test.go` 三個 test：把 `bytes.Buffer` logger sink 換成 `zaptest/observer`（`go.uber.org/zap/zaptest/observer`），用 `observer.FilterMessage(...)` 或 `formatObserved(...)` helper 取代 `buf.String() + strings.Contains`
- [x] 3.2 [P] 視 1.3 結果：`pprof_test.go` 無 logger buffer（false match）；`main_test.go` 高風險 SIGHUP/poll 測試已用既有 `threadSafeBuffer`，其餘 `var buf bytes.Buffer` 為單 goroutine 寫入後 `<-done` 同步再讀（baseline 無 race），保留不改
- [x] 3.3 跑 `go test -race -count=1 ./cmd/shadowdns/`，確認 0 race 0 失敗

## 4. 驗證

- [x] 4.1 跑 `go test -race -count=1 ./...` 全套，確認 race warning = 0、test failures = 0（11/11 packages OK，1247 → 0）
- [x] 4.2 確認沒有引入新的 `time.Sleep` 當作同步機制（diff 檢查；剩餘 sleep 皆為 SIGHUP/reload real-time wait，原本就有）
- [x] 4.3 確認 production 檔案 (`internal/server/listener.go`、`internal/server/server.go`、`internal/server/handler.go`、`internal/view/*.go`、`cmd/shadowdns/main.go`) 沒被修改（git diff 限定路徑，僅 test 檔變更 + 使用者預先改好的 Makefile -race flag）
- [x] 4.4 跑 `make lint` 確認沒有引入新 lint 警告（0 issues）
