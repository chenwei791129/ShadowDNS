## 1. Server State 架構重構

- [x] 1.1 Atomic Pointer 取代直接嵌入：修改 `internal/server/server.go` 中的 `Server` struct，將嵌入的 `ServerState` 改為 `state atomic.Pointer[ServerState]`，新增 `SwapState(new ServerState)` method 呼叫 `Store()`，更新 `NewServer` 以 `Store()` 初始化 state pointer（涵蓋 Zero-downtime state replacement、Server 提供 SwapState method）
- [x] [P] 1.2 ServeDNS 每次進入時 Load state：修改 `internal/server/handler.go` 中的 `ServeDNS`、`handleRootQuery`、`handleBackupQuery`、`handleTransfer`、`negativeReply` 方法，在進入點呼叫 `s.state.Load()` 取得 `*ServerState` snapshot，後續使用 snapshot 的欄位而非 `s.XXX`（涵蓋 Reload does not restart listeners — listener 不變，僅 state 替換）
- [x] [P] 1.3 SIGHUP 監聽位於 main.go 的 run()：在 `cmd/shadowdns/main.go` 的 `run()` 中新增獨立的 `signal.Notify` channel 監聽 `syscall.SIGHUP`，實作 reload 函式，流程為：log reload 開始 → `config.LoadNamedConf()` → `config.LoadAliases()` → `server.BuildState()`（重用啟動時的 GeoIP handles，涵蓋 GeoIP databases are not reloaded）→ `srv.SwapState()` → `dispatchNotifies()`（涵蓋 NOTIFY dispatch after successful reload）→ log reload 成功。任何步驟失敗時 log error 並 continue（涵蓋 SIGHUP triggers configuration reload、Reload failure preserves existing state、Reload logging）

## 2. 測試

- [x] [P] 2.1 Server atomic state swap unit test：在 `internal/server/` 新增測試，驗證 `SwapState` 後 `ServeDNS` 使用新 state 回應查詢，並驗證 swap 期間 in-flight 查詢使用舊 state snapshot（涵蓋 Zero-downtime state replacement）
- [x] [P] 2.2 Reload 成功/失敗 unit test：在 `cmd/shadowdns/` 新增測試，驗證 reload 成功時 state 更新且 NOTIFY 被 dispatch，reload 失敗時（如 zone file 語法錯誤）舊 state 保留且 error 被 log（涵蓋 Reload failure preserves existing state、Reload logging、NOTIFY dispatch after successful reload）
- [x] 2.3 SIGHUP reload integration test：在 `test/integration/` 新增測試，啟動 server → 修改 zone file → 送 SIGHUP → 驗證新查詢回傳更新後的資料（涵蓋 SIGHUP triggers configuration reload 端對端驗證）
