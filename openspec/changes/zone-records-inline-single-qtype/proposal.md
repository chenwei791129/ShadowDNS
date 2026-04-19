## Summary

Zone.Records 的第二層 map 改為「lazy promotion」—— owner 只有單一 qtype 時 inline 存,出現第二種 qtype 才配 sub-map。目標:消掉 `zone-records-qtype-index` 帶來的 +89% memory overhead,同時保留 +9.2% CNAME QPS 收益。

## Motivation

`zone-records-qtype-index` 於 2026-04-19 合併後,實機驗收確認 CNAME QPS 從 v0.9.0-nopool 的 11,067 回升至 12,956(vs v0.8.0 +9.2%)。**但 memory peak 從 14.8 GB 飆到 28.0 GB(+89%),遠超 design 預估的 <4%**,只剩 3 GB VM headroom。

pprof 與 Go runtime 行為推斷 overhead 來源:

- `map[uint16][]dns.RR` 即使只插一個 key,runtime 會 allocate 一個 `bmap`(bucket)≈ 208 bytes on 64-bit amd64(8 tophash bytes + 8 key slots × 2 + 8 value slots × 24 + overflow pointer)。
- ShadowDNS 的 zone corpus 包含數千萬 owner,大部分 owner 只有 1 種 qtype(例如單一 `A` 或單一 `CNAME`)。以 50M 這類 owner 計,overhead ≈ 50M × 208 bytes ≈ 10.4 GB,與實測 +13.2 GB 增量吻合。

本 change **不改 Lookup / LookupWildcard / HasWildcard / FollowCNAME / AddRR 的外部簽章與行為**,也不碰雙層索引的語意;只把「owner → qtype → RRs」的內部儲存改成:

- **single-qtype owner**:inline `(qtype uint16, rrs []dns.RR)`,零 map allocation。
- **multi-qtype owner**:promote 到 `map[uint16][]dns.RR`(沿用 `zone-records-qtype-index` 的結構)。

預期 memory 壓回 v0.9.0-nopool 級(14.8 GB 附近),同時保留本輪雙層索引帶來的 query-time 收益。

詳細量測見 `.local/dnspyre/report/compare-v090nopool-vs-qtypeindex.md` 的 "Memory Peak 對比" 段。

## Proposed Solution

`Zone.Records` 型別改為 `map[string]*qtypeStore`,其中 `qtypeStore` 是內部 struct:

```go
type qtypeStore struct {
    // inline 狀態:single == true 時 qtype/rrs 儲存 owner 的唯一 qtype;
    // promoted 狀態:single == false 時 sub 非 nil,qtype/rrs 未使用。
    single bool
    qtype  uint16
    rrs    []dns.RR
    sub    map[uint16][]dns.RR
}
```

- **AddRR**:
  1. owner 不存在 → 新建 `qtypeStore{single: true, qtype: rr.type, rrs: [rr]}`。
  2. owner 存在且 inline 且 qtype 相同 → 直接 append 到 `rrs`。
  3. owner 存在且 inline 且 qtype 不同 → promote:建 sub-map 把原本的 `(qtype, rrs)` 塞進去,加上新 qtype 的新 entry,清 single/qtype/rrs。
  4. owner 存在且已 promoted → 寫入 `sub`。
- **Lookup(owner, qtype)**:
  - 查 `Records[owner]`,nil → 回 nil。
  - inline + qtype 相符 → 回 `rrs`;相異 → 回 nil。
  - promoted → 回 `sub[qtype]`。
- **LookupWildcard(qname, qtype)**:外層邏輯不變(仍是走 label 樹);內層 `wildcardHit` 改吃 `*qtypeStore`,qtype=0 sentinel 改為「owner 存在且(inline 或 sub non-empty)」。
- **HasWildcard**、**FollowCNAME**、**Classify filterBackupRecords**、**transfer AXFR/NOTIFY**:只需要改 iterate inner 的方式(加一個小 helper `(s *qtypeStore) each(func(uint16, []dns.RR))`),邏輯不變。

