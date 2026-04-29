## Context

ShadowDNS 目前在多個路徑強制把 DNS name lowercase，導致 response 與現代 authoritative server（BIND9 ≥9.9.5、Knot、NSD、PowerDNS Auth）的 case-preserving 行為不一致。Bug surface 涵蓋三個層面：

1. **Alias config 端**：`internal/dnsutil/dnsutil.go:21-26 Canonicalize` 把 alias root / backup 全部 lowercase 儲存。即使 yaml 寫 `Example.com`，到 `RewriteRR` 時已是 `example.com.`。
2. **Alias rewrite 端**：`internal/alias/rewrite.go:42-53 RewriteName` 與 `internal/alias/rewrite.go:76-98 RewriteNameAnywhere` 內部 `lower := strings.ToLower(n)` 後所有 return path 都返 lowercase。
3. **Handler 端**：`internal/server/handler.go:83 qname := strings.ToLower(q.Name)` 把 query name lowercase 後丟進整個 pipeline；`rewriteWildcardOwner(records, qname)` 把 lowercased qname 當作 wildcard owner 寫進 response，造成 case loss。

非 alias 路徑經測試 owner case 來自 zone storage（zone parser 雖呼叫 `strings.ToLower` 但只用於 in-zone check，未 mutate RR）— 但 alias 路徑與 wildcard 路徑都會 lowercase。

dnspyre 的 `www.example.com` 不一致是 cosmetic 表象，**真正的 production 風險**是 DNS-0x20 case-randomization resolver（Google Public DNS 自 2023-07 全球啟用、Unbound `use-caps-for-id`、dnsmasq ≥2.91rc4）會 drop case-mismatch response — ShadowDNS 暴露在 cache-poisoning hardening 失效與 SERVFAIL 中。

調查結果（見 deep-research 完整報告）：
- RFC 4343 §4.1 允許 lowercase output 但建議 case-preserving；現代 de-facto 是 case-preserving。
- 0x20 隨機化只**強制**要求 Question section bit-for-bit echo；Answer section / RDATA 的 case 是 implementation choice。
- 主流 server 都採「Question section echo query case + Answer section/RDATA 用 zone-file/config case」。
- miekg/dns 是 byte-transparent — case loss 完全來自 ShadowDNS 程式碼。

## Goals / Non-Goals

**Goals:**

- ShadowDNS response 的 Question section QNAME 與 query 完全一致（byte-for-byte echo），對 0x20 resolver 友善。
- Alias zone response 的 owner name 保留 query 端 case prefix + alias config 寫入的 backup case suffix。
- Alias zone response 的 RDATA name 保留 zone-file 端 case prefix + alias config 寫入的 backup case suffix（in-bailiwick 與 anywhere-match 兩種模式皆然）。
- 非 alias zone response 的 owner name / RDATA 保留 zone-file case。
- Wildcard 命中時的 owner case 保留 query case。
- Lookup 比對仍 case-insensitive（RFC 4343 強制要求）。

**Non-Goals:**

- **不**實作 DNS-0x20 case randomization（resolver 端 anti-spoofing 機制，不在 authoritative server scope）。
- **不**改 zone storage 索引設計（`zone.go AddRR` 用 lowercase key 是正確的）。
- **不**處理 CNAME chain flatten 不一致（Case C，已記錄為非 ShadowDNS bug）。
- **不**動 alias yaml schema（schema 已在 fix-alias-rdata-mid-label-rewrite 改過；本次只改 case 處理語義）。

## Decisions

### Decision: 採 single-source 原 case 儲存 + on-demand lowercase fold

**選擇**：alias config 與 zone storage 統一保留原始 case；提供 `LookupKey(name) string` helper 在 lookup / 比對處 on-demand 做 lowercase fold。

**Rationale**：
- 原 case 是「一份 source of truth」資料，比 dual-store（lowercase + original）省記憶體、避免一致性 bug。
- Lookup 是熱路徑但 `strings.ToLower` 對短字串成本可忽略；map 查 key 前 fold 一次比 dual-store 維護兩份索引簡單。
- 易於 reason about — 資料只有原 case，需要 lowercase 比對時才 fold。

**Alternatives considered**：
- A. Dual-store（每個 AliasGroup 攜帶 `MembersOriginal []string` + `MembersLower []string`）— 雙份記憶體、index 不一致風險。
- B. 全 lowercase 儲存 + 在 output 時依 query 重建 case — 不 work，因為 alias config 有 BIND zone 端寫好的 capital backup（如 `Example.com`），output 時無從還原。

