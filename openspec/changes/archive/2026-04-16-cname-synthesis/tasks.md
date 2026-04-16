## 1. Zone 層 CNAME 查詢支援

- [x] [P] 1.1 在 `internal/zone/zone.go` 新增 `LookupCNAME(owner string) []dns.RR` 方法，回傳指定 owner 下的 CNAME 紀錄（`dns.TypeCNAME`）。若無 CNAME 則回傳空 slice。此方法供 handler 在 `Lookup` 回空時做 CNAME 合成 fallback
- [x] [P] 1.2 在 `internal/zone/zone_test.go` 為 `LookupCNAME` 新增單元測試：(a) name 有 CNAME → 回傳 CNAME，(b) name 無 CNAME 但有 A → 回傳空，(c) name 不存在 → 回傳空

## 2. Root zone CNAME 合成（Synthesize CNAME response when qtype does not match but CNAME exists at the queried name）

- [x] 2.1 實作「Synthesize CNAME response when qtype does not match but CNAME exists at the queried name」：修改 `internal/server/handler.go` 的 `handleRootQuery`，當 `rootZone.Lookup(qname, qtype)` 回空且 `qtype != dns.TypeCNAME` 時，呼叫 `rootZone.LookupCNAME(qname)`，若有 CNAME 則以 `replyWithAnswer` 回傳該 CNAME 紀錄
- [x] [P] 2.2 在 `internal/server/server_test.go` 新增 root zone CNAME 合成測試：(a) 查 A 但 name 只有 CNAME → 回傳 CNAME 且 RCODE=NOERROR，(b) 查 AAAA 但 name 只有 CNAME → 回傳 CNAME，(c) 查 CNAME 直接 → 回傳 CNAME（既有行為不變），(d) name 無任何紀錄 → NXDOMAIN（既有行為不變），(e) name 有 A 但查 AAAA → NODATA（既有行為不變）

## 3. Backup zone CNAME 合成

- [x] 3.1 修改 `internal/alias/override.go` 的 `Resolve` 函式：當 root zone 的 `Lookup(rootQName, qtype)` 回空且 `qtype != dns.TypeCNAME` 時，嘗試 `rootZone.LookupCNAME(rootQName)`，若有 CNAME 則對該 CNAME 紀錄套用 `RewriteRR` 回傳（owner name 改為 backup namespace）
- [x] [P] 3.2 在 `internal/alias/override_test.go` 新增 backup zone CNAME 合成測試：(a) root zone 有 CNAME，查 backup zone 的 A → 回傳 rewritten CNAME（owner 為 backup namespace），(b) root zone 無 CNAME 也無 A → 回傳空

## 4. 整合測試

- [x] 4.1 在 `test/integration/` 新增 CNAME 合成整合測試：在 testdata zone file 中加入 CNAME 紀錄，驗證 (a) dig A 查詢 CNAME name 回傳 CNAME，(b) dig CNAME 直接查詢正常運作，(c) backup zone 的 CNAME 合成正確 rewrite owner name
- [x] 4.2 執行 `make test` 與 `make lint` 確認所有測試通過且無 lint 錯誤
