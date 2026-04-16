## Why

ShadowDNS 的 zone lookup 使用 exact map key 匹配，不支援 DNS wildcard 紀錄（RFC 4592）。當 zone file 包含 `*.example.com` 等 wildcard 紀錄時，查詢 `foo.example.com` 無法命中 wildcard，回傳 NXDOMAIN，而 BIND 能正確匹配並回傳 wildcard 合成的 answer。此 bug 導致約 16% 的查詢不一致。

## What Changes

- 修改 zone lookup 邏輯：當 exact match 失敗時，按 RFC 4592 規則逐級剝離 label 尋找最近的 wildcard 紀錄（closest encloser 演算法）
- Wildcard 合成的回應中，owner name 必須使用原始 qname（而非 `*` label），per RFC 4592 §2.2
- 此行為適用於 root zone 查詢與 backup zone 查詢
- 當 wildcard match 命中且 qtype 不匹配但存在 CNAME 時，CNAME synthesis 行為須與 `cname-synthesis` change 銜接（wildcard CNAME 是常見用例）

## Non-Goals

- **NSEC/NSEC3 wildcard proof**：DNSSEC 的 wildcard 否定證明不在範圍內
- **Wildcard 在 zone transfer (AXFR) 中的特殊處理**：AXFR 傳輸 raw zone data，wildcard 紀錄已經以 `*` label 形式存在，不需要合成
- **Empty non-terminal (ENT) 偵測最佳化**：ENT 是 wildcard 匹配的 blocker（RFC 4592 §2.2.1），本次以 zone.Records map 中是否存在 key 來判斷 ENT，不建預先計算的 ENT index

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `dns-server`: 新增 requirement — 當 exact match 失敗時，server 必須按 RFC 4592 的 closest encloser 規則匹配 wildcard 紀錄，並以原始 qname 作為合成回應的 owner name
- `zone-parser`: 新增 requirement — parser 必須正確解析 wildcard owner name（`*` label），並以 `*.origin.` 形式儲存在 Records map 中（驗證現有實作已正確處理）

## Impact

- 受影響程式碼：`internal/zone/zone.go`（新增 wildcard lookup 邏輯）、`internal/server/handler.go`（`handleRootQuery`、`handleBackupQuery` 使用 wildcard lookup）、`internal/alias/override.go`（`Resolve` 使用 wildcard lookup）
- 受影響 specs：`dns-server`（新增 wildcard matching requirement 與 scenarios）、`zone-parser`（新增 wildcard parsing verification requirement）
