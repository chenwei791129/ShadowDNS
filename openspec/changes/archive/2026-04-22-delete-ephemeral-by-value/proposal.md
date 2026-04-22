## Why

目前 `DELETE /v1/txt/{fqdn}` 會一次刪除該 FQDN 底下所有 ephemeral TXT 項目。ACME DNS-01 驗證常在同一個 `_acme-challenge.<domain>` 掛兩個平行 token（apex + wildcard），單一驗證完成後只想收掉自己那一筆、保留另一個仍在驗證中的 token，目前只能 re-PUT 剩下的那筆來補回，競態空窗期會造成另一個正在驗證中的 challenge 短暫消失而失敗。

同時，PUT 目前沒有對 `value` 做長度驗證，可以寫入超過 RFC 1035 character-string 上限（255 bytes）的值，這會在 DNS 回應階段才被 miekg/dns 拒絕或截斷，錯誤訊息對 API 使用者不友善。

## What Changes

- `DELETE /v1/txt/{fqdn}` 新增可選的 `?value=<value>` query 參數；帶入時只刪除該 FQDN 底下 value 完全相符的那一筆 entry。
- 無 `?value=` 時沿用現行語意：一次清除該 FQDN 底下所有 ephemeral entries（向後相容）。
- 加入 `value` 長度驗證（UTF-8 bytes ≤ 255，對應 RFC 1035 TXT character-string 上限），同時套用到 `PUT /v1/txt/{fqdn}` 與 `DELETE /v1/txt/{fqdn}?value=...`；違反時回傳 HTTP 400。
- 空字串的 `?value=` 視為請求格式錯誤，回傳 HTTP 400（避免和「不帶 value」的 wipe-all 語意混淆）。
- `?value=` 帶入但無 matching entry 時回傳 HTTP 200（idempotent delete，和現行 DELETE 非存在 FQDN 的行為一致）。
- `store.DeleteValue(fqdn, value string) bool` 新增；回傳值為是否有實際刪除一筆 entry。當 FQDN 下最後一筆 entry 被移除時，整個 FQDN key 會一併從 map 移除，避免殘留空 slice。
- TXT value 視為 opaque，case-sensitive，不做任何 normalization（和現行 PUT 比對邏輯一致）。
- `docs/ephemeral-api.md` 新增 per-value delete 的 curl 範例與 value 長度限制說明。

## Non-Goals

- 不支援用 pattern / regex / prefix 匹配多筆 value 刪除；`?value=` 只接受完整字串比對。
- 不改 DELETE 的底層語意去支援 request body——RFC 9110 明文 discourage DELETE 帶 body，使用 query string 更通用且 cache/proxy 友善。
- 不引入 If-Match 或其他 concurrency control；ephemeral store 不需要樂觀鎖。
- 不對 PUT 的 TTL 欄位或 FQDN 長度另外加驗證；本 change 只處理 `value`。

## Capabilities

### New Capabilities

（none）

### Modified Capabilities

- `ephemeral-api`: DELETE endpoint 新增 `?value=` selector；PUT 與 DELETE 都加 value 長度驗證。
- `ephemeral-record-store`: 新增 `DeleteValue(fqdn, value) bool` 方法，只刪除一筆符合 value 的 entry。

## Impact

- Affected specs: `ephemeral-api`, `ephemeral-record-store`
- Affected code:
  - `internal/ephemeral/store.go` — 新增 `DeleteValue` 方法
  - `internal/ephemeral/store_test.go` — 新增 unit test 覆蓋 DeleteValue 各分支
  - `internal/api/server.go` — `handleDelete` 讀取 `?value=`、分派到 `DeleteValue` 或 `Delete`；`handlePut` 與 `handleDelete` 共用 value 長度驗證輔助函式
  - `internal/api/server_test.go` — 新增 per-value delete 與 value 長度驗證的 HTTP-layer 測試
  - `cmd/shadowdns/main_ephemeral_test.go` — 新增 e2e 測試驗證 per-value DELETE 只移除指定 entry，另一筆仍可被 DNS 查詢到
  - `docs/ephemeral-api.md` — 新增 per-value delete 章節與 value 長度限制說明
