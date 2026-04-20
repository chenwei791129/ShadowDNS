## Context

`zone-records-qtype-index` 把 `Zone.Records` 從 `map[owner][]dns.RR` 升到 `map[owner]map[qtype][]dns.RR`,query 時避開 per-query filter loop,CNAME QPS 從 11,067 回升至 12,956(+17.1% vs v0.9.0-nopool、+9.2% vs v0.8.0)。

但實機壓測後驗收報告(`.local/dnspyre/report/compare-v090nopool-vs-qtypeindex.md`)顯示:

| Variant | Memory peak | Δ vs baseline |
|---|---:|---:|
| v0.9.0-nopool | 14.8 GB | — |
| v0.10.0-qtypeindex | **28.0 GB** | **+89%** |

design.md Risks 預估「每 owner 多一個 map header ~48 bytes,10M owner 約 +480 MB」,實測誤差 27×。溯源:

- Go 的 `runtime.hmap` 本身 ~48 bytes,但一旦 insert 第一個 key,會配一個 `runtime.bmap`(bucket)。
- 64-bit amd64 下 `bmap` 的基本 size:8 tophash bytes + 8 key slots(每 slot key size × 1)+ 8 value slots + overflow pointer + alignment padding,`map[uint16][]dns.RR` 的 `uint16` key 對齊到 2 bytes × 8 = 16,`[]dns.RR` 是 24 bytes × 8 = 192,再加 8 byte tophash + 8 byte overflow ptr = **約 224 bytes per empty-but-initialized bmap**。
- ShadowDNS 的 zone corpus 中大部分 owner 只有 1 種 qtype(ACME 驗證子網域、單一 A record、單一 CNAME)。以 50M 這類 owner × 224 bytes = 11.2 GB,對上 14.8→28.0 GB 的 +13.2 GB 增量,吻合。

本 change 的核心判斷:**sub-map 並非必要結構,只有當 owner 同時持有多個 qtype 時(例如 `example.com.` 同時有 A/AAAA/NS/MX/TXT)才需要它**。大多數 owner 的單一 qtype 情境可直接 inline 儲存,避開 bmap。

相關檔案:`internal/zone/zone.go`、`internal/zone/classify.go`、`internal/transfer/axfr.go`、`internal/zone/zone_test.go`、`openspec/specs/zone-parser/spec.md`。

## Goals / Non-Goals

**Goals:**

- Memory peak 降回 ≤ 16.3 GB(v0.9.0-nopool + 10%)。
- 保留 `zone-records-qtype-index` 帶來的 CNAME +9.2% / A +2.3% QPS 收益。
- 保持 `Lookup` / `LookupWildcard` / `HasWildcard` / `FollowCNAME` / `AddRR` 的**對外簽章與行為**,caller(handler、alias、transfer、view)零改動。
- `Lookup` 的 "shared-backing reference" 契約在 inline 與 promoted 狀態下都成立。

**Non-Goals:**

- 不改 Records 的對外語意(仍是「owner → qtype → RRs」的 O(1) 查找)。
- 不引入 `sync.Pool`、arena、或其他 allocator hack。
- 不保證 promoted → inline demotion(IXFR 刪光 owner 某 qtype 不做 demote)。
- 不動 parser、classifier、AXFR、NOTIFY 對外契約。
- 不加新 public API(qtypeStore 是 package-internal)。
- 不碰 DNS wire-format 行為。

## Decisions

### Records 型別改為 `map[string]*qtypeStore`

`qtypeStore` 是 package-internal struct,同時能表達「single-qtype inline」與「multi-qtype promoted」兩種狀態:

```go
type qtypeStore struct {
    // 為 true 時 qtype/rrs 有效、sub 為 nil;為 false 時 sub 有效、qtype/rrs 未使用。
    single bool
    qtype  uint16           // inline 狀態下 owner 擁有的唯一 qtype
    rrs    []dns.RR          // inline 狀態下此 qtype 的全部 RR
    sub    map[uint16][]dns.RR // promoted 狀態下的 qtype → RRs
}
```

`Records` 型別用 pointer:`map[string]*qtypeStore`,避免 inline → promote 時的 struct copy 成本。

**替代方案 A**:`qtypeStore` 用 value 不用 pointer(`map[string]qtypeStore`)。拒絕,inline→promote 時需要 `delete + insert` 或 `Records[key] = newStore`,前者兩次 hash,後者每次 promote 付 struct copy(56+ bytes)。pointer 版本只需 `s.single = false; s.sub = ...`,原地改。

**替代方案 B**:把 `single` 用「`sub == nil`」推斷,省一個 bool。拒絕,狀態推斷 implicit,reader 要反覆確認「sub nil 代表什麼」、「rrs nil + sub nil 又代表什麼」;explicit bool 換 zero cost 可讀性。

