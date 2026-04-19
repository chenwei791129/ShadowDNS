## Context

`upgrade-go-1-26` 升級至 Go 1.26.0 + Green Tea GC(預設 ON)後,實機三方 + 四方冷啟壓測顯示 CNAME -6.7% 回退,A-domain 持平。為定位根因,在 v0.8.0 與 v0.9.0-nopool 各自的 binary 臨時加 pprof endpoint,於 CNAME 壓測中段採 60 s CPU profile 後做 `-diff_base` 分析,結果收斂為以下兩個互相印證的熱點:

| 熱點 | v0.8.0 flat | v0.9.0 flat | Δ |
|---|---:|---:|---:|
| `internal/zone.filterByQtype` | 670 ms | 2,290 ms | **+1,620 ms** |
| └─ 內部 `append(dst, rr)` 單行 | 490 ms | 2,080 ms | **+1,590 ms** |
| `runtime.tryDeferToSpanScan`(Green Tea write barrier) | 0 | 1,940 ms | **+1,940 ms** |

`make([]dns.RR, 0, len(rrs))` 在兩版本均 ~420–430 ms(持平),排除 alloc 本身的影響。regression 來源是 Green Tea GC 對 heap-backed slice 的 pointer write 啟用 defer-scan slow path;CNAME 路徑每跳 `FollowCNAME → Lookup → filterByQtype` 會對同一 query 累積多次 `append(*dns.RR)`,放大此 per-pointer barrier 成本。

**簡章擴展(filterByQtype 加 `dst` 參數)曾是 upgrade-go-1-26 規劃的優化入口**,目的是讓 caller 傳 pre-allocated buffer 避免 `make`。但 pprof 顯示 `make` 不是瓶頸;且 Go 的 escape analysis 會把 `Lookup` 的 return value 強制 move-to-heap(caller 保留使用),caller-side stack buffer 行不通 —— 簽章擴展被推翻並已從 `upgrade-go-1-26` history `git reset` 移除。

本 change 換一條路:**把 filter 從 query path 移走**。Zone 載入時就按 qtype 分類儲存,query 端純 map 查表,避開 append + barrier。

相關檔案:`internal/zone/zone.go`、`internal/zone/zone_test.go`、`internal/zone/classify_test.go`、`openspec/specs/zone-parser/spec.md`。

## Goals / Non-Goals

**Goals:**

- 消除 `filterByQtype` 這個 pprof 上可量測的 CNAME hot path 熱點(+1.62 s flat / 60 s profile)。
- 保持 `Lookup` / `LookupWildcard` / `HasWildcard` / `FollowCNAME` 的**簽章與外部行為**,使 `internal/alias`、`internal/server`、view matcher 等 caller 零改動。
- `Lookup` / `LookupWildcard` 的回傳值改為 `z.Records[owner][qtype]` 的直接 reference(不複製),進一步減少 per-call alloc;以 doc comment 強化 "caller must not mutate" 契約。
- `FollowCNAME` 的 `answer` slice 消除每次 `make([]dns.RR, 0, MaxCNAMEDepth+1)`,改由 caller buffer 傳入。
- CNAME 壓測相對 v0.8.0 至少 break-even(≥ 11,862 QPS),預期 +3%~+7%(Green Tea 在 A-domain 的 +6.4% 效益保留、CNAME barrier 消除)。

**Non-Goals:**

- 不引入 `sync.Pool`(upgrade-go-1-26 已驗證淨負)。
- 不換 zone data structure 的其他面向(如 owner trie、radix、binary serialization)。
- 不動 IXFR / AXFR / SIGHUP reload 的對外契約。
- 不保留 `filterByQtype` 作為 utility fallback(真實 caller = 0 時保留 = dead code)。
- 不改 DNS wire-format 行為、不改 CLI / 設定檔。

## Decisions

### Records 結構採雙層 map `map[string]map[uint16][]dns.RR`

最外層 key 為 canonical owner(lowercased + trailing dot),第二層 key 為 `dns.Type*` 常數(`uint16`),值為該 qtype 下全部 RR。

zone parse 時把 RR 依 `rr.Header().Rrtype` 分到對應 sub-map;read path(`Lookup(owner, qtype)`)變成純兩層 map 查表。

