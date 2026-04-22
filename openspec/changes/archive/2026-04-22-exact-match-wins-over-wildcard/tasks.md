## 1. Tests first — root zone

- [x] 1.1 在 `internal/server/handler_ephemeral_test.go` 新增 failing test 覆蓋 spec scenario `Ephemeral TXT at exact qname takes precedence over zone wildcard TXT`：建一個 zone 內含 `*.example.com. TXT "wild-value"`（沿用 `buildRootZone` helper，需要先確認它能注入 wildcard TXT，不行的話在 test helper 層加一個 `buildRootZoneWithWildcard`），ephemeral store PUT `foo.example.com.` → `ephemeral-value`，`dig foo.example.com. TXT` 應只回 `ephemeral-value`、不含 `wild-value`。
- [x] 1.2 [P] 在 `internal/server/handler_ephemeral_test.go` 新增 failing test 覆蓋 scenario `Ephemeral TXT at exact qname takes precedence over zone wildcard CNAME`：zone 含 `*.example.com. CNAME target.other.com.`，ephemeral PUT `_acme-challenge.foo.example.com.` → `token`，查 TXT 應回 `token`、不含 synthesized CNAME。
- [x] 1.3 [P] 在 `internal/server/handler_ephemeral_test.go` 新增 regression test 覆蓋 `Zone wildcard still applies when ephemeral store has no exact match`：zone 含 `*.example.com. A 1.2.3.4`，ephemeral 空，查 `foo.example.com. A` 應回 `1.2.3.4`（確認 wildcard 仍正常運作）。
- [x] 1.4 [P] 在 `internal/server/handler_ephemeral_test.go` 新增 regression test 覆蓋 `Ephemeral TXT does not suppress wildcard for non-TXT query types`：zone 含 `*.example.com. A 1.2.3.4`，ephemeral PUT `foo.example.com. TXT "token"`，查 `foo.example.com. A` 應回 `1.2.3.4`（ephemeral TXT 不影響 A 查詢的 wildcard 路徑）。

## 2. Implementation — root zone dispatch order (spec: Match wildcard records per RFC 4592 when exact lookup fails)

- [x] 2.1 在 `internal/server/handler.go` 的 `handleRootQuery` 中，把 `lookupEphemeralTXT` 呼叫從 wildcard fallback 之後（目前約 line 214）搬到 wildcard fallback 之前（位於 zone CNAME fallback 之後、`rootZone.LookupWildcard` 之前），以實現 spec `Match wildcard records per RFC 4592 when exact lookup fails` 中把 ephemeral store 視為 exact lookup 的定義；移動後的順序：zone exact → zone CNAME → **ephemeral TXT** → zone wildcard → zone wildcard CNAME → negative。
- [x] 2.2 跑 `go test -race -count=1 ./internal/server/...`，確認 1.1–1.4 四個新測試由紅轉綠，且原本的 wildcard 測試（如 `test/integration/wildcard_test.go`、`internal/alias/override_test.go`）沒有 regression。

## 3. Tests first — backup zone

- [x] 3.1 在 `internal/server/handler_ephemeral_test.go` 新增 failing test 覆蓋 scenario `Ephemeral TXT at exact backup-zone qname takes precedence over backup-derived wildcard`：root zone `root.com.` 含 `*.root.com. CNAME target.other.com.`，backup zone `backup.com.`，ephemeral PUT `_acme-challenge.foo.backup.com.` → `backup-token`，查 `_acme-challenge.foo.backup.com. TXT` 應回 `backup-token`、不含 synthesized CNAME。沿用 `newRootBackupServerWithEphemeral` helper。

## 4. Implementation — backup zone dispatch order

- [x] 4.1 在 `internal/server/handler.go` 的 `handleBackupQuery` 中，把 `lookupEphemeralTXT` 從 `alias.Resolve` 回 empty 後、`negativeReply` 之前提前到 `alias.Resolve` 回 empty 後但在 wildcard 還沒 synthesize 之前。注意：`alias.Resolve` 內部已處理 backup→root rewrite 和 wildcard，因此搬動的 ephemeral lookup 要用 **backup-namespace qname**（即 handler 收到的原始 qname），因為 API 使用者是對 backup 名稱做 PUT。如果 `alias.Resolve` 內部無法把「zone exact」與「zone wildcard」兩個成功路徑拆開回傳不同 signal，需要在 `internal/alias/` 新增 `ResolveExact` 和 `ResolveWildcard` 拆成兩階段，再把 ephemeral lookup 夾在中間；若 `alias.Resolve` 本來就只在 wildcard 命中時回傳、exact match 另有路徑，則直接在 handler 呼叫前做 ephemeral 檢查即可。
- [x] 4.2 跑 `go test -race -count=1 ./internal/server/... ./internal/alias/... ./cmd/shadowdns/...`，確認 3.1 新測試轉綠，且 backup zone 相關 regression 測試全綠。

## 5. Full verification

- [x] 5.1 `make test` 全綠（含 race detector）。
- [x] 5.2 `make lint` 無新警告。
- [x] 5.3 `make smoke` dry-run 通過。
- [x] 5.4 手動 ns2 驗證：把本 change 用 release-shadowdns 的 local-change mode 部署到 bench-ns2，對 `_acme-challenge.<某個有 wildcard 的 zone>` 先 PUT 再 `dig @127.0.0.1`，確認回的是 ephemeral TXT 值、不是 wildcard 的 synthesized CNAME。
