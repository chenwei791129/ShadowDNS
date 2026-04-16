## Why

ShadowDNS 作為 authoritative DNS server，目前無法動態新增 DNS record。申請 SSL 憑證時的 ACME DNS-01 challenge 需要在 `_acme-challenge.<domain>` 建立暫時性的 TXT record，現行做法需手動編輯 zone file 再 reload，流程繁瑣且不利自動化。原生支援 ephemeral TXT record API 可省去外部 DNS plugin（如 acme-dns）的維運成本，並讓 ACME client（certbot、acme.sh、lego）直接透過 HTTP API 完成驗證。

## What Changes

- 新增獨立的 HTTP API server，監聽獨立 port，提供 ephemeral TXT record 的 CRUD 操作
- 新增獨立設定檔（非 named.conf），配置 API listen address、IP ACL（必填）、pre-shared token（選填）
- 新增 in-memory ephemeral record store，與 zone file 完全分離
- DNS handler 在查詢時額外查詢 ephemeral store，合併回應結果
- Ephemeral record 帶有 TTL，到期後自動消失；reload/restart 亦清除所有 ephemeral records
- Response 中的 TTL 值為動態計算的剩餘秒數

## Non-Goals

- 不支援 TXT 以外的 record type（未來可擴充，但本次僅限 TXT）
- 不持久化 ephemeral records（不寫入 zone file、不寫入磁碟）
- 不實作 DNS UPDATE (RFC 2136) 協定——僅透過 HTTP API 操作
- 不修改現有的 zone file 載入與 reload 邏輯
- 不實作完整的 zone management API 或 dashboard（屬未來規劃）

## Capabilities

### New Capabilities

- `ephemeral-record-store`: In-memory ephemeral record 儲存與 TTL 過期管理（lazy eviction + periodic GC）
- `ephemeral-api`: HTTP API server，提供 ephemeral TXT record 的新增與刪除，含 IP ACL 與可選 token 驗證
- `ephemeral-api-config`: 獨立設定檔的載入與驗證，配置 API listen address、IP ACL、pre-shared token

### Modified Capabilities

- `dns-server`: DNS handler 需在 zone lookup 之外額外查詢 ephemeral store，將 ephemeral TXT records 合併至回應

## Impact

- 新增檔案：`internal/ephemeral/` (store)、`internal/api/` (HTTP server + handlers)、`internal/api/config.go` (API config loader)
- 修改檔案：`internal/server/handler.go`（整合 ephemeral lookup）、`internal/server/server.go`（ServerState 或 Server 持有 ephemeral store 參照）、`cmd/shadowdns/main.go`（啟動 API server、新增 CLI flag）
- 新增依賴：無（使用 `net/http`）
- 新增 CLI flag：`-api-conf`（API 設定檔路徑）
