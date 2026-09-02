## Why

在編排式部署（rolling deployment、外部負載平衡器、服務端點尚未收斂）中，ShadowDNS 行程一啟動就會綁定 HTTP-01 listener 並立刻建立 ACME order。此時公開端點可能還沒路由到這個新實例，或仍被導向舊實例——而 HTTP-01 challenge token 是行程內記憶體本地的，驗證請求必須打到持有該 token 的那個實例。結果是第一張正式憑證的 order 幾乎注定失敗，要等既有 retry 迴圈（10 分鐘間隔）才會補回來。

這個首次失敗是可避免的，而且它會消耗 ACME CA 的驗證額度。給運維一個明確、有界的「傳播等待窗口」，比起導入共享 challenge 儲存或耦合特定 orchestrator / 負載平衡器 API，是成本低得多的解法。

## What Changes

- 新增選填設定欄位 `doh.acme.initial_delay`，型別為 Go duration 字串（例如 `30s`），預設 `0s`。
- 設定載入階段解析該欄位：缺漏或空字串視為 `0`；無法解析為 duration、或解析出負值時，載入失敗並回報具名錯誤。不設上限值檢查。
- DoH HTTPS 與 ACME HTTP-01 兩個 listener 的啟動流程完全不變；延遲只發生在憑證取得迴圈進入第一次 obtain 之前。
- 等待可被 context 取消：關機期間取消不會阻塞，也不會嘗試發憑證、不會記錄假的 renewal failure 指標。
- 延遲**只**作用於該行程的第一次 obtain；失敗後的 retry 間隔與續期排程完全不受影響。
- 生效值為正時，延遲開始以 Info 等級記錄一筆日誌，包含設定的 duration，不記錄任何 challenge 材料；生效值為零時不記錄該筆日誌。
- 省略該欄位或設為 `0s` 時，行為與現況完全相同。

## Non-Goals

- 不引入 listener bind-ready 訊號：延遲的計時起點是憑證迴圈進入時，而非「兩個 listener 確認完成 bind 之後」。要做到後者必須讓共用的 HTTP server 原語對外暴露 bind-ready channel，改動範圍遠大於本變更，而實務上兩者差距是毫秒級，對秒級的傳播窗口無意義。
- 不設 `initial_delay` 上限：誤設過大的後果（DoH handshake 在該期間持續失敗）明顯且可自我修正，額外的上限是憑空發明政策。
- 不新增 Prometheus 指標：延遲是設定驅動的確定性行為，既有的 renewal success/failure 指標與 Info 日誌已足夠觀測。
- 不改變 SIGHUP 語意：`initial_delay` 與其他 `doh.acme.*` 欄位一樣是 restart-only，reload 時只會納入既有的 DoH 設定漂移提示。
- 不導入共享 challenge token 儲存，也不與任何 orchestrator / 負載平衡器 API 整合。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `doh-endpoint`: DoH 設定欄位表新增選填的 `doh.acme.initial_delay`（含負值與格式錯誤的載入失敗行為），並新增一項需求規範首次 ACME 發憑證的延遲、其可取消性，以及它不影響 retry / 續期時序。

## Impact

- Affected specs: `doh-endpoint`
- Affected code:
  - Modified:
    - `internal/shadowdnscfg/config.go`
    - `internal/doh/acme.go`
    - `internal/doh/server.go`
    - `internal/shadowdnscfg/doh_test.go`
    - `internal/doh/acme_test.go`
    - `docs/configuration/shadowdns-yaml.md`
    - `docs/configuration/shadowdns-yaml.zh.md`
    - `docs/guides/doh.md`
    - `docs/guides/doh.zh.md`
    - `packaging/shadowdns.yaml.example`
  - New: (none)
  - Removed: (none)
- 不影響 CLI flag 介面：`initial_delay` 只存在於 YAML，沒有對應的旗標。
- 不影響 SIGHUP reload 程式碼：DoH 設定漂移比較走整個 struct 值比較，`time.Duration` 為可比較型別，新欄位自動納入既有的 restart-to-apply 提示。
