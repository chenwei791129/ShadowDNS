## Problem

當 zone file 有 wildcard 記錄（例如 `*.example.com. CNAME source.example.com.` 或 `*.example.com. A 1.2.3.4`），而 ephemeral store 在同一個 qname 下有 TXT entry（例如透過 ACME DNS-01 PUT 進去的 `_acme-challenge.foo.example.com.`），目前的 DNS 查詢結果是 **wildcard 贏**：

- `TestEphemeralTxtApi_PerValueDeleteEndToEnd` 這類 in-process 測試能過，是因為測試 zone file 沒有 wildcard。
- 在真實 ns2 上，`example-zone.com` zone 有 catch-all wildcard（`*.example-zone.com CNAME service-host.example-zone.com.`），任何 undefined label 的查詢（即便 ephemeral store 已經 PUT 了 exact-match 的 TXT）都會被 wildcard 吞掉，ACME DNS-01 驗證就無法用 ephemeral API 完成。

## Root Cause

`internal/server/handler.go` 的查詢 dispatch 順序目前是：

1. Zone exact match（`rootZone.Lookup`）
2. Zone CNAME fallback
3. **Zone wildcard fallback**（`LookupWildcard`）
4. **Zone wildcard CNAME fallback**
5. Ephemeral TXT lookup
6. Negative reply

Ephemeral TXT 排在 wildcard 之後，因此 wildcard 總是先 match 成功而提前 return，ephemeral 永遠不會被查到。

此行為與 BIND 不一致——BIND 的 wildcard 匹配規則（RFC 4592 §2.2.1）是「wildcard 只在 qname 下**完全沒有**記錄時才觸發」，而 ShadowDNS 的 ephemeral store 其實就是 qname 下的 TXT 記錄，應該要像 zone 內的 exact record 一樣阻斷 wildcard 匹配。

`dns-server` spec 的 "Match wildcard records per RFC 4592 when exact lookup fails" 目前只把「exact lookup」定義成 zone file 的 `Lookup`，沒有把 ephemeral store 納入 exact-match 的範圍，是 spec 與 code 同步的落差。

## Proposed Solution

調整 `internal/server/handler.go` 的查詢 dispatch 順序，把 ephemeral TXT lookup 往前移到 zone wildcard 之前：

1. Zone exact match
2. Zone CNAME fallback
3. **Ephemeral TXT lookup（exact qname match）** ← 往前移
4. Zone wildcard fallback
5. Zone wildcard CNAME fallback
6. Negative reply

同樣的順序調整要套用到 `handleRootQuery` 與 `handleBackupQuery` 兩個入口；`handleBackupQuery` 走的是 `alias.Resolve`（它內部已處理 backup→root rewrite + wildcard），需要在 `alias.Resolve` 回 empty 後、fall through 到 ephemeral 之前，確認 ephemeral lookup 用的是 backup-namespace 的 qname（對應客戶端實際送的名字），因為 ACME client 會對 backup zone 的名稱做 PUT。

同步更新 `dns-server` spec 的 "Match wildcard records per RFC 4592 when exact lookup fails" 需求，明確把 ephemeral TXT store 納入「exact lookup」範圍；新增兩個 scenario 覆蓋「zone wildcard + ephemeral exact → ephemeral 贏」的行為。

## Non-Goals

- 不改變「zone file exact record 優先於 ephemeral」的既有語意（archived `ephemeral-api` spec 的 "zone file takes precedence" 保持有效）。排序原則是：zone exact > ephemeral exact > zone wildcard > NXDOMAIN。
- 不擴充 ephemeral store 支援 TXT 以外的 record type。Ephemeral 仍然只服務 `TypeTXT` 查詢，其他 type 的查詢走原本的 zone + wildcard 路徑。
- 不修改 `rootZone.HasWildcard` 在 `negativeReply` 裡對 NXDOMAIN vs NODATA 的判斷邏輯（那是 rcode 邊界，與查詢優先序是分開的議題）。
- 不提供「關閉此行為」的 feature flag；bug 修正後就是預期行為，不保留舊順序當 opt-in。

## Success Criteria

- 在一個同時有 `*.<root>` wildcard（CNAME 或 TXT 皆可）與 ephemeral TXT entry（exact qname）的 zone 下，對該 exact qname 做 TXT 查詢時，DNS 回應的 answer section 只包含 ephemeral 的 TXT 值，不包含 wildcard 的 synthesized CNAME/TXT。
- 同樣行為在 backup（alias）zone 下也成立：backup zone 的 wildcard 被 exact ephemeral TXT 蓋過。
- 當 ephemeral store 沒有 exact match 時，wildcard fallback 行為完全不變（現有 `test/integration/wildcard_test.go` 等測試保持綠）。
- 在 ns2 真實環境上，對 `_shadowdns-ephemeral-test.example-zone.com` 先 PUT 再 dig，能看到 PUT 的 TXT 值而不是 wildcard 指向的 `service-host.example-zone.com.`。

## Impact

- Affected specs: `dns-server` （MODIFIED: "Match wildcard records per RFC 4592 when exact lookup fails"）
- Affected code:
  - `internal/server/handler.go` — 在 `handleRootQuery` 與 `handleBackupQuery` 中把 `lookupEphemeralTXT` 呼叫從 wildcard 之後移到 wildcard 之前
  - `internal/server/handler_ephemeral_test.go` — 新增 wildcard + ephemeral 互動的測試 scenario（root zone + backup zone 兩版）
