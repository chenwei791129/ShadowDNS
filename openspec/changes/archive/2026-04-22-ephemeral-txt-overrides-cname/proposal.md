## Problem

當 zone 在某個 qname 上精確配置 CNAME 記錄（例如為 ACME DNS-01 delegation 常見的 `_acme-challenge.foo.example.com. CNAME acme-dns.other.com.`），即使透過 ephemeral API 為同一 qname PUT 了 TXT 記錄，`dig TXT` 查詢仍會取得 CNAME 鏈而非 ephemeral TXT，使 ephemeral API 的 TXT 無法生效。

## Root Cause

`internal/server/handler.go` 的查詢分派順序為：

1. `rootZone.Lookup(qname, qtype)` — 精確 type match（TXT 在 zone 無 TXT 記錄時 miss）
2. CNAME fallback（RFC 1034 §3.6.2）— TXT 查詢遇到精確 CNAME 時在此 return
3. `lookupEphemeralTXT` — 被前一步短路，永遠不會觸及
4. Wildcard fallback

Root zone 路徑（`handler.go:193-206`）與 backup zone 路徑（`handler.go:256-267` 及 `internal/alias/override.go:54` 的 `ResolveExact` 內部 CNAME fallback）都有同樣的短路問題。

## Proposed Solution

將 `lookupEphemeralTXT` 提前到精確 CNAME fallback 之前執行，僅 `qtype == TypeTXT` 時生效（`lookupEphemeralTXT` 內建 qtype guard，其他 qtype 為 no-op，不影響現有行為）。

Root zone 查詢順序調整為：

1. `rootZone.Lookup(qname, qtype)` — 精確 type match（CNAME 查詢在此回傳 CNAME）
2. `lookupEphemeralTXT` — 僅 TXT qtype 生效；命中則回傳 ephemeral TXT
3. CNAME fallback（RFC 1034 §3.6.2）— 未命中 ephemeral 才走 CNAME 鏈
4. Wildcard fallback

Backup zone 路徑採用對應調整：在 `alias.ResolveExact` 之前（或在其內部 CNAME fallback 之前）插入 ephemeral TXT 檢查。

此變更刻意偏離 RFC 1034 §3.6.2「CNAME 在 owner name 上獨佔」的嚴格語意，屬於 ShadowDNS 的 ephemeral overlay 設計決策，僅影響 TXT qtype 的回應路徑。

## Non-Goals

- 不放寬 zone parser 的 CNAME 共存檢查；zone file 中 CNAME 與其他類型於同一 owner name 共存仍視為 malformed。
- 不改變 `dig CNAME` / `dig A` / `dig AAAA` 等其他 qtype 的查詢行為。
- 不處理 wildcard CNAME 與 ephemeral TXT 的互動（已由 `TestEphemeral_ExactBeatsWildcardCNAME` 覆蓋且行為符合預期）。
- 不處理「zone 上同時配置精確 TXT 與 CNAME」的情境；zone 若有精確 TXT，步驟 1 即命中，與 ephemeral 無關。

## Success Criteria

- `dig +short TXT _acme-challenge.foo.example.com @<shadowdns>` 當 zone 上有精確 `_acme-challenge.foo.example.com. CNAME ...` 且 ephemeral store 有對應 TXT entry 時，SHALL 回傳 ephemeral TXT 值，不回傳 CNAME 鏈。
- `dig +short CNAME _acme-challenge.foo.example.com @<shadowdns>` 在上述同一 setup 下，SHALL 回傳 zone 上配置的 CNAME target，行為不變。
- `dig +short TXT _acme-challenge.foo.example.com @<shadowdns>` 當 ephemeral store 無對應 entry（或已過期）時，SHALL 回退為 CNAME fallback 並回傳 CNAME 鏈下 TXT 查詢結果，與變更前一致。
- `dig +short A foo.example.com @<shadowdns>` 對帶 CNAME 的 qname 仍依 RFC 1034 §3.6.2 回傳 CNAME + 跟隨結果，不受影響。
- 既有的 `TestEphemeral_ExactBeatsWildcardTXT`、`TestEphemeral_ExactBeatsWildcardCNAME`、`TestEphemeral_WildcardStillAppliesWithoutExactMatch` 持續通過。
- Backup zone 路徑下的相同行為以 integration 測試覆蓋（alias backup 場景 + exact CNAME + ephemeral TXT）。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `dns-server`: 調整 ephemeral record store 查詢時機，於 TXT qtype 下優先於精確 CNAME fallback；更新「Zone file records SHALL take precedence over ephemeral records」相關語意與 scenarios。
- `ephemeral-api`: 補充 TXT ephemeral 在同 qname 存在精確 CNAME 時的回應語意，明確宣告 ephemeral TXT 覆蓋行為。

## Impact

- Affected specs: `dns-server`, `ephemeral-api`
- Affected code:
  - Modified: internal/server/handler.go
  - Modified: internal/alias/override.go
  - Modified: internal/server/handler_ephemeral_test.go
  - Modified: test/integration/cname_following_test.go
  - New: test/integration/ephemeral_overrides_cname_test.go
  - Removed: (none)
