## Context

目前 `zone.Lookup(owner, qtype)` 使用 `z.Records[owner]` 做 exact map lookup。Zone file 中的 wildcard 紀錄（如 `*.example.com. CNAME target.`）已由 miekg/dns parser 正確解析並存入 `Records["*.example.com."]`，但查詢 `foo.example.com.` 時不會命中這個 key。

RFC 4592 定義 wildcard matching 規則：當 exact match 失敗時，server 須從 qname 逐級剝離 leftmost label，尋找 `*.<remaining>` 形式的 wildcard owner。但若剝離過程中遇到 existing name（empty non-terminal），wildcard 匹配即停止。

## Goals / Non-Goals

**Goals:**

- 實作 RFC 4592 compliant wildcard matching，使 ShadowDNS 能正確回應 wildcard zone 紀錄
- 與 `cname-synthesis` change 自然銜接（wildcard CNAME 是最常見的 wildcard 用例）
- 保持 exact match 的 O(1) 效能不變

**Non-Goals:**

- DNSSEC wildcard proof（NSEC/NSEC3）
- 預先計算 ENT index 的效能最佳化
- 多層 wildcard（`*.*.example.com`）— RFC 4592 只匹配 leftmost `*` label

## Decisions

### Wildcard lookup 演算法放在 zone.LookupWildcard 方法中

在 `internal/zone/zone.go` 新增 `LookupWildcard(qname string, qtype uint16) ([]dns.RR, bool)` 方法。回傳匹配的紀錄與是否命中 wildcard 的 boolean。

演算法：
1. 從 qname 開始，逐級剝離 leftmost label 得到 parent（如 `foo.bar.example.com.` → `bar.example.com.` → `example.com.`）
2. 每級嘗試 `*.parent` 作為 wildcard key 查 `z.Records`
3. 若命中，過濾 qtype 後回傳（owner name 仍為 `*` 形式，由 caller rewrite 為原始 qname）
4. 若 `parent` 本身存在於 `z.Records`（empty non-terminal），停止搜尋回傳空（RFC 4592 §2.2.1 blocker）
5. 持續直到 parent 等於 zone origin

**替代方案：修改 Lookup 方法**
不採用，因為 Lookup 的 exact match 語意被多處使用（AXFR、NODATA 判斷），改變語意風險太大。

**替代方案：建立 parse-time wildcard index**
不採用。現階段 zone 規模下，per-query 逐級搜尋的成本可接受（最多剝離 label 數次，每次一次 map lookup）。若日後 profiling 顯示瓶頸再最佳化。

### Wildcard 合成的 owner name 由 handler 層改寫

`LookupWildcard` 回傳的紀錄仍使用 `*` owner name。Handler（`handleRootQuery`/`handleBackupQuery`）負責將 answer 中的 owner name 改寫為原始 qname，per RFC 4592 §2.2。

理由：zone 層不應知道「當前查詢的 qname 是什麼」——這是 handler 的責任。保持 zone 層的 pure data 語意。

### Backup zone wildcard 透過 alias.Resolve 擴充

`alias.Resolve` 在 `rootZone.Lookup` 回空後，增加 `rootZone.LookupWildcard` 的 fallback 路徑。匹配後同樣套用 `RewriteRR` 轉換 owner name。

### Empty non-terminal 以 Records map 存在性判斷

判斷一個 name 是否為 ENT 的方式：`z.Records[name]` 存在即視為 ENT（即使底下的紀錄是其他 type）。這是保守判斷——如果一個 name 有任何紀錄，它就不是「不存在的 name」，wildcard 不應覆蓋它。

## Risks / Trade-offs

- **[效能] 逐級搜尋增加 per-query 成本** → 每多一層 label 增加一次 map lookup。實務上 zone 中 wildcard 通常在 2-3 層內命中。若 profiling 顯示問題，可改為 parse-time 建立 wildcard trie，但目前不需要。
- **[正確性] ENT 判斷可能誤判** → 如果 zone file 中有 `bar.example.com. TXT "..."` 且 `*.example.com. A 1.2.3.4`，查詢 `foo.bar.example.com. A` 時，`bar.example.com.` 作為 ENT blocker 會阻止 wildcard 匹配。這是 RFC 4592 的正確行為。
- **[與 cname-synthesis 的順序依賴]** → Wildcard lookup 回傳的紀錄可能是 CNAME type。若 `cname-synthesis` 尚未實作，wildcard CNAME 查 A 仍會回 NODATA。建議先 apply `cname-synthesis` 再 apply `wildcard-support`。
