## Why

`upgrade-go-1-26` 合併後實機壓測發現 CNAME QPS 相對 v0.8.0 回退 6.7%,A-domain 持平(-1.3%,在 noise 內)。pprof diff 定位 regression 根因:`internal/zone.filterByQtype` 內 `append(dst, rr)` 從 490 ms 飆到 2,080 ms(+325%),與全域 `runtime.tryDeferToSpanScan` +1.94s 對齊 —— Green Tea GC 的 write barrier 對每個 `*dns.RR` pointer 寫入 heap-backed slice 都觸發 defer-scan slow path。CNAME 路徑的 `FollowCNAME → Lookup → filterByQtype` 會對同一 query 呼叫多次 filter,放大了此 per-pointer barrier 成本。

本 change 從根本消除 filter 熱路徑:`Zone.Records` 改為雙層索引 `map[owner]map[qtype][]dns.RR`,把 qtype 分類的成本在 zone parse / SIGHUP 時一次性支付,query 端的 `Lookup` 變成純 map 查表,**零 append、零 barrier、零 per-call alloc**。

詳細量測與行級 profile 見 `.local/dnspyre/report/compare-v080-vs-v090nopool-pprof-diff.md`。

## What Changes

- `internal/zone.Zone.Records` 型別由 `map[string][]dns.RR` 改為 `map[string]map[uint16][]dns.RR`(owner → qtype → RR list)。
- `Lookup(owner, qtype)` 改為純兩層 map 查表,刪除 `filterByQtype` 呼叫。
- `LookupWildcard(qname, qtype)` 相同改動;`qtype == 0` 的 sentinel(`HasWildcard` 使用)改為檢查 owner 的 sub-map 是否 non-empty。
- **刪除** `filterByQtype` 函式與其兩個測試(`TestFilterByQtype_ReusesCallerBuffer`、`TestFilterByQtype_NilDstRetainsOldBehavior`)—— 真實 caller 歸零。
- `FollowCNAME` 的 `answer` slice 從每次呼叫 `make([]dns.RR, 0, MaxCNAMEDepth+1)` 改為 caller-provided buffer(簽章新增 `dst []dns.RR` 參數,zone.go 內部 caller 傳 nil fallback),為熱路徑的 slice alloc 消除再鋪一層優化入口。
- **API 契約強化**:`Lookup` / `LookupWildcard` 的 return value 是 `z.Records[owner][qtype]` 的直接 reference,caller **不得 mutate**(append、index write、sort 等);需要修改時應 copy。此契約以 doc comment 明示;violating 不會 panic 但會污染後續 query。
- `zone-parser` capability 新增 "RR 存入 Records 時按 qtype 分類索引" 的 requirement。
- 維持原有 `Lookup` / `LookupWildcard` / `FollowCNAME` / `HasWildcard` 的 **簽章與行為**(除 FollowCNAME 多一個 optional buffer param);外部 caller(`internal/alias`、`internal/server`)無需改動。

## Non-Goals

- **不改 Zone parser 對外介面**:parser 仍讀取 RFC 1035 格式 zone file,只改內部 RR 存入 `z.Records` 的結構。
- **不引入 `sync.Pool`**:先前 `upgrade-go-1-26` 實測 pool 在此 workload 是淨負擔(CNAME -9.4%),本 change 不復辟。
- **不改 IXFR / AXFR 增刪 RR 的對外契約**:只改內部 Records 的讀寫點以適配新結構。
- **不動 handler / alias / view / metrics**:這些只透過 `Lookup` / `LookupWildcard` / `FollowCNAME` 存取 zone,API 不變。
- **不保留 `filterByQtype` 作為 utility**:真實 caller 為零時,保留僅為假想未來需求是 over-engineering。
- **不在本 change 做更大範圍的 zone 結構重構**(如 owner trie、binary serialization),專注 qtype index 單一改動。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `zone-parser`:新增 "RR 存入 `Zone.Records` 時按 qtype 分類索引" 的 requirement,以支援 O(1) qtype-specific lookup。

## Impact

- **Affected specs**:`openspec/specs/zone-parser/spec.md`(delta)
- **Affected code**:
  - `internal/zone/zone.go`(Records 型別、Lookup / LookupWildcard / HasWildcard / FollowCNAME、刪除 filterByQtype)
  - `internal/zone/zone_test.go`(刪除 filterByQtype 兩個 test;其餘 Lookup / LookupWildcard / FollowCNAME test 應全綠,因為外部行為不變)
  - `internal/zone/classify_test.go`(若有 mutate return value 的 test 需修)
  - `internal/zone/zone.go` 的 parser 端(存入 Records 的程式碼)
- **Affected dependencies**:無
- **Performance target**:消除 `compare-v080-vs-v090nopool-pprof-diff.md` 指出的 `filterByQtype.append` +1.59s 熱點,預期 CNAME QPS 自 11,067 恢復到 ≥ v0.8.0 的 11,862(+7%),A-domain 維持 ~11,518。
- **Memory overhead**:每 owner 多一個 sub-map header(~48 bytes × owner 數量),預估 worst case < 500 MB(14.8 GB heap 量級下 < 4%)。實測於驗收階段確認。
