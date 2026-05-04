## Why

`shadowdns prune-backup` 在小型測試環境驗證沒問題，但在 ns2 production 規模（3000+ backup zones、19M+ 候選刪除）執行 dry-run 時：peak resident memory 約 13GB、stdout 印完後 process 仍卡 4+ 分鐘才退出（GC mark phase 在掃 19M `Deletion` 結構與 thousands-of-MB 的 `mergedFiles` byte slices）。流程結構性問題：所有 (view, backup) pair 的中間結果累積到末尾才一次印 / 寫，工具的記憶體 footprint 等於整個資料庫，不等於當下處理單位。

## What Changes

- 把 `cmd/shadowdns/prune_backup.go` 的 `runPruneBackup` 從「全部 pair plan → 集中排序 → 集中印 → 集中 apply」改成 **per-pair streaming**：每個 (view, backup) pair 跑完 PlanPair 就立刻在該 pair 範圍內排序、印出 dry-run 候選、（若 `--apply`）呼叫 `ApplyAll` 寫該 pair 的檔，然後丟掉 plan 讓 GC 回收。
- Pair 處理順序仍維持目前的 deterministic sort（view + backup origin），保證 dry-run 輸出順序與當前實作一致。
- 把寫入 `cmd.OutOrStdout()` 的 `fmt.Fprintf` 包成 `bufio.Writer`（64KB buffer），結束前 `Flush()`，把 19M 次 write syscall 壓成 thousand-scale。
- `runPruneBackup` 完成所有 pair 處理後直接 `os.Exit(0)`，跳過 Go runtime teardown 對殘留物件的最後一輪 GC scan；exit 路徑上的 `defer logger.Sync()` 改在 main loop 結束處顯式呼叫一次，移除之後就走 `os.Exit`。
- **行為差異記錄**：原本「全部 pair 的 zone 檔都先 parse + plan 完才開始任何寫入」被換成「per-pair plan→apply」。Fail-stop 語意實質一致（任一 pair 寫失敗即停、`.bak` 留下），但少了「先驗所有 zone 都能 parse 才動筆」的預檢；operator 的等價做法是先跑一次 dry-run 過 parse，再跑 `--apply`。Spec 中對 exit-code 條件「parse failure of a zone file exits non-zero」與「any file's write step fails」語意保持不變。

## Non-Goals

- 不改 RRSet 比對規則（`internal/prunebackup/diff.go` 完全不動）。
- 不改 `.bak` / atomic-write 機制（`internal/prunebackup/apply.go` 行為不變；上次發布的 mode-preservation fix 已就位）。
- 不改 exit-code 語意（dry-run 永遠 0、parse/load/apply 失敗非 0）。
- 不改 dry-run 輸出格式（`file:start-end owner type rdata` 一行一筆）。
- 不嘗試 streaming sort 跨 pair：跨 pair 的全域 `file:line` 排序在現實上沒有需求，因為不同 pair 不共用 backup file（每個 backup zone 屬於唯一的 (view, origin) 配對）。
- 不改 `internal/prunebackup` package 的對外 API：`PlanPair`、`ApplyAll`、`Plan`、`Deletion` 簽名不動；只是 caller 端不再累積。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `prune-backup-cli`: 把「Default to dry-run; require --apply to modify files」與「Atomic per-file write with .bak backup on --apply」的 ordering 詮釋從「全 plan 後才開始寫」放鬆為「per-pair plan→apply」。Exit-code、輸出格式、RRSet 規則、`.bak` 行為均不變。

## Impact

- Affected specs:
  - Modified: `openspec/specs/prune-backup-cli/spec.md`（注意此 spec 來自尚未 archive 的 `prune-redundant-backup-records` change；本 change 預期在那個 change archive 後才能 archive，順序由 operator 控制）
- Affected code:
  - Modified:
    - `cmd/shadowdns/prune_backup.go` — `runPruneBackup` 重構為 per-pair streaming；包 `bufio.Writer`；流程末尾 `os.Exit(0)`。
    - `cmd/shadowdns/prune_backup_test.go` — 既有測試斷言保持綠（dry-run 輸出順序、required flags、no-candidates 訊息）；新增 streaming-specific 行為測試（per-pair 順序、buffer flush、exit path 不漏輸出）。
    - `test/integration/prune_backup_test.go` — `TestPruneBackup_ApplyDeletesRedundantOverlayAndCreatesBak` 等保持綠。
  - New: (none)
  - Removed: (none)
