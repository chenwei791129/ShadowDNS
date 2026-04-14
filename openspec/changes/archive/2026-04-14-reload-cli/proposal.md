## Why

ShadowDNS 已支援 SIGHUP reload，但操作者必須手動找到 PID 並執行 `kill -HUP <pid>`。新增 `-reload` CLI flag 可以讓操作者直接執行 `shadowdns -reload -named-conf /path/to/named.conf`，自動從 named.conf 的 `pid-file` option 讀取 PID 並送 SIGHUP，與 BIND 的 `rndc reload` 類似的操作體驗。

## What Changes

- `OptionsBlock` 新增 `PidFile string` field，解析 named.conf 的 `pid-file` option（目前被 warn + skip）
- `run()` 在 listener bind 後寫 PID file，shutdown 時 defer 刪除
- 新增 `-reload` CLI flag：解析 named.conf → 讀取 PID file → `kill(pid, SIGHUP)` → exit
- PID file 格式為標準 Unix 格式（純文字 PID number + newline）

## Capabilities

### New Capabilities

- `pid-file`: server 啟動時寫 PID file、shutdown 時刪除，以及 `-reload` flag 讀取 PID file 送 SIGHUP 的完整 lifecycle

### Modified Capabilities

（無）

## Impact

- 受影響程式碼：
  - `internal/config/options.go` — `OptionsBlock` 新增 `PidFile` field + 解析 `pid-file` option
  - `cmd/shadowdns/main.go` — 新增 `-reload` flag、PID file 寫入/刪除邏輯
