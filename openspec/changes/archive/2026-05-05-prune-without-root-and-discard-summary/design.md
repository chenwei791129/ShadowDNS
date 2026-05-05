## Context

ShadowDNS 在 bench-ns2 啟動時把 `/var/log/shadowdns/shadowdns.log` 刷到 3.7M，
20,951 / 21,047 行（99.5%）都是 `backup-override zone: discarding disallowed record type`
WARN，集中在單一 zone `backup.example.com.`，type 分佈為 CNAME 20,727 + A 224。

事件鏈經 systematic-debugging 完整還原：

1. `backup.example.com.` 在 `shadowdns.yaml` aliases 宣告為 `root.example.com.` 的 backup。
2. `root.example.com.` 在 `/etc/namedb/master.zones` 完全沒宣告（grep -c = 0）。
3. `runPruneBackup`（cmd/shadowdns/prune_backup.go:142-150）對「root zone not declared」
   採 WARN + skip pair 策略。`backup.example.com.` 在 7 個 view 全部觸發此 skip，dry-run
   實測證實。
4. `backup.example.com.` 由 `$INCLUDE` 拉入 `/etc/namedb/master/allCNAME/backup.example.com_Rcname`
   含 2854 條 `xxx IN CNAME @`。此檔從未被 prune-backup 寫過。
5. runtime `zone.Classify`（internal/zone/classify.go:21-28）只看 yaml aliases 判定
   `backup.example.com.` 為 RoleBackupOverride，呼叫 `filterBackupRecords`。後者對每筆非
   SOA / 非 apex-NS / 非 TXT/MX/SRV 的 RR 印 1 行 WARN（line 66）。
6. 7 view × 2854 CNAME ≈ 19,978，與觀察到 20,727 條 CNAME WARN 完全相符（差額是其他殘留）。

關鍵不變條件：**runtime 與 prune-backup 對「是不是 backup」的判定權威必須統一**。
runtime 以 yaml aliases 為唯一權威（合理：runtime 不該因為 named.conf 沒宣告 root 就改
變 backup-override 行為）；prune-backup 目前的「root 缺席就 skip」是**實作偷懶**而非
規格要求。把 prune-backup 對齊到 yaml aliases 的權威是治本路徑。

## Goals / Non-Goals

**Goals:**

- **A（治本）**：`prune-backup` 在 root zone 未在同 view 宣告時，仍能清除 backup zone 中
  非 overridable type 的 RR；只跳過需要 root RRSet 比對的 overridable type 部分。
- **D（兜底）**：runtime `filterBackupRecords` 將 per-RR WARN 降為 DEBUG；per-zone 摘要
  從「總數」擴成「by-type 直方圖且總是輸出（only when 有 drop）」，每 zone 至多 1 行 INFO。
- 兩者合在一張 change 提交：A 修磁碟殘留來源，D 把「未來任何殘留也不會刷爆 log」做進兜底。
- 維持 v0.x.x 實驗階段允許的行為變更紀律——不標記 BREAKING、但 spec 與 changelog 說明清楚。

**Non-Goals:**

- 不修 BIND→ns2 zone 同步管線（不在 ShadowDNS 程式碼範疇）。
- 不擴增 `overridableTypes` 集合（仍維持 TXT/MX/SRV）；任何擴張需獨立 change。
- 不讓 runtime `zone.Classify` 受 named.conf 影響——權威仍是 yaml aliases。
- 不引入 logrotate / log truncation 邏輯——log 量級從源頭收斂。
- 不把 root-less 模式設成 opt-in flag——降級執行對所有 (view, backup) pair 一律生效，
  避免新增使用者必須學的旋鈕。

## Decisions

### 為什麼採取「root-less 降級執行」而不是「報錯要求補 root zone 宣告」

`shadowdns.yaml` aliases 是 ShadowDNS 全域配置，列出所有 alias 關係；named.conf 是這台
ns2 實際 serve 的 zones。同一份 yaml 部署在不同 ns2 上，root zone 是否在某台被 serve 是
拓樸決定，並非 yaml 漏寫。把 root 缺席當錯誤等於把拓樸資訊強加進 yaml，不合理。

降級執行只放棄需要 root RRSet 比對的部分（overridable type，TXT/MX/SRV——這些要跟 root
比 byte-equal 才能決定刪不刪），其餘判定（非 overridable 一律刪）不依賴 root，沒有理由
跟 root 比對綁在一起。

**Alternatives considered:**

