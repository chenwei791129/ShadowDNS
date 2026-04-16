## Why

ShadowDNS 在收到 qtype ≠ CNAME 的查詢時（例如 A、AAAA、MX），即使該 name 下存在 CNAME 紀錄，也只會回傳空答案（NODATA）。根據 RFC 1034 §3.6.2，authoritative server 遇到 CNAME 時**必須**回傳該 CNAME，不論 client 請求的 qtype 為何。此 bug 導致約 84% 的查詢不一致（與 BIND 對比），影響所有使用 CNAME 的 zone。

## What Changes

- 修改 DNS 查詢處理邏輯：當 exact match 找到 name 但沒有 requested qtype 的紀錄時，檢查該 name 是否存在 CNAME 紀錄，若有則回傳 CNAME
- 此行為適用於 root zone 查詢與 backup zone 查詢兩條路徑
- CNAME 回應中的 owner name 必須使用 client 查詢的原始 qname（對 backup zone 而言，這是 alias rewrite 前的名稱）

## Non-Goals

- **CNAME chaining / following**：本次只回傳單一層 CNAME 紀錄。CNAME target 的進一步解析（chasing）不在範圍內——ShadowDNS 是 authoritative server，不做遞迴
- **DNAME 支援**：RFC 6672 DNAME 紀錄的合成不在本次範圍
- **Wildcard 支援**：wildcard matching（RFC 4592）是獨立的功能缺失，將以另一個 change 處理

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `dns-server`: 新增 requirement — 當查詢 name 存在 CNAME 紀錄且 qtype ≠ CNAME 時，server 必須在 ANSWER section 回傳 CNAME 紀錄（RFC 1034 §3.6.2），適用於 root zone 與 backup (alias) zone

## Impact

- 受影響程式碼：`internal/zone/zone.go`（`Lookup` 方法或新增 CNAME 偵測方法）、`internal/server/handler.go`（`handleRootQuery`、`handleBackupQuery`）
- 受影響 specs：`dns-server`（新增 CNAME synthesis requirement 與 scenarios）
