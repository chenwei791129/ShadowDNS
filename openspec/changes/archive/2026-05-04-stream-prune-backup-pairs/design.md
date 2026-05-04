## Context

`shadowdns prune-backup` 在 ns2 production 規模 dry-run 出現觀察到的 peak RSS 在 13–22GB 之間（不同 run 因進度不同停在不同點），加上印完 stdout 後 process 仍卡 4+ 分鐘的尾段。Symptom 來自當前 `cmd/shadowdns/prune_backup.go:runPruneBackup` 的形狀：

1. 對所有 (view, backup) pair 跑 `PlanPair`，把每一回合的 `plan.Deletions` `append` 進 `allDeletions`、把 `plan.Files` 合併進 `mergedFiles`。
2. 全部跑完才 `sort.Slice(allDeletions, ...)`、loop print、最後 `ApplyAll(mergedFiles, logger)`。

3000+ pair × 每 pair 平均 6000+ 候選 = ~19M `Deletion` 元素同時駐留（粗估 ~3.8GB 純 slice），`mergedFiles` 同時持有所有改寫過的 backup 檔內容（數 GB），加上每次 `PlanPair` 期間活著的 `rootMerged` / `rootIdx` / lexer 中間物 + Go GC fragmentation overhead，把 RSS 推到 13–22GB 是合理上界。實測觀察到 RSS 隨 pair 數線性成長（3G → 22G），符合「跨 pair 累積」模型。Process 印完 stdout 後仍卡 4+ 分鐘是 GC mark phase 在掃這些大型結構（雖然程式即將退出，runtime 在 `defer logger.Sync()` 之類的觸發點仍會跑一輪 GC）。

問題的本質：工具的記憶體 footprint 等於整個資料庫，而不是當下處理單位。

## Goals / Non-Goals

**Goals**
- 把 peak RSS 從「全資料庫 (~13GB)」降到「單一 pair (~MB 級)」。
- 印完 stdout 與 apply 完最後一個 pair 後立即退出，不再有 GC 尾段卡頓。
- 維持 dry-run 輸出順序與當前實作一致（`(view, backup origin)` 排序，pair 內按 `file:line` 排序）。

**Non-Goals**
- 不調整 RRSet 比對規則或 `.bak` / atomic-write 機制。
- 不引入跨 pair 的全域 `file:line` 排序（不同 pair 不共用 backup 檔，全域排序沒有觀察價值）。
- 不改 `internal/prunebackup` package 對外 API（`PlanPair`、`ApplyAll` 簽名、`Plan` 與 `Deletion` 結構保持不變）。
- 不引入並行 pair 處理。Streaming 是必要條件而非並行；並行寫入會破壞單純的 fail-stop 順序語意，不在本 change 範圍。

## Decisions

### Decision: Per-pair streaming pipeline 取代全域累積

`runPruneBackup` 改成 per-pair iterator：在排序好的 `pairs` slice 上依序對每個 pair 走 plan→sort→print→(apply)→drop，而非把所有 pair 的結果累積到末尾。中間結構 (`plan.Deletions`、`plan.Files`) 在 pair 結束後失去引用、立刻可被 GC 回收，下個 pair 從 baseline 開始分配。

**Alternative**：保留累積模型，僅在末尾把 `allDeletions` / `mergedFiles` 顯式設 nil 觸發 GC。被 reject — 中間 peak 仍是全資料庫等級，沒解決 OOM 風險。

**Alternative**：goroutine pipeline（producer 跑 PlanPair、consumer 印與寫）。被 reject — 增加並行複雜度，對單純 streaming 沒額外收益；fail-stop 與排序約束會讓並行最終退化成序列等待。

### Decision: dry-run 排序語意改為 per-pair sort，不再做全域 sort

當前實作用全域 `sort.Slice(allDeletions, byFileLine)`，但每個 backup zone 屬於唯一一個 (view, origin) pair（spec 的 `Iterate backup zones per view` 保證），所以**任何 (file, line) 對都只會出現在恰好一個 pair 裡**。把 sort 從全域降為 pair 內，輸出順序對 operator 不變（仍是 deterministic file/line 順序），但無需把 19M Deletion 同時持在記憶體裡。

Pair 之間的順序由外層的 `(view, backup origin)` 排序決定，與當前實作一致。

**Alternative**：用 priority queue 跨 pair 做 streaming merge sort。被 reject — 沒有觀察價值（pair 不共用檔），徒增複雜度。

### Decision: Apply 改成 per-pair 而非 batch

當前 `--apply` 路徑是「全部 pair plan 完才 `ApplyAll(mergedFiles, ...)`」，間接提供「先把所有 zone parse 過再動筆」的預檢效果。改為 per-pair 後，第 N 個 pair 的 zone parse 失敗時，前 N-1 個 pair 可能已經 `--apply` 完成（spec 既有的 fail-stop 語意：失敗即停、`.bak` 留下、operator 自行決定如何處理）。

