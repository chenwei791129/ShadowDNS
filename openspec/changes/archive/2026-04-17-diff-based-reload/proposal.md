## Why

載入 3609 個 root zone 時 ShadowDNS 佔用約 10GB 記憶體，SIGHUP reload 會讓 peak 暫時飆到 20GB，再等 GC 慢慢回收。根因是 `BuildState()` 每次都從零把所有 zone file 重新 parse 成新的 `ServerState`，在 `SwapState()` 原子替換之前新舊兩份資料完整並存。大型部署的 memory headroom 必須預留一倍才能安全 reload，浪費明顯。實務上每次 reload 通常只有少數 zone 真的變更，沒有理由重建未變更的 zone 物件。

## What Changes

- 在 `BuildState()` 增加 `prev *ServerState` 參數，比對前後 fingerprint，**未變更的 zone 直接 reuse 舊 `*zone.Zone` pointer**，不重 parse
- 以 `(size, xxhash64)` 組合為 zone file 的 fingerprint：`size` 當 pre-filter，真正判斷依據是 xxhash64 content hash
- 採用 `github.com/cespare/xxhash/v2` 作為 hash 演算法（純 Go + amd64/arm64 assembly 加速）
- 新增 CLI flag `-reload-verify=hash|size|none`（預設 `hash`）：
  - `hash`：size pre-filter + xxhash64 content 驗證（預設，適用於 rsync `-a` 保留 mtime 的 release 流程）
  - `size`：僅比對 mtime+size（快、但 source 保留 mtime 時會漏偵測）
  - `none`：退回 full rebuild（escape hatch，與優化前行為相容）
- 首次啟動時 `prev == nil`，自動退回 full rebuild 路徑（維持現有行為）
- `SwapState()` 之後呼叫 `runtime.GC()` + `debug.FreeOSMemory()`，縮短新舊並存期間、盡快歸還 memory 給 OS
- Reload 失敗時（任何 zone parse 失敗）整體 rollback，不觸發 `SwapState()`，舊 state 完全保留

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `sighup-reload`: reload 流程從 full rebuild 改為 diff-based — 未變更 zone 共用 pointer；新增 `-reload-verify` 策略旋鈕；reload 完成後主動觸發 GC

## Impact

- Affected specs: `sighup-reload`
- Affected code:
  - [cmd/shadowdns/main.go](cmd/shadowdns/main.go) — 新增 `-reload-verify` flag，穿透到 `reload()` 與 `BuildState()`
  - [internal/server/build.go](internal/server/build.go) — `BuildState()` 簽名新增 `prev *ServerState` 與 verify mode 參數，實作 diff 邏輯
  - [internal/server/server.go](internal/server/server.go) — `SwapState()` 之後觸發 GC
  - 新檔案 `internal/server/fingerprint.go` — zone file fingerprint（size + xxhash64）與比對邏輯
- Dependencies:
  - 新增 `github.com/cespare/xxhash/v2`
- Operational：
  - 預設行為仍是安全的（`hash` 模式偵測內容變更，符合目前 rsync `-avc --inplace` 流程）
  - 運維可透過 `-reload-verify=none` 立即 fallback 到舊行為作為 escape hatch
