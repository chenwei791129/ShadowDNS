## Why

ShadowDNS 的 ephemeral TXT response TTL 原本固定為 30 秒，但在連續申請 cert 的情境下，ACME validator（或其上游 resolver）會在第二輪 challenge 仍快取住前一輪的 TXT 值，導致 DNS-01 驗證失敗。commit `9fb95cf90ab3443be5ff329dc847a5647c77f1c8` 已將 `internal/server/handler.go` 的 `EphemeralResponseTTL` 常數從 30 改為 0，並在 bench-ns2 上驗證可消除該失敗模式。本 change 將已驗證的程式碼行為回填到 spec，使規格與實作保持一致。

## What Changes

- `ephemeral-api` spec：scenario "ACME delegation qname with ephemeral TXT returns the ephemeral value to TXT queries" 中的 **THEN** 子句，將 `RR TTL 30` 改為 `RR TTL 0`，使其反映新的固定回應 TTL。
- `dns-server` spec：Example "48 TXT RRs at `_acme-challenge.example.com.` with 43-byte values" 中提到的 `TTL 30` 改為 `TTL 0`，以保持與 ephemeral 實際行為一致（此 Example 僅作為壓縮大小驗證的輸入範例，TTL 數值不影響 compression 結論）。
- 不涉及任何程式碼變更（程式碼已於 commit `9fb95cf` 完成並驗證）。

## Non-Goals

- 不變更 `EphemeralResponseTTL` 的型別或常數機制（仍為固定常數，僅數值由 30 改為 0）。
- 不調整 store-side TTL clamp 範圍 `[1, 3600]`（PUT body 的 `ttl` 欄位語意不變）。
- 不改寫 `ephemeral-record-store` spec，該 spec 全程描述 store-side lifespan，與 DNS 回包 TTL 無關。
- 不重新討論 TTL=0 是否為最佳值（已由 commit `9fb95cf` 在實機上驗證有效）。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ephemeral-api`: 修改 "Ephemeral TXT entries override exact CNAME at the same qname for TXT queries" requirement 底下的 ACME delegation scenario，將回應 TTL 由 30 改為 0。
- `dns-server`: 修改 "Successful answer responses SHALL use DNS name compression" requirement 底下的 48 TXT RRs Example，將 TTL 由 30 改為 0 以與 ephemeral 實際行為對齊。

## Impact

- Affected specs: `ephemeral-api`, `dns-server`
- Affected code:
  - Modified: （無新增程式碼變更；commit `9fb95cf90ab3443be5ff329dc847a5647c77f1c8` 已將 internal/server/handler.go 的 EphemeralResponseTTL 從 30 改為 0）
  - New: （無）
  - Removed: （無）
- 行為影響：DNS resolver/ACME validator 會立即拋棄前一輪 TXT 回應而不會快取 30 秒，連續 ACME DNS-01 challenge 不再因為快取舊值而失敗。下游 resolver 會將 ephemeral TXT 視為「不要快取」（RFC 1035 §3.2.1，TTL=0 代表不應該被快取）。
