## Summary

修復 `openspec/specs/zone-transfer/spec.md` 內因前次 archive sync bug 造成的整檔 Requirements 重複，並將 notify-toggle 的 `--no-notify` flag / `options.notify` config directive 場景補回新版 NOTIFY Requirement，與 glue resolution 場景並列。

## Motivation

`spectra archive notify-toggle` 在 2026-04-15 落地時誤把 zone-transfer 的全部 6 個 Requirements 整段附加（line 573 起）到主 spec 末尾，而不是替換對應條目，導致主 spec 同時存在兩份完整的 Requirements 區塊：

- **第一份（L3-572）**：每個 Requirement 都有完整的 `<!-- @trace -->` 區塊，記錄 `shadowdns-foundation` → `notify-toggle` → `notify-glue-resolution` 的修改軌跡。`Send NOTIFY on zone content change` 仍停留在 foundation 階段的版本（3 個基礎 scenarios，無 toggle、無 glue）
- **第二份（L697-809）**：純 Requirements 沒有 @trace。`Send NOTIFY` 是 `notify-glue-resolution` archive sync 後的最新版本（6 個 glue-resolution scenarios）

兩份內容描述同一個 capability、規格讀者會困惑、未來 archive sync 行為更難預期。同時，notify-toggle 在歸檔過程中導入的 6 個 toggle 行為 scenarios（`--no-notify`、`options.notify`、SIGHUP 後行為等）在前次 sync 時被 glue-resolution delta 整段重寫蓋掉了——雖然程式碼層 toggle 功能仍正常運作，但 spec 已不再記錄這些行為。

## Proposed Solution

純 spec 文件層的修復，不動程式碼：

1. **保留第一份**（L3-572）作為 canonical 來源——它已經包含完整 @trace audit；以原地編輯方式把其中 `Send NOTIFY on zone content change` 的 Requirement 內文與 scenarios 更新為「glue-only resolution + notify-toggle」兩組行為的合併版本：
   - 維持 `notify-glue-resolution` 帶來的 6 個 scenarios（in-zone glue / multi-IP glue / no-glue skip / retry / MNAME exclusion / cross-view dedup）
   - 補回 `notify-toggle` 帶來的 6 個 scenarios（disabled by CLI flag / disabled by config / enabled by default / CLI overrides config / CLI persists across SIGHUP / config takes effect on SIGHUP reload）
   - Requirement 本文同時描述 toggle gate 與 glue-only resolution 兩個面向
2. **刪除第二份**（L697 起到 EOF）整個尾段重複區塊

不引入 spec delta 檔案（`specs/` 子目錄留空），因為這是 main spec 自身結構修復而非 capability 演進；下次 archive 時 sync 邏輯就不會試圖再次套用任何 delta、也就不會再觸發附加 bug。

## Non-Goals

- **不**修改任何程式碼、測試、`README.md`、build 系統
- **不**追溯修補其他可能有相同 archive sync bug 的 spec 檔（若有，另開 cleanup change 各別處理）
- **不**改變 notify-toggle 或 notify-glue-resolution 任一既有 Requirement 的可觀察行為
- **不**重寫 @trace block 內容或追加新的 trace 來源

## Alternatives Considered

- **保留第二份、補 @trace 後刪第一份**：需要從 git history 重建 3 個 @trace block 的完整 file 清單，工作量大且容易出錯。第一份已是正確 audit 狀態，反向操作划不來。
- **以正式 spec delta 重寫 NOTIFY Requirement**：先前 sync 行為證明 archive 階段的 delta 套用對「同 Requirement name 出現兩次」會誤判，本次目標就是讓 main spec 回到單一 Requirement 的乾淨狀態。delta 在這個過渡期反而會把問題搞複雜，因此選擇直接編輯 main spec。
- **以單純刪除第二份完成清理**：可選，但會丟失 notify-toggle 場景描述。同時補回 toggle 場景是 propose 一併處理較合理（同檔同段、避免再開一次 change）。

## Impact

- Affected specs: zone-transfer（main spec 文件結構修復 + Send NOTIFY Requirement scenarios 合併）
- Affected code:
  - Modified:
    - openspec/specs/zone-transfer/spec.md
