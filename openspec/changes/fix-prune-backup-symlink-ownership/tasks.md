## 1. 回歸測試（TDD，先寫）

- [x] 1.1 新增 `internal/prunebackup/apply_symlink_test.go`：(a) 對一個 symlink 路徑呼叫 `applyFile`，斷言回傳含 "symlink" 的錯誤且該 symlink 未被換成一般檔案（仍是 symlink、指向不變）；(b) Unix-guarded 測試：對一般檔案成功 apply 後，斷言結果檔的 uid/gid 與原檔相同（在無法變更 uid 的非特權環境下，改以「chown 被呼叫且不改變合法情況下的 owner」或直接 `t.Skip` 處理）。此覆蓋需求 "prune-backup refuses symlinked paths and preserves file ownership"。驗證：`go test ./internal/prunebackup/ -run Symlink` 於修補前失敗（symlink 被替換）。

## 2. 修補 applyFile（symlink 拒絕 + ownership 保留）

- [x] 2.1 在 `internal/prunebackup/apply.go` 的 `applyFile` 入口以 `os.Lstat` 檢查 path：若為 symlink（`fi.Mode()&os.ModeSymlink != 0`）則回傳描述性錯誤（含 path 與 "symlink"）並不進行改寫。觀察結果：symlink 路徑被拒、拓樸保留。此覆蓋需求 "prune-backup refuses symlinked paths and preserves file ownership"。驗證：步驟 1.1(a) 轉綠。
- [x] 2.2 保留 ownership：於最終 rename 前，從原檔 `origInfo.Sys().(*syscall.Stat_t)` 取 uid/gid，對 temp 檔 `os.Chown` 至該 uid/gid（best-effort：chown 失敗時記錄但不致命）。以 build-tag 或 runtime 型別斷言方式隔離 `syscall.Stat_t`，確保非 Unix 建置仍可編譯。觀察結果：改寫後檔案 uid/gid 同原檔。驗證：步驟 1.1(b) 轉綠。

## 3. 驗證與回歸

- [x] 3.1 確認 #16 帶入的 crash-safe 寫入順序與既有 prune-backup 成功路徑行為不變。驗證：`go test -race ./internal/prunebackup/ -run 'Apply|Prune|WriteOrder|Symlink'` 全數通過。
- [x] 3.2 整體品質閘通過。驗證：`make lint` 與 `make test` 皆 exit 0。