**替代方案 C**:inline 固定容量 4(`rrs [4]dns.RR` + `n uint8`),overflow 才走 slice。拒絕,array 長度被誇大時浪費(大部分 single-qtype owner 只有 1-2 個 RR),被低估時退化成 slice + overflow 邏輯複雜。slice + len/cap 已經是 Go 慣用 dynamic sizing。

### 狀態轉換只有「inline → promoted」、不做反向

Promotion trigger:AddRR 時 `s.single == true` 且 `rr.type != s.qtype`。

Demotion(promoted → inline)**不實作**。IXFR 刪光某個 qtype 時,`delete(s.sub, qtype)` 後可能剩一個 qtype 或零個 qtype,但 sub-map 的 bmap 已配置,回不去。

**理由**:demotion 需要在 delete 路徑加 size check + copy-to-inline + sub-map release 邏輯,複雜度明顯大於 memory 收益;production 的 zone 長期看 owner 類型分布傾向穩定,多 qtype owner 很少退化回單 qtype。

### `qtypeStore.each` helper 收斂所有 "iterate owner's all RRs" 的 caller

Classify 的 `filterBackupRecords` 與 transfer 的 `buildAliasRecords` / `collectNonSOA` 需要遍歷 owner 底下所有 qtype 的 RR。目前(`zone-records-qtype-index`)是 `for rrtype, rrs := range sub { ... }`。新加 helper:

```go
func (s *qtypeStore) each(fn func(qtype uint16, rrs []dns.RR)) {
    if s == nil { return }
    if s.single { fn(s.qtype, s.rrs); return }
    for q, r := range s.sub { fn(q, r) }
}
```

caller 改成 `s.each(func(q, rrs) { ... })`,delete 操作仍走原 map delete(只 promoted 才做)。

**替代方案**:在 zone.go 外暴露 `(Zone).EachRRsAt(owner, fn)` 公開 API。拒絕,目前 caller 在 internal package,不必 export;API surface 越小越好。

### Lookup 契約的「shared backing」如何在 inline / promoted 下都成立

`Lookup(owner, qtype)` 行為:

- inline + qtype 匹配 → return `s.rrs`(backing 就是 `qtypeStore.rrs`)。
- inline + qtype 不匹配 → return nil。
- promoted → return `s.sub[qtype]`(backing 就是 sub-map 中的 slice)。

`TestZone_LookupReturnsSharedBacking`(zone-records-qtype-index 留下的 test)驗證 `&stored[:cap][0] == &result[:cap][0]`。在本 change:
- inline path 仍指向 `qtypeStore.rrs` 的底層 array,test 綠。
- 新增 test `TestZone_LookupReturnsSharedBacking_Promoted` 驗證 promoted 路徑亦共享 backing。

### Test scaffolding 的型別切換

`zone-records-qtype-index` 讓 ~12 處 test 從 `make(map[string][]dns.RR)` 改成 `make(map[string]map[uint16][]dns.RR)`。本 change 再改成 `make(map[string]*qtypeStore)`。compile fail 就是安全網,一次性 mechanical sweep 全部更新;沒有隱藏 caller。

### 不產生 spec delta

`zone-records-qtype-index` 的 spec 寫「the slice returned by `Lookup` has the same underlying array address as the stored slice」。內部儲存層從 sub-map 移到 `qtypeStore.rrs`(inline)或 `qtypeStore.sub[qtype]`(promoted)後,caller 視角的 contract(shared backing)不變;spec 不需改。inline/promoted/cross-promotion 的 shared-backing 契約由本 change 的 unit test(task 1.1 的三個 test)保證。

## Risks / Trade-offs

