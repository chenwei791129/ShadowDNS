## 1. 審視現況與統一入口

- [x] 1.1 grep `test/integration/` 下所有直接呼叫 `net.ListenPacket("udp", "127.0.0.1:0")` 或 `exec.Command(.*shadowdns.*)` 的位置，列出 call site 清單（對應 Requirement: Integration test harness SHALL provide a single entry point for launching shadowdns）
- [x] 1.2 確認 `startShadowDNS` 是目前唯一的 launch 入口；若 `helpers_test.go` 或其他 file 有平行路徑，在此 task 中記錄並納入後續統一

## 2. 實作 Decision 1: 採用 retry-with-fresh-port 作為主要修法

- [x] 2.1 在 `test/integration/notify_test.go`（或獨立 `test/integration/launcher_test.go`）實作 `startShadowDNSWithRetry(t, namedConf, extraArgs...)`，內部包既有的 `freeLoopbackPort` 與 `exec.Command` 啟動流程，落地 Decision 1: 採用 retry-with-fresh-port 作為主要修法
- [x] 2.2 實作 Decision 4: 偵測 child 啟動成功的訊號 — tail child 的 stdout/stderr，出現 `"shadowdns ready"` 即回傳、窗口 3 秒

## 3. 實作 Decision 2: retry 觸發條件與上限

- [x] 3.1 實作 retry 觸發條件（Decision 2: retry 觸發條件與上限）：偵測 stderr 出現 `"address already in use"` 或 `"bind: no listeners bound"` 即進 retry，對應 Requirement: Integration test harness SHALL launch shadowdns binary without port-allocation races 的「First-attempt bind loses the race」scenario
- [x] 3.2 設定 retry 上限 = 3 次（首次 + 2 次 retry），超過即 `t.Fatalf` 並印出三次 attempt 的完整 log，落地 Decision 2: retry 觸發條件與上限 並對應「Repeated retries exhaust the budget」scenario

## 4. Decision 3: 統一 helper 位置與命名 + 子行程生命週期管理

- [x] 4.1 依 Decision 3: 統一 helper 位置與命名 規範新 helper 為唯一入口；其他 test file 只能透過它啟動 shadowdns
- [x] 4.2 [P] retry 前：`cmd.Process.Signal(SIGKILL)` 並 `cmd.Wait()` 收屍失敗 child，對應 Requirement: Integration test harness SHALL clean up child processes on retry and on test completion 的「Retry path kills the losing child」scenario
- [x] 4.3 [P] 測試結束時：既有 `cleanup` callback 保留 SIGTERM + Wait 路徑，同樣對應 Requirement: Integration test harness SHALL clean up child processes on retry and on test completion 的「Test completion reaps the child」scenario
- [x] 4.4 [P] 加 guard：若 retry 過程中 child 在「尚未觀察到成功或失敗訊號」就自己 exit，視為 flaky child crash、也算一次失敗並嘗試下一輪，補強 Requirement: Integration test harness SHALL clean up child processes on retry and on test completion 的防護

## 5. 替換既有 call site

- [x] 5.1 把 `test/integration/notify_test.go` 內所有 `startShadowDNS` 呼叫改為 `startShadowDNSWithRetry`（或直接就地重寫 `startShadowDNS`，讓 rename 非必要）
- [x] 5.2 若 step 1.1 發現 `helpers_test.go` 或其他 test file 有平行 launch 路徑，全部收斂到同一入口
- [x] 5.3 grep 確認 `test/integration/` 內沒有殘留的 ad-hoc `net.ListenPacket("udp", "127.0.0.1:0")` 搭配 `exec.Command(.*shadowdns.*)` 的組合，對應 Requirement: Integration test harness SHALL provide a single entry point for launching shadowdns 的「Existing test launchers are migrated」scenario

## 6. 驗證

- [x] 6.1 `go test -run TestIntegration -count=50 ./test/integration/` 本地跑 50 輪全綠
- [x] 6.2 Push 到 feature branch，連續觀察 3 次 CI run 全綠（不一定要連續 3 次 push，可用 `gh run rerun` 累積 3 次綠）
- [x] 6.3 `make lint` 零 issues、`make test` 零失敗
