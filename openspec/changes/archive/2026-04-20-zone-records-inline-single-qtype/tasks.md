## 1. TDD 紅燈:Lookup 契約的「shared backing」如何在 inline / promoted 下都成立(inline↔promoted 狀態轉換與 shared-backing 契約)

- [x] 1.1 於 `internal/zone/zone_test.go` 新增三個 test(一次紅燈):
  `TestQtypeStore_InlineToPromoted`(owner 先插 A 一筆、再插 AAAA 一筆,Lookup 兩個 qtype 皆回正確數量;內部 state 可用 package-internal field `z.Records["owner"].single` 驗證從 true→false 的轉換)、
  `TestZone_LookupReturnsSharedBacking_Promoted`(對稱於現有 `TestZone_LookupReturnsSharedBacking`,在 2-qtype owner 上驗證 `&stored[:cap][0] == &lookupResult[:cap][0]`)、
  `TestZone_LookupSharedBackingAcrossPromotion`(先 inline 拿一個 Lookup 結果,觸發 promotion 後內部 `z.Records["owner"].sub[dns.TypeA]` 與先前 Lookup 結果的 backing array 地址相同)。三 test 在 Records 型別改為 `map[string]*qtypeStore` 前全部編譯失敗 —— 即為紅燈。

## 2. `qtypeStore` 結構與 Records 型別切換(狀態轉換只有「inline → promoted」、不做反向;`qtypeStore.each` helper 收斂所有 "iterate owner's all RRs" 的 caller)

- [x] 2.1 於 `internal/zone/zone.go` 新增 `qtypeStore` package-internal struct(`single bool; qtype uint16; rrs []dns.RR; sub map[uint16][]dns.RR`),並加 doc comment 說明 inline(`single=true`:rrs 為該 qtype 的 RR list)與 promoted(`single=false`:sub 持有所有 qtype→RRs)兩狀態的語意與轉換規則
- [x] 2.2 將 `Zone.Records` 宣告由 `map[string]map[uint16][]dns.RR` 改為 `map[string]*qtypeStore`,更新 `Zone` struct 上方的 doc comment(改描述 inline/promoted dual-state)
- [x] 2.3 新增 `(s *qtypeStore) each(fn func(uint16, []dns.RR))` helper(nil-safe;inline 呼叫一次、promoted 走 sub-map range),供 classify/transfer iterate owner-all-rrs 使用

## 3. AddRR 狀態機

- [x] 3.1 改寫 `AddRR`:owner 不存在 → 建 `&qtypeStore{single: true, qtype, rrs: []dns.RR{rr}}`;owner 存在且 inline 且 qtype 相同 → `s.rrs = append(s.rrs, rr)`;owner 存在且 inline 且 qtype 不同 → promote(建 `sub = map[uint16][]dns.RR{s.qtype: s.rrs, rr.Rrtype: {rr}}`,清 `s.single/qtype/rrs`);owner 存在且已 promoted → `s.sub[rr.Rrtype] = append(s.sub[rr.Rrtype], rr)`。保持 SOA cache(`z.SOA = soa`)邏輯不變
- [x] 3.2 promote 路徑必須用 `s.sub[s.qtype] = s.rrs`(同一 slice header,保持 backing array 連續),以滿足 spec 的 "Shared backing array survives inline→promoted transition" scenario

## 4. Read path 改造

- [x] 4.1 改寫 `Lookup(owner, qtype)`:`z.Records[owner]` → nil 回 nil;`s.single && s.qtype == qtype` → 回 `s.rrs`;`s.single && s.qtype != qtype` → 回 nil;`!s.single` → 回 `s.sub[qtype]`
- [x] 4.2 [P] 改寫 `wildcardHit(s *qtypeStore, qtype uint16)`:qtype==0 sentinel 改為「`s != nil && (s.single || len(s.sub) > 0)`」;qtype != 0 時 delegate 到等同 Lookup 的 inline/promoted 分支;`LookupWildcard` 內 `z.Records[wildcard]` 的 ok-check 型別更新為 `*qtypeStore`

## 5. 既有 iterate-all-RRs caller 改走 each helper