**替代方案 A**:單層 `map[string][]dns.RR` + 每 owner 另配一個 `map[uint16]int` owner→qtype→index 索引。拒絕,兩次 map 查表成本一樣但讀邏輯更複雜,sub-slice 切片仍需走 `rrs[start:end]`,對 Go escape analysis 不利。

**替代方案 B**:把 RR 依 qtype 排序後存 flat slice + 每 qtype 的 `(start, end)` 索引。拒絕,每次 IXFR 增刪 RR 需 re-sort 整條 slice,維護成本高;讀邏輯雖只一次查表但整體複雜度升高。

**替代方案 C**:改 `map[ownerQtypeKey][]dns.RR`(key 是 `owner + "|" + strconv(qtype)` 或 struct)。拒絕,`HasWildcard` 需要「owner 下是否有任何 qtype」的反查,struct key 實作能做但 idiom 繞;string concat key 每次 query 多一次 alloc,與本 change 目標衝突。

### 刪除 `filterByQtype`

Records 改為 double-index 後,`filterByQtype` 的唯一 caller(`Lookup` / `LookupWildcard` 共三處)全部消失,函式變 dead code。

**替代方案**:保留作為 utility。拒絕,真實 caller = 0 時保留僅為假想未來需求;其原始簽章擴展動機(caller buffer)已被 pprof 推翻,保留會誤導後來的 reader 以為 zone 熱路徑仍是 filter 模式。

### `HasWildcard` 與 qtype=0 sentinel

現行 `HasWildcard(qname)` 內部呼叫 `LookupWildcard(qname, 0)` 並以 `found` bool 回報。double-index 下 `LookupWildcard(qname, 0)` 語意改為「檢查 owner 是否有 sub-map,且 sub-map 非空」,不走 qtype map lookup。

**理由**:`qtype=0` 在 DNS 本身不是有效 query type,只有 HasWildcard 這條內部 path 使用。保留 `qtype=0` 作 sentinel 比新增 `LookupWildcardAny` 公開 API 乾淨。

### `FollowCNAME` 加 caller-buffer 參數

簽章由 `FollowCNAME(initial []dns.RR, qtype uint16) []dns.RR` 擴為 `FollowCNAME(dst []dns.RR, initial []dns.RR, qtype uint16) []dns.RR`;`dst == nil` 時 fallback 到 `make([]dns.RR, 0, MaxCNAMEDepth+1)`(舊行為)。本 change 內 caller 仍全部傳 nil(行為等同舊版),未來優化可接真實 buffer。

**理由**:`FollowCNAME.answer` 在 CNAME 路徑每次呼叫都 alloc 新 slice;pprof 沒標紅是因為它一次 query 只呼叫一次(不像 filterByQtype 被呼叫多次),但 append `initial` + 每跳結果仍觸發 barrier。結構已經動,順便鋪這條 buffer 入口成本接近零。

**替代方案**:不擴展簽章,等將來需要再加。拒絕,本 change 已在重構 `FollowCNAME` 內部(Lookup return 變 reference,需小心 alias hazard),同一次改完比分兩次 PR 乾淨。

### Lookup / LookupWildcard 回傳值「caller must not mutate」契約

`Lookup(owner, qtype)` 回傳 `z.Records[owner][qtype]` 的直接 reference(不 copy)。目前三個 caller(`internal/alias/override.go`、`FollowCNAME` 內部、`Lookup` 自己遞迴)都只讀不改,契約可安全引入;以 doc comment 明示。

**替代方案**:每次回傳 `append([]dns.RR(nil), z.Records[owner][qtype]...)` 一份 copy。拒絕,double-index 的效能收益有一半來自 zero-copy return;做 copy 等於把 filterByQtype 的 append 成本搬到 Lookup 層,白忙一場。

**風險**:未來 caller 若 mutate 了 return value,會污染 zone 的狀態且跨 query persist。Mitigation:doc comment 明示 + 在 test 內加一個 "mutate detection" test(掃所有 caller grep 是否有 `append(lookupResult,...)` 或 `lookupResult[i] = ...`)。

