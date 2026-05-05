## 1. 撰寫紅燈測試（TDD 先行）

- [x] 1.1 [P] 在 internal/prunebackup/diff_test.go 新增測試：Iterate backup zones per view 在 root 缺席時的 root-less 降級行為——驗證為什麼採取「root-less 降級執行」而不是「報錯要求補 root zone 宣告」這個決策的可觀察 contract（CNAME/A/AAAA/PTR 仍被 plan 為刪除、TXT/MX/SRV 在 root-less 下被保留）
- [x] 1.2 [P] 在 internal/prunebackup/prunebackup_test.go 新增測試：Iterate backup zones per view 在「root-less pair 與正常 pair 並存」時兩者各自正確產出 dry-run 結果，且不互相影響——對應 design「為什麼 prune-backup root-less 模式的 INFO 訊息與既有 WARN 訊息字串不同」要求的可觀察輸出契約
- [x] 1.3 [P] 在 internal/zone/classify_test.go 新增測試：Classify zones as root or backup override at load time 修改後的 per-RR DEBUG 行為——對應 design「為什麼把 per-RR WARN 降為 DEBUG 而不是完全移除」（zaptest observer 驗證 WARN 級為 0、DEBUG 級含每筆 RR）
- [x] 1.4 [P] 在 internal/zone/classify_test.go 新增測試：Classify zones as root or backup override at load time 的 by-type 直方圖摘要——對應 design「為什麼 per-zone INFO 摘要改成 by-type 直方圖」與「為什麼 INFO 摘要的觸發條件「有任何 drop 就印一行」而不是維持「only when other_dropped > 0」」（含 SOA / apex_NS / NS / A / CNAME 標籤、deterministic alphabetic key order、零 drop 不印 / 只 RFC-mandated drop 仍印）

## 2. 實作 A：prune-backup root-less 降級

- [x] 2.1 修改 internal/prunebackup/diff.go：將 classify 的「非 overridable type → decisionDelete」分支抽到不依賴 rootRRSet 的純函式，使呼叫端可在 root 缺席時仍能驅動該分支
- [x] 2.2 修改 internal/prunebackup/prunebackup.go PlanPair：新增 root-less 路徑——當 rootFile 為空時跳過 rootMerged 載入與 rootIdx 建立，且 PlanPair 內部對每個 (owner, rtype) group 只在 overridable type 路徑進入 decisionRetain，非 overridable type 仍進入 decisionDelete；維持既有 deletion 標註與 pruneFile rewrite 邏輯
- [x] 2.3 修改 cmd/shadowdns/prune_backup.go pair 收集：移除「root 未宣告就 skip」分支，改為帶空 rootFile 進入 PlanPair；新增 root-less INFO log 訊息字串以覆寫既有 WARN——對應 design「為什麼 prune-backup root-less 模式的 INFO 訊息與既有 WARN 訊息字串不同」
- [x] 2.4 跑 1.1 / 1.2 紅燈測試使其轉綠

## 3. 實作 D：runtime classify log 摘要化

- [x] 3.1 修改 internal/zone/classify.go filterBackupRecords：per-RR Warn 改 Debug 對齊 spec 修訂版 Classify zones as root or backup override at load time 中「Per-record entry MUST NOT appear at INFO or higher levels」——對應 design「為什麼把 per-RR WARN 降為 DEBUG 而不是完全移除」
- [x] 3.2 修改 internal/zone/classify.go：將既有 soa_dropped / apex_ns_dropped / other_dropped 三個總數欄位改為單一 by-type histogram 結構欄位，type 標籤含 SOA / apex_NS / 一般 RR type；以 deterministic alphabetic key order 序列化（zap.Object marshal 自定義以避免 map 隨機順序）——對應 design「為什麼 per-zone INFO 摘要改成 by-type 直方圖」
- [x] 3.3 修改 INFO 摘要觸發條件：當 zone 有任何 drop 時都印一行（不再要求 other_dropped > 0），但零 drop 仍不印——對應 design「為什麼 INFO 摘要的觸發條件「有任何 drop 就印一行」而不是維持「only when other_dropped > 0」」
- [x] 3.4 跑 1.3 / 1.4 紅燈測試使其轉綠

## 4. 整合驗證與部署 sanity check

- [x] 4.1 跑 make test 確認所有單元測試通過（含 race detector）
- [x] 4.2 跑 make lint 確認 golangci-lint 無新警告
- [x] 4.3 請使用者跑 release-shadowdns skill 本地 build deb，部署到 bench-ns2，重啟 shadowdns，確認 /var/log/shadowdns/shadowdns.log 不再爆量（單次重啟 < 100 行 INFO 摘要層級，且不再出現 per-RR `discarding disallowed record type` WARN）；之後請使用者跑一次 `shadowdns prune-backup --apply --named-conf /etc/namedb/named.conf --config /etc/shadowdns/shadowdns.yaml` 將 backup.example.com_extra 的 2854 條 CNAME 清掉，再重啟驗證 INFO 摘要顯示 dropped histogram 為空