實質安全網等價：
- Operator 標準流程是先跑一次 dry-run 看候選，再跑 `--apply`。Dry-run 不寫檔但走完整 parse → 這就是「先驗所有 zone 都能 parse」的等價手段。
- 即使 operator 跳過 dry-run 直接 apply，spec Requirement「Atomic per-file write with .bak backup on --apply」第 3 條已經涵蓋這個情境，沒有新類別的失敗模式。

**Alternative**：保留 batch apply 但把 `mergedFiles` 改成 spill-to-disk。被 reject — 為了一個 operator 能用 dry-run 替代的預檢搞 spill 機制不划算。

### Decision: bufio.Writer 包裹 stdout，64KB buffer

dry-run 主要 cost 是 `fmt.Fprintf(out, ...)` × 19M = 19M syscalls。包成 `bufio.NewWriterSize(out, 64*1024)`、流程末尾 `Flush()`，把 syscalls 壓到千級。對 buffer 上界選 64KB 是憑常識：dry-run 一行約 80–200 bytes，64KB 約對應 300–800 行的攤銷，明顯比預設 4KB 好但又不誇張。

**注意**：cobra `cmd.OutOrStdout()` 預設指向 `os.Stdout`；單元測試裡會被替換成 `*bytes.Buffer`。bufio 對 `bytes.Buffer` 同樣有效（只是不必要），測試行為不變。

### Decision: 流程末尾以 `os.Exit(0)` 取代 cobra return

`runPruneBackup` 正常路徑結束時，`logger.Sync()` 之類的 defer 會觸發一輪 GC scan。即使所有 pair 結構都早已 unreferenced，殘留的 stack frame、cobra 內部狀態等仍要被掃。改成在 `runPruneBackup` 完成寫入後手動 `_ = logger.Sync()`、然後 `os.Exit(0)`，讓 process 立刻退出由 OS 回收記憶體。

風險與 trade-off：
- 跳過 cobra 的後續清理（沒有 — 沒登記 deferred shutdown）。
- 跳過 deferred file close（`config.LoadNamedConf` 與 `shadowdnscfg.Load` 已自行 close 相關 file handle；prune-backup 流程不留長 handle）。
- 跳過 `defer logger.Sync()` 的延後執行 — 改成顯式同步呼叫一次。
- Test 端：`runPruneBackup` 的 unit test 直接呼叫此函式並期待 return；`os.Exit` 寫在此函式末尾會中斷測試流程。**因此 `os.Exit(0)` 必須寫在更外層**（cobra `RunE` 結束處或 `main` 結束處），讓 `runPruneBackup` 仍 return error/nil 給呼叫者。

**Alternative**：runtime tuning（`debug.SetGCPercent(-1)` 關 GC、`runtime.GC()` 強制壓最低）。被 reject — 與其調 GC 不如不要產生那麼多物件；此 decision 是 belt-and-suspenders，跟 streaming 一起做才有意義。

## Risks / Trade-offs

- [Per-pair apply 失去「先驗所有 zone parse」的預檢] → 文件化 operator workflow：先跑 dry-run（純讀、走完整 parse、不寫檔）→ 再跑 `--apply`。Spec 的 Exit-code 描述不變（parse 失敗仍非零）。
- [`os.Exit(0)` 跳過 deferred cleanup] → cmd/shadowdns/prune_backup.go 確認沒登記任何「必須等 cleanup 完成才能退出」的 defer；`logger.Sync()` 改成顯式呼叫；其他 defer（如 cobra 內部）對 prune-backup 路徑不影響資料完整性。
- [`bufio.Writer` 沒 Flush 會吞輸出] → 在所有正常返回路徑顯式 `Flush()`；在 error 返回路徑也 Flush（已印的 dry-run candidates 對 operator 仍有價值）；以 unit test 覆蓋「最後一行不會丟」。
- [Output 順序與當前實作不一致] → spec 從未保證「跨 pair 全域 file:line 排序」（只保證 deterministic），per-pair sort 仍 deterministic。`TestPruneBackup_*` 既有測試的斷言會檢查；新增 streaming-specific test 釘住 `(view, backup) → file:line` 兩層順序。

## Migration Plan

- 純內部重構，無 config schema 變動、無 CLI 介面變動、無 packaging 變動。
- 部署上等同重新發 binary；operator workflow 不變（先 dry-run 再 `--apply` 仍是建議路徑）。
- 回滾：reverse 該 commit / 退回前一版 binary 即可，行為等價。

## Open Questions

- 64KB buffer 是否需要設成可調 flag？本版固定 64KB，足以把 syscall 數壓到千級；若未來 operator 報告 buffer 大小重要再加 `--out-buffer-size`。
- Root zone 重複 I/O：當前 `PlanPair` 對每一個 (view, backup) 都重新讀一次對應 root 的 zone tree。ns2 上多個 backup 共用同一個 root 是常態（典型 2–5 個 backup 對 1 個 root），streaming 之後 peak RSS 雖降，但 root 重複讀的 I/O 與 GC churn 沒解。本版**不**處理這條，等 streaming 落地、量出新的 baseline 後再評估是否需要 root parsed-state 的 LRU cache（另起 change）。
