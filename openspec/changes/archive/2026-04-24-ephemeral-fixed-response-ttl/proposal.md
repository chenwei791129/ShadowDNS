## Why

目前 ephemeral TXT API 的 `ttl` 欄位同時扮演兩個角色：記錄在 Store 中的存活時間、以及寫進 DNS 回應封包的 TTL 欄位。這導致 resolver 每次查詢時 TTL 會隨著秒數遞減（`dig` watch 觀察到 3105 → 3104 → 3100...），而 ACME validator 等呼叫者的預期是：API 的 `ttl` 只控制 record 在 Store 中的存活時間，DNS 回應 TTL 應為固定短值，讓下游 resolver 有穩定、可預期的快取行為。

## What Changes

- ephemeral TXT API 的 `ttl` 欄位語意收斂：只用來控制 record 在 Store 中的存活時間（expiry）。
- DNS 回應中 ephemeral TXT 紀錄的 TTL 欄位固定為 **30 秒**，不再隨剩餘存活秒數遞減。
- Store 的 `Lookup` 回傳值移除 remaining TTL 計算（`Record.TTL` 欄位刪除），只回傳 `Value`；DNS handler 組裝 `dns.TXT` 時寫入固定常數 `EphemeralResponseTTL = 30`。
- 過期邊界不變：record expiry 過後即消失，查詢回傳 NODATA（與現行一致）。
- API PUT 回應仍回傳 clamped 後的 API `ttl`（存活時間），對外契約不變。

## Non-Goals

- 不提供「回應 TTL 可設定」的 config／API flag。30 秒是固定常數。如未來有需求再另起 change。
- 不改變 API 的 TTL clamp 範圍 `[1, 3600]`。
- 不改變 Store 的 expiry 語意、idempotent refresh 行為、或多值支援。
- 不影響非 ephemeral（zone-loaded）TXT record 的 TTL 處理。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ephemeral-record-store`: `Record` 不再帶 remaining TTL；`Lookup` 語意改為只回傳 value 列表（與 expiry 過濾），不再輸出動態 TTL。
- `dns-server`: ephemeral TXT 回應的 TTL 欄位由「remaining 秒數」改為固定 30 秒常數。
- `ephemeral-api`: PUT `ttl` 欄位語意澄清為僅控制 Store 存活時間；回應 body 中的 `ttl` 欄位為 clamped 後的存活時間值（不變），但 scenario 中引用 DNS 回應 TTL 的部分需同步更新為固定 30。

## Impact

- Affected specs: `ephemeral-record-store`, `dns-server`, `ephemeral-api`
- Affected code:
  - Modified:
    - internal/ephemeral/store.go
    - internal/ephemeral/store_test.go
    - internal/server/handler.go
    - internal/server/handler_ephemeral_test.go
    - test/integration/ephemeral_overrides_cname_test.go
    - test/integration/cname_following_test.go
  - New: (none)
  - Removed: (none)