- [x] 5.1 `internal/zone/classify.go` 的 `filterBackupRecords`:owner 迴圈內改用 `s.each(...)`;刪除 disallowed qtype 時,inline 狀態直接 `delete(z.Records, owner)`,promoted 狀態用 `delete(s.sub, rrtype)` + `if len(s.sub) == 0 { delete(z.Records, owner) }`
- [x] 5.2 [P] `internal/transfer/axfr.go` 的 `buildAliasRecords` 與 `collectNonSOA`:`rootZone.Records[owner]` 取得 `*qtypeStore` 後用 `s.each(...)` 收集 per-qtype RRs;排序 types 的邏輯保留,只是 key 從 `typeMap map[uint16][]dns.RR` 變成先 `s.each` 收到 map 再排序(或直接維持現有 typeMap local var 做法,把 `s.each` 的 callback 內 `typeMap[q] = rrs`)

## 6. Test scaffolding 的型別切換

- [x] 6.1 `internal/zone/zone_test.go`、`internal/zone/parser_test.go`、`internal/alias/override_test.go`、`internal/server/server_test.go` 的所有 `Records: make(map[string]map[uint16][]dns.RR)` 初始化改為 `make(map[string]*qtypeStore)`
- [x] 6.2 `internal/zone/parser_test.go` 三處直接存取 `z.Records[wantKey][dns.TypeA]`(或 TypeCNAME)的 case:改為透過 `z.Lookup(wantKey, dns.TypeA)`,保留 scenario 的斷言意圖(「wildcard owner 存到正確 key 下」)。`TestParseFile_BlankAndCommentLines_Skipped` 的 `total` 累計改走 `s.each`

## 7. 驗證 & 壓測

- [x] 7.1 `go test ./...` 全綠,涵蓋:本 change 新增三個 inline/promotion test、先前 `TestZone_LookupReturnsSharedBacking`、`TestZone_LookupByOwner`、`TestZone_LookupWildcard_*`、`TestFollowCNAME_*`、`internal/alias` 與 `internal/server` 整合 test、`internal/transfer` AXFR/NOTIFY test;`go tool golangci-lint run` 0 issues
- [x] 7.2 cross-compile `.deb`:`GOOS=linux GOARCH=amd64 make build && VERSION=0.11.0-inline go tool nfpm package --packager deb`;scp 到 bench-ns2、`dpkg -i`、restart shadowdns,journalctl 見到 `shadowdns ready views=7`
- [x] 7.3 從 bench-ns1 跑 3 輪 cold 壓測:CNAME `dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/cname-domains.txt`、A-domain `@/tmp/a-domains.txt`;raw 檔存 `.local/dnspyre/report/raw-{cname,adomains}-v110inline-run[1-3].txt`
- [x] 7.4 Memory peak 採樣:壓測結束後 `ssh bench-ns2 'grep -E "VmPeak|VmRSS" /proc/$(pidof shadowdns)/status'` 與 systemd memory peak
- [x] 7.5 產出 `.local/dnspyre/report/compare-qtypeindex-vs-inline.md`:v0.8.0 / v0.9.0-nopool / v0.10.0-qtypeindex / v0.11.0-inline 四方對比 QPS(CNAME + A-domain)與 memory peak;驗收:memory peak ≤ 16.3 GB(v0.9.0-nopool + 10%)且 CNAME QPS ≥ 12,956 且 A-domain QPS ≥ 11,933。未達時停下 pprof(heap profile `/debug/pprof/heap`)找剩餘 overhead 來源,於報告中列出並提 follow-up

## 8. Commit 與 PR(不產生 spec delta)

- [x] 8.1 commit 分段:`refactor(zone): inline single-qtype owner storage to cut memory overhead`(zone.go + classify.go + transfer/axfr.go + 全部 test scaffolding)+ `test(zone): assert shared backing survives inline-to-promoted transition`(三個新 test);PR 描述引用 `.local/dnspyre/report/compare-v090nopool-vs-qtypeindex.md` 為 memory regression 證據、`.local/dnspyre/report/compare-qtypeindex-vs-inline.md` 為驗收證據

## 驗收條件

- `go test ./...` 全綠
- `go tool golangci-lint run` 0 issues
- `TestQtypeStore_InlineToPromoted`、`TestZone_LookupReturnsSharedBacking_Promoted`、`TestZone_LookupSharedBackingAcrossPromotion` 全綠
- v0.11.0-inline cold memory peak ≤ 16.3 GB(v0.9.0-nopool 14.8 GB + 10%)
- v0.11.0-inline cold QPS:CNAME ≥ 12,956、A-domain ≥ 11,933(保留 `zone-records-qtype-index` 的收益)
- 外部 API(`Lookup` / `LookupWildcard` / `HasWildcard` / `FollowCNAME` / `AddRR`)簽章與行為無改動