Benchmark 驗收:
- CNAME QPS ≥ 12,956(回到 `zone-records-qtype-index` 的水準,不倒退)。
- A QPS ≥ 11,933。
- Memory peak ≤ 16.3 GB(v0.9.0-nopool 的 14.8 GB +10%)。
- `go test ./...` 全綠(保留 `TestZone_LookupReturnsSharedBacking`,再加 inline↔promote 狀態轉換的 unit test)。

## Non-Goals

- **不改對外 API**:`Lookup` / `LookupWildcard` / `HasWildcard` / `FollowCNAME` / `AddRR` 簽章與行為維持 `zone-records-qtype-index` 後的版本。
- **不回退 filterByQtype**:已刪除,不復辟。
- **不動 zone parse 對外介面**(RFC 1035 + $INCLUDE 支援)。
- **不改 IXFR / AXFR / SIGHUP reload 對外契約**。
- **不引入 sync.Pool 或 arena allocator**:都是先前實測或預判淨負 / 超 scope。
- **不改 public spec**:`zone-parser` capability 的 requirements 由 `zone-records-qtype-index` 提供(shared-backing reference 等)行為不變。本 change 僅內部 storage representation 變更,不產生新的 spec delta。
- **不保證 demote**:promoted owner 即使後續 entries 被刪光(IXFR remove)也不 demote 回 inline;demotion 的複雜度收益不足。

## Alternatives Considered

### A. 用 `map[string][2]any` 或 interface 包裝

拒絕:interface 每個 entry 額外 16 bytes overhead;`[2]any` 無法透過 type system 保證 slot 用法正確性。

### B. 按 RR 層級 pool,不動 owner index

拒絕:`upgrade-go-1-26` 驗收實測 sync.Pool 在此 workload 淨負(CNAME -9.4%)。

### C. 改用 flat slice + `(owner, qtype) → (start, end)` secondary index

拒絕:AddRR / IXFR 增刪 RR 需重排 flat slice 或維護 free list,複雜度指數成長。雙層索引在 insertion 維持 O(1),flat slice 走不到。

### D. 接受 memory 現狀,靠擴大 VM RAM 解決

拒絕:VM size 翻倍僅解一輪,`zone-records-qtype-index` 的 27× 預估誤差代表 per-owner overhead 是本質問題,下一次 corpus 成長會再撞牆。

## Impact

- **Affected specs**:無(純內部 refactor,外部行為與 spec contract 不變)。
- **Affected code**:
  - `internal/zone/zone.go`(新增 `qtypeStore` 型別;`Records` 型別改為 `map[string]*qtypeStore`;`AddRR`、`Lookup`、`LookupWildcard`、`wildcardHit` 改寫)
  - `internal/zone/parser.go`(init `Records` map type)
  - `internal/zone/classify.go`(filterBackupRecords 的 iterate)
  - `internal/zone/zone_test.go`(新增 inline↔promote 狀態轉換 test;既有 test 依新 Records 型別調整 test scaffolding `Records: make(map[string]*qtypeStore)`)
  - `internal/zone/parser_test.go`(`z.Records[wantKey][qtype]` 改為 `z.Records[wantKey].Lookup(qtype)` 或類似 helper)
  - `internal/alias/override_test.go`(Records init)
  - `internal/server/server_test.go`(Records init × 3)
  - `internal/transfer/axfr.go`(iterate 改走 `qtypeStore.each`)
  - `internal/transfer/notify.go`(無改動:已用 `z.Lookup(z.Origin, TypeNS)`,走 public API)
  - `internal/alias/override.go`(無改動:走 public API)
  - `internal/server/handler.go`(無改動:走 public API)
- **Affected dependencies**:無。
- **Performance target**:
  - memory peak ≤ 16.3 GB(vs 當前 28.0 GB,省 ~11.7 GB)。
  - CNAME QPS ≥ 12,956;A QPS ≥ 11,933。
- **Deployment risk**:`Zone.Records` 是 package-internal struct field,但目前有 ~10 處 test scaffolding 直接 `make(map[string]map[uint16][]dns.RR)` 初始化。本 change 後皆需改 `make(map[string]*qtypeStore)`,compile fail 會提示所有需要更新的點。
