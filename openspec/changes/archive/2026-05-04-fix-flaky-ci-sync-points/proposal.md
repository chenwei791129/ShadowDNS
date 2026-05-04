## Problem

PR #26（feat(prune-backup)）期間累計 4 次 CI run，其中 2 次無故失敗，本地 `go test -race` 連跑都綠。失敗模式有兩種，皆與 PR diff 無關：

1. **`cmd/shadowdns.TestSIGHUP_ReloadIntegration`**：CI run 25298601729（首次 fixup 後）失敗，整個 test binary 直接 die，stderr 只有 `signal: hangup` + `FAIL github.com/chenwei791129/ShadowDNS/cmd/shadowdns 10.510s`，沒有任一 `--- FAIL: TestXxx` 行。代表 process 是被 SIGHUP 直接 terminate，不是 test assertion fail。
2. **`internal/server.TestServeDNS_Metrics_RecordsRequestAndResponse`**：CI run 25304256726 失敗，明確錯誤訊息 `server_test.go:1432: shadowdns_dns_requests_total not found`。

兩次都靠 re-run 通過，但這兩個 test 仍會在後續任何 PR 隨機重現，把「CI 紅燈 = 此 PR 有問題」的訊號可信度拉到接近 0。

## Root Cause

**Flaky #1（SIGHUP）**：`cmd/shadowdns/main_test.go::TestSIGHUP_ReloadIntegration` 用 `go run(ctx, opts)` 在背景啟動 server，hardcode `time.Sleep(200 * time.Millisecond)` 等啟動完成，然後 `syscall.Kill(syscall.Getpid(), syscall.SIGHUP)` 觸發 reload。可是 `run()` 在 `cmd/shadowdns/main.go` 中真正執行 `signal.Notify(sighupCh, syscall.SIGHUP)` 是在 BindMany / dispatchNotifies / WritePidFile 之後（line ~459），啟動鏈很長。雖然 test 在送 SIGHUP 前還會先做一次 DNS query 確認 server 已 ready，但 query 成功只證明 `srv.Serve(ctx)` 已開始接受 packet — 這跟 `signal.Notify` 是否已實際 attach 到 Go signal subsystem 不是同一個 happens-before 關係。CI runner 慢 + Go runtime signal 註冊與 query path 啟動的相對順序，使 `signal.Notify` 偶爾在 SIGHUP 已送出後才真正生效，default action（terminate）就贏了。

**Flaky #2（metrics）**：`internal/server/server_test.go::TestServeDNS_Metrics_RecordsRequestAndResponse` 在 `query()` return 後立刻呼叫 `m.Gather()`，並斷言 `shadowdns_dns_requests_total` 一定要存在。但 server 端的 DNS handler 是 `miekg/dns` server 為每個 query spawn 的 goroutine，metric increment 與 response write 的相對順序由 handler 實作決定。`dns.Exchange` 在 client 收到 response 即 return — 此時 server handler goroutine 不保證已完成 metric increment（即使 increment 排在 write 之前，atomic store 對其他 goroutine 可見性也不是 wire-order 等同）。CI runner 慢時這個窗口擴大到 race 暴露。

兩者共同的本質：test 與背景 goroutine 之間缺乏 explicit happens-before 同步點，靠 sleep / 自然順序碰運氣。

## Proposed Solution

**Flaky #1 修法（cmd/shadowdns）**：
1. 在 `cmd/shadowdns/main.go` 的 `runOptions` 結構新增一個可選欄位 `ReadyCh chan<- struct{}`（默認 zero value = nil）。
2. 在 `run(ctx, opts)` 內的 `signal.Notify(sighupCh, syscall.SIGHUP)` 之後（亦即 SIGHUP handler 已 attach 那一刻），若 `opts.ReadyCh != nil` 就 `close(opts.ReadyCh)`（或非 blocking send）。
3. main 入口（`cobra` `RunE`）建立 `opts` 時不設 `ReadyCh`，行為完全不變（生產 binary 不在乎這個欄位）。
4. `cmd/shadowdns/main_test.go::TestSIGHUP_ReloadIntegration` 改成：建立 `readyCh := make(chan struct{})`、`opts.ReadyCh = readyCh`、`go run(ctx, opts)`、`<-readyCh` 取代 `time.Sleep(200 * time.Millisecond)`、然後送 SIGHUP。其它使用 sleep 等啟動的相同 pattern test 一併改。

**Flaky #2 修法（internal/server）**：
1. 在 `internal/server/server_test.go` 新增 test-internal helper `gatherWithRetry(t, m, name, timeout)`：每 10ms 嘗試一次 `m.Gather()`，找到目標 metric family 即返回，最長等 `timeout`（預設 200ms）才 fail。
2. 把 `TestServeDNS_Metrics_RecordsRequestAndResponse` 內目前直接 `m.Gather()` 的兩處（找 `shadowdns_dns_requests_total` 與 `shadowdns_dns_responses_total`）換成這個 helper。
3. 不改 production handler 的 metric increment 順序 — 保持 minimum invasive。

## Non-Goals

- 不調整 production handler 的 metric increment 與 response write 的順序（風險偏高、為了測試改 hot path 不划算）。
- 不引入新的 metric flush / barrier 機制。
- 不擴大 `runOptions` 成 test-only injection 框架；只加 `ReadyCh` 一個欄位，且僅在 SIGHUP handler attach 後 fire（語意明確、不擴張）。
- 不修改任何 spec — 純 internal testability fix，不會走 capability spec delta。
- 不改其它 flaky test（如果之後出現新的 flake，另開 change）。

## Success Criteria

1. `cmd/shadowdns/main_test.go::TestSIGHUP_ReloadIntegration` 不再依賴 `time.Sleep(200ms)` 等啟動，`signal: hangup` 的 process-level kill 在 100 次 CI re-run 中不應再出現。
2. `internal/server/server_test.go::TestServeDNS_Metrics_RecordsRequestAndResponse` 在 metric 暫不可見時透過 polling helper 等到，最壞情況拖到 timeout 才 fail；正常情況下不應再見 `shadowdns_dns_requests_total not found` 一發即斷的失敗。
3. 生產 binary（`shadowdns` 主流程）行為完全不變：`runOptions` 沒設 `ReadyCh` 時整段邏輯與舊版等價、無額外 syscall、無額外 channel send。
4. 本 change 落地後，跑 `go test -race -count=10 ./cmd/shadowdns/... ./internal/server/...` 全綠。
5. 在 PR #26 之後的每個 PR CI run（每 PR 至少 3 次 push 的累計樣本），這兩個 test 不再隨機失敗。

## Impact

- Affected code:
  - Modified:
    - `cmd/shadowdns/main.go` — `runOptions` 新增 `ReadyCh chan<- struct{}` 欄位；`run()` 在 `signal.Notify` 後 close（或非 blocking send）該 channel。
    - `cmd/shadowdns/main_test.go` — `TestSIGHUP_ReloadIntegration` 用 `ReadyCh` 取代 `time.Sleep`；同檔內任何走相同「sleep 200ms 等啟動」pattern 的 test 一併移植。
    - `internal/server/server_test.go` — 新增 `gatherWithRetry` 私有 helper（test-only）；`TestServeDNS_Metrics_RecordsRequestAndResponse` 改用該 helper。
  - New: (none)
  - Removed: (none)