- 「報錯不繼續」：拒絕，理由如上——拓樸資訊不該強塞進 yaml。
- 「runtime 在 root 缺席時跳過 backup-override 過濾」：拒絕，會讓 backup zone 退化成裸服務
  （直接吐 A/AAAA/CNAME 給客戶端），是行為大幅變更。

### 為什麼把 per-RR WARN 降為 DEBUG 而不是完全移除

DEBUG 級的單行 RR 細節在 ops 端啟用 `--log-level debug`（暫不存在的話走 zap atomic level）
時仍可拿到，對「我想知道哪筆 RR 被丟」這個 use case 是必要的觀察手段。WARN 級則保留給
「真的需要人類介入」的事件——backup zone 含大量 disallowed RR 是預期狀態（例如本案的
未 pruned 殘留），不該驚動人。

### 為什麼 per-zone INFO 摘要改成 by-type 直方圖

原本（classify.go:95-102）的 INFO 摘要只給 `soa_dropped`、`apex_ns_dropped`、`other_dropped`
三個總數，看到 `other_dropped: 20951` 並不能判斷是 CNAME 還是 PTR 還是其他——這在 ops
場景是失語的。by-type 直方圖（如 `dropped: {CNAME: 2854, A: 32}`）讓 ops 一眼看出殘留結構，
也利於 log 聚合工具做 ad-hoc query。

**Alternatives considered:**

- 保留 `other_dropped` 總數同時新增直方圖：拒絕，欄位重複，直方圖 sum 即總數。
- 把直方圖序列化成字串：拒絕，喪失 zap 結構化日誌的機讀價值。改用 `zap.Object` /
  `zap.Reflect` 或多個 `zap.Int(typeName, count)` 動態欄位（具體實作交給 tasks）。

### 為什麼 INFO 摘要的觸發條件「有任何 drop 就印一行」而不是維持「only when other_dropped > 0」

原條件是要讓「fully-pruned ns2 stays silent」——這個目標仍可保留（`len(dropped) == 0`
時不印）。但「只有 SOA / apex-NS 被丟也不印」這條規則隨直方圖細化後就沒必要了，每個 zone
給一行 INFO 摘要不會構成噪音（zone 數量 ≪ RR 數量），反而有助於確認啟動掃過哪些 zone。
同時把 `soa_dropped`、`apex_ns_dropped` 也納入直方圖（type 標示為 `SOA` / `apex_NS`），
資訊統一在一個欄位。

### 為什麼 prune-backup root-less 模式的 INFO 訊息與既有 WARN 訊息字串不同

既有訊息 `"skipping backup zone: root zone not declared in same view"` 表示「跳過」，
語義將從 v0.x.x 之後改成「降級執行」。沿用同一字串會讓 log grep 含義錯亂；改成
`"backup zone: root not declared, running in root-less mode"`（具體字串交給 tasks）並
降為 INFO 級——不是錯誤，是預期降級。

## Risks / Trade-offs

- **[Risk] root-less 模式刪除 backup zone 中所有非 overridable RR，可能誤刪「使用者刻意
  在 backup 留著、不希望進 root」的記錄。** → Mitigation：本來 runtime 就會把這些 RR 全部
  WARN 丟棄（本案 root cause 即為此），磁碟殘留是 noise 不是 feature；root-less 模式只是
  把「runtime 反正會丟」的決策提前到 prune 階段、讓磁碟與行為一致。dry-run（不加 --apply）
  在升級前 sanity check 仍可保留人類審核 gate。
- **[Risk] log schema 變更（per-RR WARN 消失、INFO 摘要欄位變動）會打破依賴舊字串的 ops
  dashboard / alert。** → Mitigation：v0.x.x 實驗階段且只部署於 bench-ns2，無外部
  consumers；CHANGELOG 與 release notes 明確標示 log schema 變動。
- **[Risk] 直方圖欄位若用 `zap.Reflect(map[string]int)`，欄位 key 順序在 console encoder
  下不可預期，不利於 grep。** → Mitigation：實作時對 type name 排序後用 `zap.Object` 方式
  序列化，產出 deterministic 結構（具體寫法交給 tasks）。
- **[Risk] root-less 模式對 `(view, backup)` pair 的 dry-run / apply 輸出格式無變動，但
  pair 不再 skip，每個 backup zone 多印 N 行刪除候選。** → Mitigation：本來就是 dry-run
  該做的事（讓人看到所有候選）；輸出量增加是預期。
