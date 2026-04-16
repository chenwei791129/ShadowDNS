## Context

ShadowDNS 是 authoritative-only DNS server。先前的 cname-synthesis change 實作了 CNAME 合成（當查詢 qtype ≠ CNAME 但該 name 有 CNAME 紀錄時回傳 CNAME），但明確排除了 CNAME following。然而 RFC 1034 §3.6.2 要求 authoritative server 在自身 zone 資料內「restart the query」，這不是遞迴，而是使用本地資料繼續查詢。

目前的查詢流程（root zone path）：
1. `Lookup(qname, qtype)` → 空
2. `Lookup(qname, CNAME)` → 找到 CNAME → 立即回傳，結束

BIND 的行為：
1. 同上 1-2
2. 檢查 CNAME target 是否在 zone 內 → 是 → `Lookup(target, qtype)` → 合併回傳

兩條路徑受影響：`handleRootQuery`（root zone）和 `alias.Resolve`（backup zone）。

## Goals / Non-Goals

**Goals:**

- 當 CNAME target 在同一 zone 內（in-bailiwick）時，follow CNAME 並在 ANSWER section 回傳完整 chain + 最終紀錄
- 支援 CNAME chain（A → CNAME B → CNAME C → A record），設迴圈上限防止無限迴圈
- Root zone 與 backup zone 路徑均須支援
- Wildcard 匹配產生的 CNAME 也須 follow（wildcard CNAME synthesis 路徑）

**Non-Goals:**

- 不實作遞迴解析（out-of-bailiwick target 仍只回傳 CNAME）
- 不處理 Additional section glue record
- 不處理 DNAME

## Decisions

### 使用 zone origin suffix 判斷 in-bailiwick

判斷 CNAME target 是否在同一 zone 內的方式：檢查 target FQDN 是否以 `"."+zone.Origin` 結尾或等於 `zone.Origin`。這與 `LookupWildcard` 中已使用的 `originSuffix` 判斷一致，無需引入新的 zone membership 概念。

**替代方案**：在 `Zone` struct 上新增 `Contains(name)` 方法 — 雖然更語意化，但本質上只是 suffix check 的包裝，複雜度不值得。

### 在 handler/alias 層實作 following loop，不修改 zone 層

CNAME following 邏輯放在 `handleRootQuery` 和 `alias.Resolve` 中，而非在 `Zone.Lookup` 內部自動 follow。原因：
1. `Zone.Lookup` 是純粹的 key-value 查詢，保持簡單
2. 不同查詢路徑（root vs backup）的 owner name rewrite 邏輯不同，放在各自的 handler 層更清晰
3. Wildcard CNAME 需要 owner rewrite 後才能 follow，zone 層不知道 rewrite context

### CNAME chain 上限設為 8

RFC 1034 未規定具體上限，但 BIND 預設使用的 CNAME chain 上限為 16（`named.conf` 的 `max-cname-follow`）。ShadowDNS 作為 authoritative server 處理的是自己的 zone 資料，合理的 chain 長度不應超過幾層。設為 8 足以覆蓋所有合理場景，同時防止意外的迴圈配置。

**替代方案**：使用 visited set 偵測迴圈 — 增加分配開銷，且 8 層上限已足夠兼顧兩者。

### Backup zone path 在 root zone 空間內 follow，最後統一 rewrite

`alias.Resolve` 已將 qname rewrite 到 root zone namespace 後再查詢。CNAME following 同樣在 root zone namespace 中進行（因為 CNAME target 本身就是 root zone 的 name），最後對所有收集到的 RR 統一呼叫 `RewriteRR` 轉換回 backup namespace。

## Risks / Trade-offs

- **效能影響**：每次 CNAME following 多做 1-N 次 `Lookup`（N = chain 長度）。由於 `Lookup` 是 map 查詢（O(1)），且 chain 通常只有 1 層，影響可忽略
- **Zone 配置迴圈**：若 zone 資料中存在 CNAME 迴圈（A → B → A），8 層上限會截斷並回傳已收集的 partial chain。這與 BIND 的行為一致（BIND 也會截斷）
- **Wildcard CNAME chain**：wildcard 匹配後 follow CNAME target 時，target 可能需要再次做 wildcard matching。此情況較罕見但需支援