- **inline → promoted 切換成本**:每個 owner 最多發生 1 次(單方向),cost 是「建 sub-map + 插 2 個 entries + 清 inline fields」,在 zone parse 期支付;對 parse 時間的影響應在個位數 %。
- **qtypeStore 本身 overhead**:`*qtypeStore` = 8 bytes pointer + struct 約 48 bytes(single bool + 7 bytes padding + uint16 qtype + 6 bytes padding + slice 24 bytes + map pointer 8 bytes)。一個 owner 的 overhead 從「hmap 48 + bmap 224 = 272 bytes」降到「pointer 8 + struct 48 = 56 bytes」,省 216 bytes/owner。以 50M single-qtype owner 計,省 ~10.8 GB,對得上目標(28 → 16 GB)。
- **`sub` field 未 init 但宣告占空間**:即使 inline 狀態 `sub` 是 nil slot,仍占 8 bytes。可以用 union-like encoding 把 `rrs` / `sub` 重疊,但 Go 不支援 union,硬擠會變 unsafe 或 interface,得不償失。
- **Promoted 狀態下 single/qtype/rrs 仍占空間但不用**:48 bytes × promoted owner 數的 waste。Promoted owner 佔少數(多 qtype owner 通常是 zone origin 這類,總數 << single-qtype owner),overhead 可忽略。
- **shared-backing 契約在 IXFR 動態更新下的存活**:若未來 IXFR 實作會對 rrs 做原地 update(例如 replace),contract 要求「caller 拿到 slice 期間不 mutate」——這點 `zone-records-qtype-index` 已有 doc comment 保證,本 change 繼承。
- **drift from `zone-records-qtype-index` spec scenario**:spec 的第二個 requirement "Lookup returns stored records as a shared-backing reference" 的 scenario 敘述 "same underlying array address as the stored slice";在 inline 狀態下 stored slice 就是 `qtypeStore.rrs`,shared 關係仍成立;test 會同時 cover inline 和 promoted。但 **若未來 promotion 發生在 Lookup 之後**,先前回傳的 slice 仍指向舊的 `qtypeStore.rrs`(現在 promoted 的 sub-map 會新建 slice header 但 copy 同一個 backing array?)**Mitigation**:promote 實作時 `s.sub[s.qtype] = s.rrs`,保持同一 backing array,舊 return value 的 shared-backing 契約不破。本 design 明確要求 promote 走此路徑。

## Migration Plan

1. 在 `internal/zone/zone.go` 定義 `qtypeStore` 與其 method(`each`、`lookup`、`addRR` helper)。
2. 把 `Zone.Records` 型別從 `map[string]map[uint16][]dns.RR` 改為 `map[string]*qtypeStore`。編譯 fail 會指出所有 test scaffolding + parser init + classify iterate + axfr iterate + parser_test direct access 的需更新點。
3. 改寫 `AddRR`:狀態機走 new→inline / inline-same-type→append / inline-diff-type→promote / promoted→sub-map-append。
4. 改寫 `Lookup` 與 `wildcardHit`(`LookupWildcard` 共用):inline 與 promoted 分支。
5. 改 `classify.filterBackupRecords`、`transfer.buildAliasRecords`、`transfer.collectNonSOA` 用新的 `each` helper。
6. 改所有 test scaffolding:`Records: make(map[string]*qtypeStore)`。
7. `parser_test.go` 中 `z.Records[wantKey][dns.TypeA]` 改為走 public Lookup,或 `z.Records[wantKey].lookup(dns.TypeA)` helper(package-internal)。
8. 加 unit test:
   - `TestQtypeStore_InlineToPromoted`:single qtype insert → 不同 qtype insert,驗證 state transition 與兩個 qtype 的 Lookup 結果正確。
   - `TestZone_LookupReturnsSharedBacking_Promoted`:與 inline 版本對稱,owner 插 2 個 qtype 使其 promoted,確認 Lookup 各 qtype 的 backing 與 internal storage 共享。
   - `TestZone_LookupSharedBackingAcrossPromotion`:先 Lookup 拿到 inline 狀態的 slice,觸發 promote,原 slice 仍指向 promote 後 sub-map 的同一 backing array(驗證 Risks 段的 mitigation)。
9. `go test ./...` + `go tool golangci-lint run` 綠。
10. cross-compile `.deb`、deploy bench-ns2(`0.11.0-inline`),ns1 跑 3 輪 cold CNAME + A-domain 壓測,確認 memory ≤ 16.3 GB,QPS ≥ 12,956 / 11,933。
11. Rollback:未 push 時 `git reset --hard` 回前一 commit(`zone-records-qtype-index` 的 top)或 `git revert` 本 change commits。對外 API 不變,revert 不影響 caller。

## Open Questions

- `qtypeStore` 的 struct layout 是否要主動對齊 cacheline?目前的 fields 排列會落在 56 bytes 左右,尚未與 cacheline boundary 對齊;本 change 先不 premature optimize,先測 memory 與 QPS,若 pprof 顯示新的 hot path 是 Lookup 的 struct field access 再考慮。
- `TestZone_LookupSharedBackingAcrossPromotion` 的邊界 case:如果 inline 狀態 `rrs` 的 `cap` 不足,append 時會 realloc 出新 backing array;在那之後 promote,promote 內的 `s.sub[s.qtype] = s.rrs` 指向的是「append 後的新 array」,並非 Lookup 先前回傳指向的「舊 array」。**Mitigation**:doc comment 警告 Lookup 返回值不得跨 AddRR 使用(這本來就是 zone 系統的 invariant:zone 是 SIGHUP reload 時整體換掉,不是原地修改)。本 change 不需新 code,只要 doc 清楚。
