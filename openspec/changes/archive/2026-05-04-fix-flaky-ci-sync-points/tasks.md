## 1. Add ReadyCh sync hook to runOptions / run() (Flaky #1)

- [x] 1.1 在 `cmd/shadowdns/main.go` 的 `runOptions` struct 新增欄位 `ReadyCh chan<- struct{}` 並補一行 doc comment 說明「optional; if non-nil, run() closes it once the SIGHUP handler is installed; production callers leave it nil」。
- [x] 1.2 在 `cmd/shadowdns/main.go::run()` 內，找到 `signal.Notify(sighupCh, syscall.SIGHUP)` 那一行，緊接其後加入 `if opts.ReadyCh != nil { close(opts.ReadyCh) }`；放在 `defer signal.Stop(sighupCh)` 之後、啟 sighup goroutine 之前，確保 close 時點明確等同「SIGHUP handler 已 attach」。
- [x] 1.3 確認 `newRootCmd().RunE` 與其他生產 callers 都不曾設 `opts.ReadyCh`（grep 一遍 `cmd/shadowdns/`）；新欄位不影響 existing test 的 `runOptions{}` 字面量初始化。

## 2. Migrate TestSIGHUP_ReloadIntegration to ReadyCh (Flaky #1)

- [x] 2.1 [P] 在 `cmd/shadowdns/main_test.go::TestSIGHUP_ReloadIntegration` 內，把 `runOptions{...}` 字面量加上 `ReadyCh: make(chan struct{})`（保留欄位，並把 channel 變數抓在 outer scope 變數，命名 `readyCh`）。
- [x] 2.2 [P] 把該 test 內 `time.Sleep(200 * time.Millisecond)` 那一行（位於 `go func() { done <- run(ctx, opts) }()` 之後、第一個 DNS query 之前）改成 `select { case <-readyCh: case <-time.After(5 * time.Second): t.Fatalf("server did not become ready in 5s") }`。5 秒 ceiling 是上界（CI 上 run() 起飛 < 1 秒就完成 signal.Notify），在 hang 時提供清楚 error 而非 timeout 後 zero-info。
- [x] 2.3 在同檔內 grep 其它使用 `time.Sleep(200 * time.Millisecond)` 等啟動 pattern 的 test（若存在），同樣改用 ReadyCh + bounded select。若沒有別的就在 commit message 註明「目前同檔僅 TestSIGHUP_ReloadIntegration 走此 pattern」。

## 3. Add gatherWithRetry helper for metric polling (Flaky #2)

- [x] 3.1 在 `internal/server/server_test.go` 內新增 unexported helper `gatherMetricFamilyWithRetry(t *testing.T, m *metrics.Metrics, name string, timeout time.Duration) *dto.MetricFamily`：迴圈每 10ms 呼叫 `m.Gather()`、把 result map 成 `map[name]*MetricFamily`、若找到 `name` 則 return；達 timeout 仍找不到 → `t.Fatalf("metric %q not gathered within %v", name, timeout)`；用 `t.Helper()` 標記讓 stack trace 指向呼叫端。
- [x] 3.2 預設 timeout 200ms 寫進 helper 簽名（呼叫端可顯式覆寫）；helper 不依賴 `time.Tick`（避免 goroutine 洩漏），改用 `time.NewTimer` + `time.After` 顯式 close。

## 4. Migrate TestServeDNS_Metrics_RecordsRequestAndResponse to helper (Flaky #2)

- [x] 4.1 在 `internal/server/server_test.go::TestServeDNS_Metrics_RecordsRequestAndResponse` 內，把直接呼叫 `m.Gather()` + 從 `families` map 找 `shadowdns_dns_requests_total` 那段（line ~1419-1432）改成 `reqMF := gatherMetricFamilyWithRetry(t, m, "shadowdns_dns_requests_total", 200*time.Millisecond)`，移除原本的 `families` 中介 map 與「not found」`t.Fatal`。
- [x] 4.2 同 test 對 `shadowdns_dns_responses_total` 的 lookup（line ~1438-1442）也換成 helper 呼叫；後續 label 檢查邏輯保留不動。

## 5. Verification

- [x] 5.1 [P] 跑 `go test -race -count=10 ./cmd/shadowdns/...` 全綠（10 次都不應 hang / die）。
- [x] 5.2 [P] 跑 `go test -race -count=10 ./internal/server/...` 全綠。
- [x] 5.3 [P] 跑 `make lint` 無新增 warning。
- [x] 5.4 push 到一個 throwaway branch（或重 push 當前 PR）觀察 CI run，確認該 PR 連續 3 次 push 對應的 CI run 中這兩個 test 不再失敗。
