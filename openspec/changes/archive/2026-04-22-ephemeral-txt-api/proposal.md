## Why

ShadowDNS 作為 authoritative DNS server，目前無法動態新增 DNS record。申請 SSL 憑證時的 ACME DNS-01 challenge 需要在 `_acme-challenge.<domain>` 建立暫時性的 TXT record，現行做法需手動編輯 zone file 再 reload，流程繁瑣且不利自動化。原生支援 ephemeral TXT record API 可省去外部 DNS plugin（如 acme-dns）的維運成本，並讓 ACME client（certbot、acme.sh、lego）直接透過 HTTP API 完成驗證。

此外，為了避免設定檔碎片化，本 change 同時把現有的 `aliases.yaml` 併入一份總 ShadowDNS YAML 設定檔，讓 alias 映射與 ephemeral TXT API 設定共用同一入口、同一 reload 時序。

## What Changes

- 新增一份總 ShadowDNS YAML 設定檔（例如 `/etc/shadowdns/shadowdns.yaml`），內含兩個 section：
  - `aliases`：root ↔ backup domain 映射（取代現行 `aliases.yaml`）
  - `ephemeral_api`：API `listen`、`allow`（IP ACL，必填）、`token`（選填）
- 新增獨立的 HTTP API server，監聽獨立 port，提供 ephemeral TXT record 的 CRUD 操作；API 設定只讀取總設定檔中的 `ephemeral_api` section
- 新增 in-memory ephemeral record store，與 zone file 完全分離
- DNS handler 在查詢時額外查詢 ephemeral store，合併回應結果
- Ephemeral record 帶有 TTL，到期後自動消失；reload/restart 亦清除所有 ephemeral records
- Response 中的 TTL 值為動態計算的剩餘秒數
- **新增 CLI flag `--config`** 指向總設定檔，**移除 `--aliases` flag**，不新增 `--api-conf` flag（遵循 cobra 遷移後的 POSIX 雙破折號慣例）
- **Reload 原子性**：SIGHUP 時先完整解析新總設定檔並通過所有 section 的驗證，才整體切換；任一 section 驗證失敗則保留舊狀態並記錄錯誤
- **多值 TXT 支援**：同一 FQDN 允許存在多筆 ephemeral TXT value（對應 ACME 同時驗證 apex + wildcard 會在相同 `_acme-challenge.<domain>` 放兩筆 token 的情境）。PUT 對已存在相同 value 會刷新 TTL，對新 value 會追加；DNS 回應將所有未過期 value 合併為同一 RRSet。
- **DELETE 全清語意**：`DELETE /v1/txt/{fqdn}` 移除該 FQDN 下**所有** ephemeral records（不針對單一 value）。Zone file 中同名 record 不受影響——API 只觸及 ephemeral store。

## Non-Goals

- 不支援 TXT 以外的 record type（未來可擴充，但本次僅限 TXT）
- 不持久化 ephemeral records（不寫入 zone file、不寫入磁碟）
- 不實作 DNS UPDATE (RFC 2136) 協定——僅透過 HTTP API 操作
- 不修改現有的 zone file 載入與 reload 邏輯
- 不實作完整的 zone management API 或 dashboard（屬未來規劃）
- **不保留向後相容**：舊版獨立 `aliases.yaml` 與 `--aliases` flag 直接移除，不提供 migration 工具、不同時接受兩種格式
- **不提供「刪除單一 value」的 endpoint**：DELETE 永遠清除 FQDN 下所有 ephemeral entries。若未來需要 per-value 刪除，屬後續擴充。

## Capabilities

### New Capabilities

- `ephemeral-record-store`: In-memory ephemeral record 儲存與 TTL 過期管理（lazy eviction + periodic GC）
- `ephemeral-api`: HTTP API server，提供 ephemeral TXT record 的新增與刪除，含 IP ACL 與可選 token 驗證
- `shadowdns-config`: 總 ShadowDNS YAML 設定檔的載入與驗證，內含 `aliases` 與 `ephemeral_api` 兩個 section；reload 需全部通過才整體切換

### Modified Capabilities

- `dns-server`: DNS handler 需在 zone lookup 之外額外查詢 ephemeral store，將 ephemeral TXT records 合併至回應
- `config-loader`: 不再獨立解析 `aliases.yaml`；aliases 改由 `shadowdns-config` 提供的結構取得；`--aliases` CLI flag 移除

## Impact

- 新增檔案：
  - `internal/ephemeral/` (store)
  - `internal/api/` (HTTP server + handlers)
  - `internal/shadowdnscfg/` (總設定檔 loader + schema + reload 原子切換)
  - `dist/shadowdns.yaml.example` (總設定檔範例，取代 `aliases.yaml.example`)
- 修改檔案：
  - `internal/server/handler.go`（整合 ephemeral lookup）
  - `internal/server/server.go`（`Server` 持有 ephemeral store 參照）
  - `internal/config/aliases.go`（改為從 `shadowdns-config` 結構讀取，而非獨立解析檔案）
  - `cmd/shadowdns/main.go`（在 cobra 的 `registerServerFlags` 新增 `--config` flag、移除 `--aliases` flag、啟動 API server、SIGHUP reload 原子切換）
  - `packaging/` 下安裝腳本、systemd unit、範例檔路徑
- 刪除檔案：
  - `dist/aliases.yaml.example`
- 新增依賴：無（使用 `net/http`、`gopkg.in/yaml.v3`）
- 新增 CLI flag：`--config`（總設定檔路徑，必填）
- 移除 CLI flag：`--aliases`