### Decision: `Canonicalize` 重新定義為 case-preserving

**選擇**：`Canonicalize(name)` 只做 `strings.TrimSuffix(name, ".") + "."`（normalize trailing dot），**不**做 lowercase；新增 `LookupKey(name) string` 做 `strings.ToLower(strings.TrimSuffix(name, ".")) + "."` 給比對 / map key 用。

**Rationale**：保留 API 名稱（`Canonicalize`）但語義從「lowercased FQDN」改為「FQDN with trailing dot」。所有現有呼叫端必須 audit 並依使用情境改用 `Canonicalize`（output / storage）或 `LookupKey`（comparison）。

**Migration impact**：每個 `Canonicalize` 呼叫點都需檢視 — 改成 `LookupKey` 還是維持 `Canonicalize`。Audit checklist 詳見 tasks。

**Alternatives considered**：保留舊 `Canonicalize` 行為新增 `Original(name)` — 但會造成大量呼叫點不知該選哪個的混淆，且舊 API 名稱「Canonical」誤導性強（DNS canonical form per RFC 4034 是 lowercase，但這 server 的 internal canonical 應該是「case-preserving FQDN」更貼切）。

### Decision: `RewriteName` / `RewriteNameAnywhere` 改為 case-preserving output

**選擇**：簽章保持 `(n, root, backup string)` 不變，但語義變為：
- `root` 必須是 lowercase（lookup 用，呼叫端應用 `LookupKey`）。
- `backup` 必須是 alias config 寫入的原 case。
- `n` 為 input 的原 case（owner name 來自 query 或 zone；RDATA target 來自 zone storage）。
- Match 用 `strings.ToLower(n)` 跟 `root` 比；Output 用 n 原 case 的 prefix + `backup` 原 case suffix。

**Rationale**：
- 保留 hot-path API 形狀，不擴大 blast radius。
- prefix / suffix 切割點用 lowercase index 計算，但 slice 出來的字串用原 n（保留 case）。
- `RewriteRR` 的 owner / RDATA 改寫呼叫不變，case 自動傳遞。

**Implementation hint**：
```go
func RewriteName(n, root, backup string) string {
    if n == "" { return n }
    nLower := strings.ToLower(n)
    if nLower == root { return backup }
    suffix := "." + root
    if strings.HasSuffix(nLower, suffix) {
        return n[:len(n)-len(suffix)] + "." + backup
    }
    return n
}
```

`RewriteNameAnywhere` 同理 — match index 用 lowercase，output 拼接用原 n 的 byte slice。

**Alternatives considered**：拆成 `RewriteNameMatch(n, root) (idx int, ok bool)` + `RewriteNameApply(n, idx, backup)` 兩階段 API — 過度設計，呼叫端不需要這種分離。

### Decision: Handler 用 `req.Question[0].Name` 替代 lowercased qname 組裝 response

**選擇**：handler 保留 `qname := strings.ToLower(q.Name)` 用於 zone matching / map lookup，但所有觸及 response 組裝（owner name 寫入、wildcard rewrite）的呼叫改用 `q.Name`（原 case）。具體：
- `rewriteWildcardOwner(records, qname)` 改為 `rewriteWildcardOwner(records, q.Name)`。
- Alias resolve 路徑傳入 alias resolver 的 query 名稱用原 case，內部用 `LookupKey` fold。
- `replyWithAnswer` / `m.SetReply(req)` 確認 Question section 不被 mutate（miekg/dns 預設行為，只需驗證）。

**Rationale**：Question section echo 是 0x20 友善的硬要求；Answer section owner name 是 query 端 prefix + alias config 端 suffix 的組合，prefix 必須來自 query 原 case。

**Alternatives considered**：保留 `qname` 為 lowercased 但組 response 時用 `q.Name` — 已是這個方向；只是要逐處 audit 清楚 lowercased qname 不會洩漏進 response。

### Decision: 配置端不做 case validation；接受任何 case 並原樣儲存

**選擇**：alias yaml 接受 backup 名稱寫成任何 case，原樣儲存。lookup 比對用 `LookupKey` fold 後比較。Operator 寫 `Example.com` 跟寫 `example.com` 兩種不同 case 仍視為同一 backup（lookup 一致），但 RDATA output case 會跟 yaml 寫法一致。

