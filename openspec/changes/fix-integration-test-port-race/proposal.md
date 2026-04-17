## Why

`test/integration/notify_test.go` 的 `freeLoopbackPort` helper 透過「bind `127.0.0.1:0` → 記下 OS 分配的 port → Close 釋放 → 把 port 傳給 exec 出去的 shadowdns binary」取得可用 port，但 Close 與 child process bind 之間存在一個 race window：別的平行測試或系統其他 process 可能搶先 bind 同一個 port，導致 child 啟動失敗並留下 `bind: address already in use` 錯誤。

實際已多次在 CI 上命中：`TestIntegration_ConfigNo_SuppressesAllSends` 及其他透過 `startShadowDNS` 啟動外部 binary 的 notify 測試偶發性失敗。helper 自己的 godoc 也承認此 race（"There's an inherent race between close-and-bind, but in practice the OS keeps the port unused long enough"）。結果是 CI flaky 會干擾 PR 合入流程、拖慢開發速度。

## What Changes

- 重寫 `test/integration/notify_test.go` 的 `freeLoopbackPort` 或改寫 `startShadowDNS`，以 race-free 方式把 port 交給 child process
- 採用 **parent pre-bind + ExtraFiles** 方案作為預設：parent 在 listen 後不 Close，而是把已 bind 的 UDP/TCP file descriptor 透過 `exec.Cmd.ExtraFiles` 傳給 child；對應需要 shadowdns 支援「從繼承的 fd 啟動 listener」的 flag（例：`-listen-fd=3,4`），或由測試改用別的 launcher pattern
- 若 parent pre-bind 方案對 shadowdns main package 侵入過大，退回 **retry-with-backoff** 方案：測試在 start child 後若 stderr 出現 `address already in use`，就重選一個新 port 重啟，最多重試 N 次
- 同步掃 `test/integration/helpers_test.go` 與其他 test file，把所有「取 port → Close → 交給 child」的呼叫點統一走新 helper
- 在 CI（`.github/workflows/ci.yml`）加入 `-count=3` 的 notify 測試 smoke 跑，降低同類 race 再度進入 main 的機率（選用，design 時評估成本）

## Non-Goals

- 不改動 shadowdns runtime 的 port binding 語意（只新增「吃 inherited fd」的啟動路徑，原本的 `-listen host:port` 行為不變）
- 不重寫 integration test 成「純 in-process」形式（那是更大的重構，另開 change）
- 不改動既有測試案例的斷言邏輯，只改 port 取得方式
- 不處理非 notify 測試的其他 flakiness（例如 GeoIP MMDB 讀取、TLS handshake 等），範圍只限「啟動 shadowdns binary 時的 port 衝突」

## Capabilities

### New Capabilities

- `integration-test-harness`: 規範 `test/integration/` 底下跨 test file 共用的測試基礎設施——尤其是「啟動 shadowdns binary 並分配 listener port」這條路徑——必須提供 race-free 的 port 取得機制，保證 CI 上平行執行時不會因 port 衝突而偽陽性失敗。

### Modified Capabilities

(none)

## Impact

- Affected specs: 無
- Affected code:
  - `test/integration/notify_test.go` — 重寫 `freeLoopbackPort` / `startShadowDNS`
  - `test/integration/helpers_test.go` — 若有類似 port 分配路徑則同步修正（待 design 階段確認）
  - `cmd/shadowdns/main.go` — **僅在走 fd-inheritance 方案時**新增 `-listen-fd` flag 與對應 listener 初始化；若走 retry 方案則無改動
  - `.github/workflows/ci.yml` — 選用：新增 notify 測試 `-count=3` smoke run
- 不影響：任何 runtime 行為、封裝產物（`.deb`）、設定檔格式
- 風險：若走 fd-inheritance 方案，需謹慎處理 fd 編號在 `exec.Cmd` 中的 offset（`ExtraFiles[0]` 會是 fd 3）以及跨平台（macOS/Linux）差異；design 階段須明確規範
