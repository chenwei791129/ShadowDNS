## 1. 回歸測試（TDD，先寫）

- [x] 1.1 新增 `internal/prunebackup/apply_writeorder_test.go`：(a) 成功路徑——`applyFile` 後斷言 `path` 為新內容且權限位元同原檔、`path.bak` 為舊內容；(b) 順序不變式——以可注入失敗點的方式（或重構後可測的內部步驟）驗證「backup 建立後、最終 rename 之前」若中斷，`path` 仍指向一個完整有效檔案（原內容或新內容），不會缺檔。此覆蓋需求 "prune-backup rewrite keeps a valid file at the path at all times"。驗證：`go test ./internal/prunebackup/ -run WriteOrder` 於修補前（destroy-then-write）對不變式斷言失敗。

## 2. 修補 applyFile 寫入順序

- [x] 2.1 在 `internal/prunebackup/apply.go` 重排 `applyFile`：先建立 sibling temp（write → Sync → Chmod 為原檔 mode），再以 hardlink（`os.Link`，先移除既有 `.bak`；跨檔案系統失敗時退回 byte copy）從仍在 `path` 的原檔產生 `bakPath`，最後 `os.Rename(tmpName, path)` 原子替換。原檔在最終 rename 前始終保留在 `path`。觀察結果：任一時點 `path` 皆存在有效檔案。此覆蓋需求 "prune-backup rewrite keeps a valid file at the path at all times"。驗證：步驟 1.1 測試轉綠。
- [x] 2.2 於最終 rename 後 fsync 包含目錄（開啟 `filepath.Dir(path)`、呼叫 `Sync`、關閉），使 rename metadata 持久化。驗證：擴充 1.1 測試或以行為說明覆蓋；`make test` 綠。

## 3. 驗證與回歸

- [x] 3.1 確認既有 `ApplyAll` 與 prune-backup 成功路徑行為不變（stop-on-first-failure、選檔、prune 轉換）。驗證：`go test -race ./internal/prunebackup/ -run 'Apply|Prune'` 全數通過。
- [x] 3.2 整體品質閘通過。驗證：`make lint` 與 `make test` 皆 exit 0。
