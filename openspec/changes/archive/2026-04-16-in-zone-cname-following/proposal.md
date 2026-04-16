## Why

ShadowDNS 在遇到 CNAME 時只回傳 CNAME 紀錄本身，不會繼續解析同一 zone 內的 CNAME target。根據 RFC 1034 §3.6.2，authoritative server 遇到 CNAME 時必須在 zone data 內「restart the query at the domain name specified in the data field of the CNAME record」。這並非遞迴（recursion），而是使用本地 zone 資料繼續查詢（in-zone CNAME following）。

BIND 正確實作此行為：當 CNAME target 在同一 zone 內時，BIND 回傳 CNAME + target 的 A record（共 2 筆），ShadowDNS 只回傳 CNAME（1 筆）。此差異是 cname-synthesis 與 wildcard-support 完成後剩餘不一致的主要來源。

先前 cname-synthesis change 的 spec 將 in-zone CNAME following 歸類為「遞迴」並排除在範圍外，但這是對 RFC 的誤讀。Authoritative server 在自身 zone 內 follow CNAME 是 RFC 1034 §3.6.2 的明確要求，與向外部 name server 發出查詢的遞迴行為完全不同。

## What Changes

- 修改 DNS 查詢處理邏輯：當找到 CNAME 且 CNAME target 在同一 zone 內（in-bailiwick）時，以 `(target, 原始 qtype)` 繼續查詢，將 CNAME 與 target 的紀錄合併放入 ANSWER section
- 此行為適用於 root zone 查詢路徑（`handleRootQuery`）與 backup zone 查詢路徑（`alias.Resolve`）
- 支援 CNAME chain：若 target 本身也是 CNAME（仍在 zone 內），持續追蹤直到找到最終紀錄或離開 zone，並設迴圈上限（8 層）防止無限迴圈
- 當 CNAME target 不在 zone 內（out-of-bailiwick），維持現有行為——只回傳 CNAME 紀錄

## Non-Goals

- **遞迴解析（Recursion）**：不向外部 name server 發出查詢。Out-of-bailiwick CNAME target 仍只回傳 CNAME，由 client 的 recursive resolver 負責後續解析
- **DNAME 支援**：RFC 6672 DNAME 紀錄的合成不在本次範圍
- **Additional section 處理**：不主動在 Additional section 填入 glue record（如 CNAME target 的額外地址紀錄）

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `dns-server`: 新增 requirement — 當 CNAME target 在同一 zone 內時，server 必須 follow CNAME 並在 ANSWER section 回傳完整的 CNAME chain + 最終紀錄（RFC 1034 §3.6.2）
- `alias-resolver`: 新增 requirement — backup zone 查詢路徑的 `Resolve` 函式在 root zone 內找到 CNAME 時，同樣須 follow in-zone CNAME target

## Impact

- 受影響程式碼：`internal/server/handler.go`（`handleRootQuery` CNAME fallback 路徑）、`internal/alias/override.go`（`Resolve` 函式的 CNAME fallback 路徑）
- 受影響 specs：`dns-server`（新增 in-zone CNAME following requirement 與 scenarios）、`alias-resolver`（新增 backup zone CNAME following requirement）