### 解刪 filterByQtype 時 `zone_test.go` 的兩個測試

`TestFilterByQtype_ReusesCallerBuffer` / `TestFilterByQtype_NilDstRetainsOldBehavior` 隨函式刪除。`TestZone_LookupByOwner`、`TestZone_LookupWildcard_*`、`TestFollowCNAME_*` 等外部行為測試保留 —— 外部契約未變,這些測試應全綠,驗證 refactor 不破壞語意。

## Risks / Trade-offs

- **Memory overhead**:每個 owner 多一個 `map[uint16][]dns.RR` 的 hashmap header(Go runtime 上 ~48 bytes 含 growth overhead)。若 zone 有 10M owner,約 +480 MB;14.8 GB heap 基準下 < 3.3%。**Mitigation**:於驗收階段實測 `/etc/namedb/master/` 全載入後 memory peak,若超過 +10% 需評估 struct packing 或延遲 sub-map 建立(lazy init)。
- **Zone parse / SIGHUP reload 慢化**:RR insert 由 `append` 變為 two-level map write。單個 parse worker 的時間影響預估個位數 %,在分鐘級 parse 中可忽略。**Mitigation**:`smoke` test 與 integration test 檢查 reload 時間無顯著回退(目前 14.8 GB heap 的 full reload ~1–2 min,+10% 上限可接受)。
- **Lookup return value 誤 mutate**:見 Decisions 第 5 點,doc + 測試驗證。
- **qtype=0 路徑**:`LookupWildcard(qname, 0)` 的語意變化需在 spec 明示;外部無人呼叫此路徑(只有 `HasWildcard` 內部),但若 future code 誤以為 qtype=0 能查所有 RR 會意外 behavior。**Mitigation**:於 doc comment 明示 qtype=0 是 "any qtype / existence check" sentinel。
- **壓測未達標**:若 CNAME 實測 < v0.8.0 的 11,862,表示 regression 還有其他來源(例如 CNAME path 上的其他 append / barrier 點)。**Mitigation**:再跑一次 pprof diff 看剩餘熱點,視情況延伸 scope 或回頭 revert。

## Migration Plan

1. 在 `internal/zone/zone.go` 把 `Records` 宣告改為 double-index,順便把 parser 端 insert 邏輯改成 two-level map write。這一步會讓整個 zone package **編譯失敗**(其他 function 還在用舊 type),是刻意的 "compiler-guided refactor" 起點。
2. 逐一修 `Lookup` / `LookupWildcard` / `HasWildcard` / `FollowCNAME` 的 read path,直到 `go build ./internal/zone/...` 綠。
3. 刪除 `filterByQtype` + 兩個對應 test。
4. 跑 `go test ./...` 全綠 + `go tool golangci-lint run` 0 issues。此時外部 caller(alias / server / view)應**完全沒動**;若有測試壞掉代表語意 refactor 有漏。
5. 以 `make deb` + `scp` + `dpkg -i` 部署 ns2(版本 `0.10.0-qtypeindex`),從 ns1 跑 3 輪 cold CNAME + A-domain 壓測。
6. 把結果與 v0.8.0 / v0.9.0-nopool 對比,寫入 `.local/dnspyre/report/compare-v090nopool-vs-qtypeindex.md`,確認 CNAME ≥ 11,862,A-domain ≥ 11,518。
7. Rollback 路徑:未 push 時 `git reset --hard` 回本 change 前一個 commit;已 push 但未 release 時 `git revert` 本 change 的 commits(影響範圍只有 zone package 內部,不牽連對外 API)。

## Open Questions

- `FollowCNAME` 的 caller buffer 實際接入(傳 non-nil `dst`):是否在本 change 同步做,還是也延後到下一個 change(需先量測接入後的 escape 是否被編譯器正確消除)?目前 design 傾向**只擴展簽章、caller 傳 nil**,理由同 `filterByQtype` 的前車之鑑 —— 先測量後再實際 hook。
- Memory overhead 實測值:driver code 已完備,於驗收階段取實際 zone full load 後的 RSS 對比,若超過 +10% 考慮 sub-map lazy init(例如 owner 只有一個 qtype 時不建 sub-map,改為 inline single slice)。