**Rationale**：尊重 op 的 case 意圖（通常 op 會跟 BIND zone-file 內 case 一致以維持 consistency），ShadowDNS 不該強加 normalization。

**Operator guidance**：release notes 提示 ops 檢查 yaml 中 backup 名稱 case 是否與對應 BIND zone-file owner case 一致；不一致會導致 dnspyre 仍 flag。

## Risks / Trade-offs

- [Risk] `Canonicalize` 語義變更可能影響其他呼叫端 — 不只 alias，還有 zones / config / shadowdnscfg 等。
  → **Mitigation**：tasks 必須包含完整 `Canonicalize` callsite audit，逐個判斷該改 `LookupKey` 還是維持 `Canonicalize`。golangci-lint forbidigo rule 可暫時禁用 `strings.ToLower` 在某些 package（出於 code review 強化）。

- [Risk] case-preserving rewrite 在 hot path 的效能回退 — `RewriteName` 對每個 RDATA 呼叫，多一次 `strings.ToLower(n)`。
  → **Mitigation**：`strings.ToLower` 對短字串（<256 bytes）效率夠；若 benchmark 顯示 regression，可用 byte-level 比對（lowercase fold inline）取代 alloc 一個新字串。`RewriteNameAnywhere` 已是 allocation-free 設計，新版需保持。

- [Risk] op 的 yaml 既有 backup 名稱可能全 lowercase（之前 lowercase 跟 BIND zone-file 的 capital 不一致也沒事），改版後 dnspyre 仍會 flag — 因為現在 ShadowDNS 會 echo lowercase yaml 內容，跟 BIND 的 capital zone case 不對。
  → **Mitigation**：release notes 列舉 — op 應檢查 yaml 中 backup case 是否與對應 BIND zone case 一致。

- [Risk] mixed-case query 進來時，alias zone owner 的 prefix 用 query case + suffix 用 alias yaml case，可能拼出非預期 mix（如 query `wWw.eXaMpLe.cOm` → owner `wWw.Example.com.`）。
  → **Mitigation**：這是符合 BIND 行為的（BIND 也這樣），且 case-insensitive 比對下不影響語義；DNSSEC 在 RFC 4034 §6.2 簽章時用 canonical lowercase 故不受影響。記錄在 spec / changelog 即可。

- [Risk] 既有 unit test 大量斷言 lowercase 結果 — 全部要更新。
  → **Mitigation**：tasks 第一步先跑 `make test` 取得 baseline 失敗清單；分批調整斷言為 case-preserving expected output。

## Migration Plan

1. **Step 1**：`internal/dnsutil/dnsutil.go` 拆分 `Canonicalize` 與 `LookupKey`，先讓 `Canonicalize` 內部呼叫 `LookupKey`（行為不變）→ 跑 test 通過。
2. **Step 2**：把 `Canonicalize` 改為 case-preserving，立即會有大量 test 失敗 — 此 step 只做 dnsutil 的 API 變更，呼叫端先不動（但會編譯通過因為簽章未變）。
3. **Step 3**：逐個 audit 呼叫 `Canonicalize` 的點，比對與 lookup 路徑的改用 `LookupKey`（包括 alias config 與 zone-origin 處理），output / storage 路徑保持 `Canonicalize`。
4. **Step 4**：改 `RewriteName` / `RewriteNameAnywhere` 為 case-preserving；更新單元測試。
5. **Step 5**：handler 改用 `q.Name`（原 case）組 response；改 `rewriteWildcardOwner` 簽章；config / alias 結構保留原 case。
6. **Step 6**：integration test — mixed-case query 對 alias / non-alias / wildcard / 多種 RR type。
7. **Step 7**：op release notes + CHANGELOG。

無需 rollback 機制 — v0.x.x 階段，部署到 bench-ns2 後跑 dnspyre 檢驗，有問題直接前進修復。

## Open Questions

- 是否需要為「op 想保留舊 lowercase 行為」提供 feature flag？傾向**不**提供 — 新行為是更標準的，舊行為是 bug，不該保留 toggle 增加維護成本。release notes 提示即可。
- `LookupKey` 是否需要快取？目前不需要 — 每 query 一次 fold 成本極低。如後續 benchmark 顯示熱點，再加 thread-local cache。
