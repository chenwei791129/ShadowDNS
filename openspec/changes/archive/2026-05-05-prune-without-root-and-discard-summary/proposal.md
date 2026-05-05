## Why

部署在 bench-ns2 上 `backup.example.com` backup zone 重啟後刷出 3.7M log，99.5% 都是
`backup-override zone: discarding disallowed record type` WARN（20,727 條 CNAME +
224 條 A，集中在單一 zone）。根因有兩層：

1. **`prune-backup` 對 root zone 缺席的 backup zone 完全 skip。** `backup.example.com` 在
   `shadowdns.yaml` aliases 裡宣告為 `root.example.com` 的 backup，但 named.conf
   完全沒宣告 `root.example.com`。`runPruneBackup` 在 `cmd/shadowdns/prune_backup.go:142-150`
   遇到「root zone not declared」就跳過整個 pair，導致 `backup.example.com_Rcname` INCLUDE 子檔的
   2854 條 CNAME 永遠沒被清。然而 runtime `zone.Classify` 看 yaml aliases 就把它當
   backup-override 處理，每次重啟刷 7 view × 2854 ≈ 19,978 行 WARN。`prune-backup`
   與 runtime 對「是不是 backup」的判定條件不對等，造成「跑了 prune 也清不掉」的盲區。
2. **runtime `filterBackupRecords` 對每筆被丟的 RR 印 1 行 WARN，未做摘要。** 即使 (1)
   修好，未來只要再有任何一條 disallowed RR 殘留，依然會以 per-RR 粒度刷 log。
   `internal/zone/classify.go:35-41` 設計註解預設「fully-pruned ns2 stays silent」，
   但這個前提脆弱——只要 BIND→ns2 同步管線出任何漏網之魚，log 就會爆。

## What Changes

- **修 `prune-backup` root 缺席處理（治本，A）**：當 backup zone 的 root alias 在 named.conf
  未宣告時，不再整個 skip pair；改為以「root-less 模式」執行——對非 overridable type
  （非 TXT/MX/SRV，如 CNAME/A/AAAA/PTR/非 apex NS）一律 plan 為刪除（這部分不需要 root
  RRSet 比對），對 overridable type（TXT/MX/SRV）才需要 root 比對的部分跳過保留。
  pair-level WARN 改為 INFO 表達「進入 root-less 模式、僅清除非 overridable」。
- **修 runtime discard log 噪音（兜底，D）**：`internal/zone/classify.go` `filterBackupRecords`
  對每筆被丟的 RR 從 WARN 降為 DEBUG（保留個別 RR 細節給 debug 用），同時將既有的
  per-zone INFO 摘要從「only when other_dropped > 0、僅含總數」擴充為「by-type 直方圖
  且總是輸出（可摘要時）」，例：`{"zone":"backup.example.com.","dropped":{"CNAME":2854,"A":32}}`。
- **回歸測試覆蓋**：A 的「root-less 降級」路徑、D 的 by-type 摘要與單行 INFO 行為。
- v0.x.x 實驗階段：log message/level 變更不視為 BREAKING。

## Non-Goals

- 不在這次 change 改 BIND→ns2 zone 同步管線（不是 ShadowDNS 程式碼範疇）。
- 不引入 logrotate / log file truncate 邏輯——log 量級問題從源頭收斂即可。
- 不改 runtime `zone.Classify` 對「是不是 backup」的判定——維持以 yaml aliases 為準。
  若 root 不在 named.conf，runtime 仍把 zone 當 backup-override 處理；新行為由 prune-backup
  端的 root-less 模式吸收。
- 不擴增 `overridableTypes` 集合（仍維持 TXT/MX/SRV）；任何擴張需獨立 change 走 review。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `prune-backup-cli`: 新增「root-less 降級模式」需求——root zone 未在同 view 宣告時，
  pair 不再整個 skip，仍會清除 backup zone 中非 overridable type 的 RR。
- `zone-parser`: 修改 backup-override classifier 的 log 輸出形狀——per-RR WARN 降為
  DEBUG，per-zone INFO 摘要改為 by-type 直方圖且在有任何 drop 時都輸出。

## Impact

- Affected specs: `prune-backup-cli`、`zone-parser`
- Affected code:
  - Modified:
    - internal/prunebackup/diff.go
    - internal/prunebackup/prunebackup.go
    - cmd/shadowdns/prune_backup.go
    - internal/zone/classify.go
    - internal/zone/classify_test.go
    - internal/prunebackup/diff_test.go
    - internal/prunebackup/prunebackup_test.go
  - New: (none)
  - Removed: (none)
- Operational：使用者重啟 shadowdns 觀察 log，預期 `backup.example.com` 在跑過新版 prune-backup
  --apply 後不再出現 `discarding disallowed record type`，最壞情況下也只剩單行
  zone-level INFO 摘要。
