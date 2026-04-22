## Context

ShadowDNS 的 ephemeral record store 在 `v0.x.x` 實驗階段由 `internal/server/handler.go` 的 `lookupEphemeralTXT` 於 zone 查詢 miss 後才被諮詢。對於典型的 ACME DNS-01 delegation 場景（`_acme-challenge.*` CNAME 至外部 acme-dns），zone 上該 qname 有精確 CNAME，`dig TXT` 查詢會被步驟 2 的 RFC 1034 §3.6.2 CNAME fallback 短路，使 ephemeral API 在此 qname 上的 TXT 寫入對 DNS 平面完全不可見。

現行 `dns-server` 規範（`openspec/specs/dns-server/spec.md` 的 Listen for DNS queries on UDP and TCP port 53 Requirement 段落）明文記載「Zone file records SHALL take precedence over ephemeral records」，是此限制的規範依據。本變更需在該規範中精細化此規則：僅當 zone 對同一 `(qname, qtype)` 有記錄時才由 zone 優先；對 `qtype == TXT` 且 zone 上僅有 CNAME 的情況，改由 ephemeral store 優先。

涉及兩條查詢路徑：

- **Root zone 路徑**：`internal/server/handler.go:handleRootQuery`（line 193-206 區段）
- **Backup zone 路徑**：`internal/server/handler.go:handleBackupQuery`（line 256-267）與其呼叫的 `internal/alias/override.go:ResolveExact`（line 54 的 CNAME fallback）

## Goals / Non-Goals

**Goals:**

- 讓 `dig TXT <qname>` 在 zone 有精確 CNAME 且 ephemeral store 有對應 TXT 時，回傳 ephemeral TXT。
- 保留 `dig CNAME <qname>` 的回應不變，仍回傳 zone 上配置的 CNAME。
- 保留 `dig A/AAAA/MX/...` 等其他 qtype 的行為不變，仍依 RFC 1034 §3.6.2 執行 CNAME fallback。
- 保留 ephemeral store 無對應 entry 或已過期時的 CNAME fallback 行為。
- 在 root zone 與 backup zone 兩條路徑上行為一致。

**Non-Goals:**

- 不放寬 `internal/zone` 的 CNAME 共存檢查；zone file 仍視同名 CNAME + 其他 type 為 malformed。
- 不改變 wildcard CNAME 與 ephemeral TXT 的互動（既有 `TestEphemeral_ExactBeatsWildcardCNAME` 已覆蓋）。
- 不改變 `ephemeral_api` 的寫入路徑、key 規範、TTL 處理、過期邏輯。
- 不提供任何旗標讓使用者關閉此新行為；v0.x.x 期間直接採用新語意。

## Decisions

### Reorder ephemeral TXT lookup before exact CNAME fallback on root zone path

在 `handleRootQuery` 中將現有步驟 2（CNAME fallback）與步驟 3（ephemeral TXT）對調。調整後順序：

1. `rootZone.Lookup(qname, qtype)` — 精確 `(qname, qtype)` 匹配
2. `lookupEphemeralTXT(qname, qtype)` — 僅 TXT qtype 生效
3. CNAME fallback（RFC 1034 §3.6.2）
4. Wildcard fallback（既有行為）

**Rationale**：`lookupEphemeralTXT` 的 qtype guard（`handler.go:283`）保證非 TXT qtype 為 no-op，提前此呼叫對 `dig CNAME/A/AAAA` 等路徑無副作用。唯一行為差異：zone 在 qname 上有精確 CNAME 且 store 有 TXT entry 時，TXT qtype 改由 ephemeral 回應。

**Alternatives Considered**：

- *在 CNAME fallback 內部額外檢查 ephemeral*：等價但耦合 CNAME fallback 與 ephemeral 邏輯，增加後續維護成本。拒絕。
- *新增 per-qname 「ephemeral override」旗標*：增加設定面積與 zone-level 狀態；v0.x.x 實驗階段不需要。拒絕。
- *在 zone parser 允許 CNAME + TXT 共存並走 RRSet 合併*：違反 RFC 1034 §3.6.2，且破壞 zone file 語意一致性。拒絕。

### Apply equivalent reorder on backup zone path

在 `handleBackupQuery` 中將 ephemeral TXT 檢查提前到 `alias.ResolveExact` 之前；由於 `ResolveExact` 內部亦有 CNAME fallback（`internal/alias/override.go:54`），需同時確認 `ResolveExact` 或其呼叫端在 ephemeral TXT 命中時不進入 CNAME 跟隨分支。

**實作選項**：

- (A) 於 handler 層把 ephemeral TXT 移到 `ResolveExact` 之前
- (B) 於 `alias/override.go` 的 `finalizeBackupRRs` 或 `ResolveExact` 加入「僅當 qtype == TXT 且有 live ephemeral 則略過 CNAME fallback」

採用 **(A)**：把決策收斂在 handler 層，避免 `internal/alias` 反向依賴 ephemeral store。`alias` 套件維持純 zone/alias 解析職責。

**Rationale**：維持 root 與 backup 兩條路徑在 handler 層的結構一致性；`alias` 套件不需要感知 ephemeral store。

### Intentional RFC 1034 §3.6.2 deviation is scoped to TXT qtype

明文記錄此變更在 TXT qtype 上偏離「CNAME 獨佔 owner name」的嚴格語意，並將此偏離限縮在 ephemeral overlay 範疇：

- 僅 TXT qtype 受影響
- 僅當 ephemeral store 有 live entry 才偏離；無 entry 時完全回退標準行為
- 不影響 DNSSEC 語意（ephemeral 本身已不參與 DNSSEC 簽章路徑）
- 在 `specs/dns-server/spec.md` 與 `specs/ephemeral-api/spec.md` 明文宣告此語意

**Rationale**：ephemeral API 的核心價值是「不需改 zone 即可短期寫入 TXT」，標準 RFC 行為使此價值在已存在 CNAME delegation 的 qname 上完全無法實現；而 ACME DNS-01 delegation 正是 ephemeral TXT API 的主要 use case。在 v0.x.x 實驗階段對此做出明確設計選擇，優於引入設定面積或延後解決。

## Risks / Trade-offs

- [偏離 RFC 1034 §3.6.2 造成 DNS 一致性觀感問題] → 在規範文件與 code comment 明確標註為刻意設計；僅限 TXT qtype；僅限 live ephemeral entry 時生效；其餘 qtype 行為不變。
- [使用者忘記清除 ephemeral TXT 導致 `dig TXT` 與 `dig CNAME` 回應分歧] → 透過 spec 中的 scenarios 與 docs/ephemeral-api.md 明確紀錄此覆蓋行為；v0.x.x 實驗階段可接受。
- [backup zone 路徑若未同步調整會造成 root 與 backup 行為不一致] → 由 `tasks.md` 明列兩條路徑各自的單元/整合測試覆蓋，並在 integration test 中同時覆蓋 alias backup + CNAME + ephemeral TXT 場景。
- [既有的「Zone file record takes precedence over ephemeral record」scenario 可能被誤解為已作廢] → 該 scenario 仍然成立（zone 有精確 TXT 時），僅須補充說明新增的 CNAME + ephemeral TXT 情境為顯式例外，並保留原 scenario 作為「精確同 type 優先」的語意基準。
