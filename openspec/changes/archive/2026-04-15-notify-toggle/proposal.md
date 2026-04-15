## Why

目前 ShadowDNS 啟動與 SIGHUP reload 後會**無條件**對所有 zone 的 NS target 發送 NOTIFY，這個行為完全沒有開關。對於沒有 secondary 的單機部署、測試環境、或刻意想抑制啟動噪音的場景，沒有停用的正規方式——只能透過刪 NS record 之類會破壞 DNS 語意的 hack。此外，每個 NS target 會重試 3 次（1s + 2s + 4s backoff），若下游不可達，每個 zone × target 會佔用 ~7 秒與一條 goroutine，對啟動時間與執行緒數量都有可觀察的影響。

## What Changes

- 在 `options { ... }` 區塊新增 `notify yes|no;` directive（BIND 相容子集）
- 新增 CLI flag `-no-notify`，僅在**顯式傳遞**時生效
- 行為優先順序：顯式 `-no-notify` > config `options.notify` > default(`true`)
- CLI flag 為 process-lifetime sticky：若啟動時帶了 `-no-notify`，後續 SIGHUP reload 即使 config 改為 `notify yes;` 也**不會**恢復發送
- 當 notify 關閉時，`dispatchNotifies()` 兩處呼叫點（啟動、reload）都跳過，不產生 goroutine、不重試、不記 log
- 預設行為不變：未設 flag、未設 config 時，仍為 `notify yes`（維持現有 SHALL 行為，避免既有部署 regression）

## Non-Goals

- **不支援** `notify explicit`（BIND 的「只對 also-notify 發」模式）——本專案無 `also-notify`
- **不支援** zone-level `notify` override——維持全域單一設定，降低認知負擔
- **不新增** `also-notify { ... }` directive——已列入 README 的 future work，與本 change 正交

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `zone-transfer`: NOTIFY 從強制（SHALL）放寬為「SHALL by default, MAY be disabled via config 或 CLI flag」；新增停用時的 scenario
- `config-loader`: `options { ... }` 支援清單新增 `notify` key，值為 `yes|no`

## Impact

- **Affected specs**: `zone-transfer`、`config-loader`
- **Affected code**:
  - [cmd/shadowdns/main.go](cmd/shadowdns/main.go) — 新增 `-no-notify` flag、`flag.Visit` 偵測、`resolveNotifyEnabled()` 工具函式、兩處 `dispatchNotifies` 前加 guard
  - [internal/config/options.go](internal/config/options.go) — `OptionsBlock` 新增 `Notify *bool` 欄位、`case "notify"` 解析分支
  - [internal/config/options_test.go](internal/config/options_test.go) — 新增 `notify yes` / `notify no` / 未設值的解析測試
  - [packaging/named.conf.example](packaging/named.conf.example) — 加一個註解範例說明 `notify` directive
  - [README.md](README.md) — 更新 NOTIFY 段落，說明新的開關
