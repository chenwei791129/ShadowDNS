## Why

目前 `PUT /v1/txt/{fqdn}` 對任意 FQDN 都會寫入 ephemeral store 並回 HTTP 200，即使該 FQDN 不在任何已載入的 zone 底下。結果是 ACME client 收到 `{"status":"ok"}`，但隨後對該名稱做 DNS TXT 查詢時會拿到空結果（因為 server 根本不 authoritative 於那個名字），形成 silent failure。常見肇因是 caller 端 typo（例如 `_acme-challenge.exmaple.com` 本想打 `example.com`），但目前 API 無法分辨。

## What Changes

- **Modified**：`PUT /v1/txt/{fqdn}` 在寫入 ephemeral store **之前**，SHALL 檢查 canonical FQDN 是否 in-bailiwick 於任一已載入的 root 或 backup zone origin（跨所有 view、兩種 role 都納入比對範圍）。
- **Modified**：若 FQDN 不落在任何已載入 zone 底下，API SHALL 回 HTTP 422 Unprocessable Entity，沿用既有 `{"status":"error","error":"..."}` JSON shape，且 SHALL NOT 寫入 ephemeral store。
- **Unchanged**：`DELETE /v1/txt/{fqdn}` 維持現行冪等語義，不受此檢查影響。
- **Unchanged**：既有的 IP ACL、token、TTL clamp、value 長度驗證行為不變；zone 檢查發生在那些檢查之後、`store.Put` 之前。
- **Reload 行為**：zone origin 的比對資料來源必須在每次請求時讀取最新 snapshot，這樣 SIGHUP 後新增或移除的 zone 會立刻反映在 API 的允收清單上，不需要重啟 API server。

## Non-Goals

- 不檢查 caller 的 source IP 對應的 view 是否實際服務該 FQDN。只要有任一 view 或任一 role（root/backup）載入了涵蓋此 FQDN 的 zone，就視為「可服務」。避免 ACME client 因換網段或 resolver view 不同而誤判失敗。
- 不對 DELETE 加上相同檢查，保持清理語義的冪等性。
- 不引入 per-FQDN 配額、rate limit、或寫入後異步 reconcile 機制。
- 不修改 `--dry-run`、smoke 腳本、metrics、或 packaging 設定。

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `ephemeral-api`: 新增一條 PUT 前置檢查 requirement（拒絕不在任何已載入 zone 底下的 FQDN）。

## Impact

- Affected specs:
  - `openspec/specs/ephemeral-api/spec.md` — 新增一條 Requirement
- Affected code:
  - Modified:
    - `internal/api/server.go` — `handlePut` 加入 zone-membership 檢查；`NewServer` 新增 zone origin provider 依賴
    - `internal/api/server_test.go` — 新增對應測試
    - `cmd/shadowdns/main.go` — 建構 API server 時注入讀取 `srv.CurrentState().ZoneOrigins` 的 provider
    - `cmd/shadowdns/main_ephemeral_test.go` — 補強整合測試
  - New:
    - （若需要可新增一個 `internal/api/zonecheck.go` 承載比對邏輯；視實作簡潔度決定，可直接放在 `server.go`）
  - Removed: （無）
- Affected runtime behavior:
  - API PUT 對未知 zone 的請求由 200 變 422（對合法 caller 而言是新增的錯誤路徑，符合 v0.x.x 實驗階段可接受的行為調整）
