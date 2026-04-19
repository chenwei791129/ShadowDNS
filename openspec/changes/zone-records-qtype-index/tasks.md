## 1. TDD 紅燈:Lookup returns stored records as a shared-backing reference

- [x] 1.1 於 `internal/zone/zone_test.go` 新增 `TestZone_LookupReturnsSharedBacking`,驗證「Lookup returns stored records as a shared-backing reference」契約:建一個小型 zone 含 owner `a.example.com.` 的兩筆 A 紀錄,呼叫 `z.Lookup(...)` 取得 result,與直接存取 `z.Records["a.example.com."][dns.TypeA]` 的第一個元素地址比對(用 `&s[:cap(s)][0]` 比 pointer)確認共享 backing。此 test 在 Records 型別改為雙層 map 前會編譯失敗 —— 即為紅燈

## 2. Index parsed records by owner and qtype (Records 結構採雙層 map `map[string]map[uint16][]dns.RR`)

- [x] 2.1 將 `internal/zone/zone.go` 的 `Zone.Records` 宣告由 `map[string][]dns.RR` 改為 `map[string]map[uint16][]dns.RR`,並於型別上方加 doc comment 說明「owner → qtype → RR list;回傳的 slice 與內部 storage 共享底層陣列,caller 不得 mutate」
- [x] 2.2 修改 `internal/zone/parser.go` 的 RR insert 邏輯,實作「index parsed records by owner and qtype」:對每個解析出的 RR,依 `rr.Header().Rrtype` 寫入 `z.Records[owner][rrtype]`,owner sub-map 不存在時 lazy init(`z.Records[owner] = map[uint16][]dns.RR{}`)

## 3. Read path 改造(HasWildcard 與 qtype=0 sentinel、FollowCNAME 加 caller-buffer 參數)

- [x] 3.1 `Lookup(owner, qtype)` 改為 `z.Records[owner][qtype]` 兩層查表,刪除對 `filterByQtype` 的呼叫;owner 不存在或 qtype sub-entry 不存在時回傳 nil slice(caller 已以 `len() == 0` 判斷,不破壞現有行為)
- [x] 3.2 [P] `LookupWildcard(qname, qtype)` 的兩個 wildcard 匹配點(parent-level 迴圈內的 `*.` + parent 與 origin-level 的 `*.` + z.Origin)改為雙層查表,移除 `filterByQtype` 呼叫
- [x] 3.3 [P] `HasWildcard` 與 qtype=0 sentinel:`LookupWildcard` 的 `qtype == 0` 語意改為「檢查 wildcard owner 是否有 sub-map 且 sub-map 非空(即存在任何 qtype 的紀錄)」,維持 `HasWildcard(qname)` 的行為不變
- [x] 3.4 [P] `FollowCNAME` 加 caller-buffer 參數:簽章由 `(initial []dns.RR, qtype uint16)` 擴展為 `(dst []dns.RR, initial []dns.RR, qtype uint16)`;`dst == nil` 時 fallback `make([]dns.RR, 0, MaxCNAMEDepth+1)` 維持舊行為;同步更新 package 內部所有 caller 傳 nil

## 4. 解刪 filterByQtype 時 `zone_test.go` 的兩個測試(刪除 `filterByQtype`)

- [x] 4.1 刪除 `internal/zone/zone.go` 的 `filterByQtype` 函式(連同其 doc comment)
- [x] 4.2 [P] 刪除 `internal/zone/zone_test.go` 的 `TestFilterByQtype_ReusesCallerBuffer` 與 `TestFilterByQtype_NilDstRetainsOldBehavior` 兩個 test

## 5. API 契約 doc:Lookup / LookupWildcard 回傳值「caller must not mutate」契約

- [x] 5.1 `Lookup` 與 `LookupWildcard` 的 doc comment 加「Returns the stored slice as a direct reference. Callers MUST NOT mutate the returned slice (no element assignment, no append sharing its capacity, no sort) — doing so corrupts the zone's internal state.」

## 6. 驗證 & 壓測

- [x] 6.1 `go test ./...` 全綠(涵蓋新增的 `TestZone_LookupReturnsSharedBacking`、既有 Lookup/LookupWildcard/HasWildcard/FollowCNAME tests、`internal/alias` 與 `internal/server` 整合 tests);`go tool golangci-lint run` 0 issues
- [x] 6.2 以 cross-compile 建 `.deb`:`GOOS=linux GOARCH=amd64 make build && VERSION=0.10.0-qtypeindex go tool nfpm package --packager deb`;scp 到 bench-ns2、`dpkg -i`、重啟 shadowdns,journalctl 看到 `shadowdns ready views=7`
- [x] 6.3 從 bench-ns1 跑 3 輪 cold 壓測:`dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/{cname,a}-domains.txt`,raw 檔存 `.local/dnspyre/report/raw-*-v100qtypeindex-run[1-3].txt`
- [x] 6.4 產出 `.local/dnspyre/report/compare-v090nopool-vs-qtypeindex.md`,將 v0.8.0 / v0.9.0-nopool / v0.10.0-qtypeindex 三方 cold QPS 對比;驗收條件:CNAME ≥ 11,862 且 A-domain ≥ 11,518(皆取自 v0.8.0 baseline);若未達 → 停下重採 pprof 分析剩餘熱點,於報告中列出來源並提 follow-up 決策
- [x] 6.5 Memory peak 對比:`ssh bench-ns2 'grep -E "VmPeak|VmRSS" /proc/$(pidof shadowdns)/status'`,與 v0.9.0-nopool 的 14.8 GB peak 對比;超過 +10% 時於本 change 內評估 owner 單 qtype 時 sub-map lazy init,否則進入 follow-up change 處理並於驗收報告中記錄數值

## 7. Commit 與 PR

- [ ] 7.1 commit 分段:`refactor(zone): index Records by qtype and remove filterByQtype`(zone.go + parser.go + zone_test.go 的結構改動與 filter test 刪除)+ `test(zone): assert Lookup returns shared-backing reference`(新 test);PR 描述引用 `.local/dnspyre/report/compare-v080-vs-v090nopool-pprof-diff.md` 為 regression 分析、`.local/dnspyre/report/compare-v090nopool-vs-qtypeindex.md` 為驗收證據

## 驗收條件

- `go test ./...` 全綠
- `go tool golangci-lint run` 0 issues
- `TestZone_LookupReturnsSharedBacking` 綠燈
- v0.10.0-qtypeindex cold QPS:CNAME ≥ 11,862(v0.8.0) 且 A-domain ≥ 11,518(v0.9.0-nopool)
- pprof `-diff_base v090nopool-cname.pprof v100qtypeindex-cname.pprof` 顯示 `filterByQtype` 不再出現於 top 熱點(已刪除)
- Memory peak vs v0.9.0-nopool ≤ +10%(否則於報告記錄並開 follow-up)
