# Tasks

## 1. Spec backfill — ephemeral-api: Ephemeral TXT entries override exact CNAME at the same qname for TXT queries

- [x] 1.1 在 openspec/specs/ephemeral-api/spec.md 中定位 Requirement "Ephemeral TXT entries override exact CNAME at the same qname for TXT queries"（約第 536 行），將其底下 scenario "ACME delegation qname with ephemeral TXT returns the ephemeral value to TXT queries" 的 **THEN** 子句中 `RR TTL 30` 改為 `RR TTL 0`，並在同一個 Requirement 底下新增 scenario "Ephemeral TXT response carries TTL 0 to suppress downstream caching" 與其 SBE example，內容對齊本 change 的 specs/ephemeral-api/spec.md delta。

## 2. Spec backfill — dns-server: Successful answer responses SHALL use DNS name compression

- [x] 2.1 [P] 在 openspec/specs/dns-server/spec.md 中定位 Requirement "Successful answer responses SHALL use DNS name compression"（約第 46 行），將其底下 Example "48 TXT RRs at `_acme-challenge.example.com.` with 43-byte values" 的 **GIVEN** 子句裡 `TTL 30` 改為 `TTL 0`。同檔案第 1469 行（如果存在相同 Example 的重複出現）以同樣方式更新 `TTL 30` → `TTL 0`，使主 spec 與本 change 的 dns-server delta 一致。

## 3. Trace block 維護

- [x] 3.1 確認 openspec/specs/ephemeral-api/spec.md 與 openspec/specs/dns-server/spec.md 中既有的 `<!-- @trace source: ephemeral-fixed-response-ttl ... -->` 區塊保留不動；若需更新 `updated:` 日期，使其指向本 change 的歸檔日期（archive 階段可一併處理）。本 change 不新增或重寫 @trace 區塊。

## 4. 驗證

- [x] 4.1 在 repo 根目錄執行 `spectra validate ephemeral-response-ttl-zero` 並確認回傳 0 errors。
- [x] 4.2 在 repo 根目錄執行 `grep -n "TTL 30\|RR TTL 30" openspec/specs/ephemeral-api/spec.md openspec/specs/dns-server/spec.md` 並確認沒有殘留的 `TTL 30` / `RR TTL 30` 字串（excluding 任何純文字 documentation 或 PUT body store-side ttl 範例如 `"ttl": 30`，那些不在本 change 範圍內）。
- [x] 4.3 與 commit `9fb95cf90ab3443be5ff329dc847a5647c77f1c8` 對照：執行 `git show 9fb95cf90ab3443be5ff329dc847a5647c77f1c8 -- internal/server/handler.go` 並確認 `EphemeralResponseTTL uint32 = 0` 已存在於 main 分支的 internal/server/handler.go，spec 文字與實作一致。
