## Why

ShadowDNS 目前啟動後載入所有設定與 zone 資料，一旦需要更新 zone file 或 aliases 設定，必須完整停止再重啟 process。這會導致 DNS 服務短暫中斷。加入 SIGHUP reload 功能可以在不中斷服務的前提下重新載入設定與 zone 資料，符合權威 DNS server（如 BIND、NSD）的業界慣例。

## What Changes

- 新增 SIGHUP 信號監聽：`run()` 中監聽 `syscall.SIGHUP`，觸發時重新載入 named.conf、aliases、zone files
- 將 `Server` 中的 `ServerState` 改為 `atomic.Pointer[ServerState]`，實現 zero-downtime state 替換
- `ServeDNS` 及所有讀取 state 的方法改為在每次進入時透過 `Load()` 取得當前 state
- Reload 成功後重新 dispatch NOTIFY 通知 slave server
- Reload 失敗時 log error 並保留舊 state 繼續服務

## Non-Goals

- **不做自動 hot reload**：不監控 zone file 的檔案系統變更（inotify/fsnotify），僅透過手動 SIGHUP 觸發
- **不重新載入 GeoIP 資料庫**：GeoIP mmdb 更新頻率低且載入成本高，維持啟動時載入一次的現行行為
- **不重啟 listener**：reload 僅替換 in-memory state，UDP/TCP listener 維持原本的 binding

## Capabilities

### New Capabilities

- `sighup-reload`: 收到 SIGHUP 信號時，重新載入設定（named.conf、aliases）與所有 zone files，以 atomic pointer swap 替換 server state，不中斷正在進行的 DNS 查詢

### Modified Capabilities

（無）

## Impact

- 受影響程式碼：
  - `cmd/shadowdns/main.go` — 新增 SIGHUP 監聽與 reload 流程
  - `internal/server/server.go` — `ServerState` 改為 atomic pointer，調整 `Server` 結構
  - `internal/server/handler.go` — `ServeDNS` 及相關方法改為從 atomic pointer 讀取 state
  - `internal/server/build.go` — 無需修改，`BuildState()` 已可獨立呼叫
