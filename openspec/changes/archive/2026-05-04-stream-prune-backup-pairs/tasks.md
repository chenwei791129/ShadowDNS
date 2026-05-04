## 1. Refactor runPruneBackup to per-pair streaming pipeline（支撐 spec Requirement「Process zone pairs as a streaming pipeline」、「Apply writes flush per pair instead of batched at the end」與 design decision「Per-pair streaming pipeline 取代全域累積」、「Apply 改成 per-pair 而非 batch」）

- [x] 1.1 在 `cmd/shadowdns/prune_backup.go` 的 `runPruneBackup` 內，把現有「先全 loop 累積 `allDeletions` + `mergedFiles`，後排序 / 印 / `ApplyAll`」改成單一外層 `for _, p := range pairs` 迴圈，迴圈內依序：呼叫 `prunebackup.PlanPair`、對 `plan.Deletions` 做 pair-local `sort.Slice` (by `File`, then `StartLine`)、寫出該 pair 的 dry-run lines、若 `applyWrites` 為真則對該 pair 的 `plan.Files` 呼叫 `prunebackup.ApplyAll`、迴圈結尾把 `plan` 變數設 nil 確保引用被釋放。
- [x] 1.2 移除 `runPruneBackup` 中的 `var allDeletions []prunebackup.Deletion` 與 `mergedFiles := map[string][]byte{}` 兩個累積容器；連同末尾原本的全域 `sort.Slice(allDeletions, ...)` 和 `prunebackup.ApplyAll(mergedFiles, logger)` 一併刪除。
- [x] 1.3 在迴圈外新增「至少有一個 pair 跑出 deletion」與「跑完所有 pair 都沒 deletion」的二元狀態追蹤（簡單 bool），跑完外層迴圈後若沒有任何 pair 產生 deletion，印 `no redundant records found`（保持當前行為）。

## 2. Wrap output destination with buffered writer（支撐 spec Requirement「Output writer flushes before exit」、design decision「bufio.Writer 包裹 stdout，64KB buffer」）

- [x] 2.1 [P] 在 `runPruneBackup` 入口建立 `bw := bufio.NewWriterSize(out, 64*1024)`，並讓 step 1.1 的 dry-run print 改寫到 `bw`。
- [x] 2.2 [P] 在 `runPruneBackup` 所有 return 路徑（成功 / 任一錯誤）前顯式呼叫 `bw.Flush()`；以 `defer` 寫一道兜底並覆蓋 success 路徑的明確 Flush，確保 error 路徑也不丟行。

## 3. Replace process-end teardown with explicit logger.Sync + os.Exit(0)（支撐 design decision「流程末尾以 `os.Exit(0)` 取代 cobra return」）

- [x] 3.1 把 `newPruneBackupCmd` 的 `RunE` 改成：呼叫 `runPruneBackup`，若回傳非 nil error 直接 return（沿用 cobra exit-code 對應）；若 nil，顯式呼叫 `logger.Sync()`、然後 `os.Exit(0)`。
- [x] 3.2 確認 `runPruneBackup` 本身仍 return error/nil 給呼叫者（**不在此函式內呼叫 `os.Exit`**），讓既有 unit test 可以照常測 return value 與 stdout buffer。
- [x] 3.3 在 `prune_backup.go` 的 `runPruneBackup` 與 `RunE` 兩處加註解，明確標示「`os.Exit` 限定在 cobra `RunE` 結束處，函式本體保留 testable return」。

## 4. Test coverage for streaming behaviour（支撐 spec Requirement「Process zone pairs as a streaming pipeline」、「Deterministic dry-run output order across pairs and within each pair」、「Output writer flushes before exit」）

- [x] 4.1 在 `cmd/shadowdns/prune_backup_test.go` 新增 `TestRunPruneBackup_TwoPairsStreamInOrder`：兩個 view × 兩個 backup zone（手刻 named.conf + yaml + 4 個 zone 檔到 `t.TempDir()`），各自貢獻不同 deletion；assert 輸出每行的 `(view, backup origin)` block 與 pair-local `file:line` 順序符合 spec example table 的相對順序（不假設跨 pair 全域排序）。
- [x] 4.2 [P] 在 `cmd/shadowdns/prune_backup_test.go` 新增 `TestRunPruneBackup_NoTrailingLineLost`：單一 pair 多筆 deletion 跑 dry-run，把 stdout 接到 `bytes.Buffer`，assert 行數與預期一致，最後一行非空且以 `\n` 結尾，證明 `bufio.Writer` 有 Flush。
- [x] 4.3 [P] 在 `cmd/shadowdns/prune_backup_test.go` 新增 `TestRunPruneBackup_ApplyWritesPerPair`：兩個 pair 各有 deletion，手動把第二個 pair 的 backup 檔權限改成不可寫（或用不存在的目錄當 file 路徑）模擬第二 pair apply 失敗；assert 第一 pair 的 `.bak` + pruned 檔已寫成功留在磁碟，第二 pair 維持原狀，函式回傳非 nil error。

## 5. Doc + integration（支撐 design Migration Plan）

- [x] 5.1 在 `cmd/shadowdns/prune_backup.go` 的 sub-command Long help 文字裡加一句：建議 operator workflow 為「先 dry-run 看候選，再 `--apply`」（dry-run 走完整 parse 等同預檢，per-pair apply 不再做 plan-all 預檢）。
- [x] 5.2 [P] 跑 `make lint`、`make test`、`make smoke`、`go test ./test/integration/... -run PruneBackup -count=1` 全綠；尤其確認 `TestPruneBackup_NoCandidatesOnCleanFixture` 與 `TestPruneBackup_ApplyDeletesRedundantOverlayAndCreatesBak` 仍通過（streaming 不影響這兩個小規模情境）。
- [x] 5.3 請使用者在 ns2 重跑 `shadowdns prune-backup --named-conf /etc/namedb/named.conf --config /etc/shadowdns/shadowdns.yaml`，確認：(a) peak RSS 大幅低於先前 13GB 量級；(b) 印完最後一行候選後立即退出（無 4 分鐘卡頓）；(c) 候選總數仍與 streaming 前一致（19M± 級）。
