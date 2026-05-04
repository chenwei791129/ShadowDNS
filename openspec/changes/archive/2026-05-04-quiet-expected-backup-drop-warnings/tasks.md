## 1. Code change：classify.go logging 規則

- [x] 1.1 在 `internal/zone/classify.go` 的 `filterBackupRecords`（或對應 helper）內，把對 disallowed 記錄發出的 zap log 呼叫從單一 WARN 分支改成三路分流：(a) `rr.Header().Rrtype == dns.TypeSOA` → `logger.Debug(...)`；(b) `rr.Header().Rrtype == dns.TypeNS && dnsutil.LookupKey(rr.Header().Name) == dnsutil.LookupKey(zoneOrigin)` → `logger.Debug(...)`；(c) 其餘 → 維持原 `logger.Warn(...)` 訊息與欄位（zone / owner / type）。
- [x] 1.2 在 `filterBackupRecords` 處理單一 backup zone 的迴圈外側維護三個計數器（`soaDropped`, `apexNSDropped`, `otherDropped`）；分流寫入時順手 ++。
- [x] 1.3 在該 zone 處理結束處（迴圈出來、return 前），若至少一個計數器 > 0，呼叫 `logger.Info("backup-override zone: drop summary", ...fields)`，欄位含 `zone`（canonical origin）、`soa_dropped`、`apex_ns_dropped`、`other_dropped`。

## 2. Tests：unit test 覆蓋三條分流 + 摘要

- [x] 2.1 [P] 在 `internal/zone/classify_test.go` 新增 `TestFilterBackupRecords_SOADropIsDebug`：建構含一筆 SOA 的 backup zone RR slice，使用 `zaptest/observer` 補捉 log entries，assert 該 SOA 對應的 entry level == zapcore.DebugLevel、message 包含 "discarding disallowed record type"、欄位 type=="SOA"。
- [x] 2.2 [P] 在 `internal/zone/classify_test.go` 新增 `TestFilterBackupRecords_ApexNSDropIsDebug`：建構含一筆 owner==zone-origin 的 NS RR 的 backup zone，assert 對應 log entry level == DebugLevel、欄位 owner==canonical(origin)。
- [x] 2.3 [P] 在 `internal/zone/classify_test.go` 新增 `TestFilterBackupRecords_SubDelegationNSStaysWarn`：建構含一筆 owner == "child.<origin>" 的 NS RR 的 backup zone，assert 對應 log entry level == zapcore.WarnLevel（沒被誤降）。
- [x] 2.4 [P] 在 `internal/zone/classify_test.go` 新增 `TestFilterBackupRecords_NonOverridableStaysWarn`：建構含一筆 A 與一筆 CNAME 的 backup zone，assert 兩個對應 log entries 都是 WarnLevel；既有的「retain only TXT/MX/SRV」斷言不變。
- [x] 2.5 在 `internal/zone/classify_test.go` 新增 `TestFilterBackupRecords_EmitsPerZoneInfoSummary`：建構含 1 SOA + 4 apex NS + 17 A + 3 sub-delegation NS 的 backup zone，跑完後 assert 收到恰好一條 InfoLevel entry，欄位 `zone`==canonical origin、`soa_dropped`==1、`apex_ns_dropped`==4、`other_dropped`==20（17 A + 3 sub-delegation NS）。

## 3. Integration：ns2-style 規模驗證 hook（不啟動 server）

- [x] 3.1 跑 `make lint`、`make test` 全綠（`internal/zone` 內所有測試 + 既有 `test/integration/...` 不退化）。
- [x] 3.2 請使用者在 ns2 重啟 shadowdns（`sudo systemctl restart shadowdns`）後抓 30 秒 journal，確認：(a) `discarding disallowed record type` 的 WARN 計數應為 ~0（剩下的只可能是 prune --apply 後仍存在的非豁免類型殘留）；(b) 每個 backup zone 都有對應一條 `backup-override zone: drop summary` INFO 記錄；(c) DEBUG 預設不開時 SOA/apex-NS 那批 log 不應出現。
