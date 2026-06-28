## Why

現行 DoH 端點僅支援 RFC 8484 的 wire-format（`application/dns-message`），要求 client 自行組出 binary DNS 查詢。這對維運人員撰寫與維護驗證腳本極不友善：本專案實測中，用 curl 查一筆 TXT 必須先以 dnspython 產生 wire query、再做 base64url 編碼才能送出。Google Public DNS 與 CloudFlare 採用的事實格式 `application/dns-json` 以 `GET ?name=&type=` 搭配 JSON 回應，讓 `curl` + `jq` 零依賴即可查詢，並可透過 `edns_client_subnet` 參數在單機模擬任意網段、驗證 split-horizon / GeoIP 行為。

## What Changes

- 在現有 `/dns-query` 端點新增 `application/dns-json` 格式支援，以 content negotiation（`Accept: application/dns-json`）與既有 wire-format 並存；未要求 JSON 的請求行為與現狀逐位元相同（純 additive）。
- 新增 JSON 查詢解析：`GET` 參數 `name`（必填）、`type`（預設 `A`，接受型別名稱或數值）、`edns_client_subnet`（解析為來源網段並注入 EDNS0 Client Subnet option）。
- JSON 路徑重用同一條 `ServeDNS` 權威查詢路徑（view 選擇、ephemeral overlay、ratelimit、metrics 全部沿用）；回應序列化為 Google Public DNS JSON schema（`Status`、`TC`、`RD`、`RA`、`AD`、`CD`、`Question`、`Answer[]`，並回填實際生效的 `edns_client_subnet` scope）。
- `edns_client_subnet` 透過注入 `dns.EDNS0_SUBNET` option 實作，使下游既有 ECS 邏輯透明運作，與「wire-format DoH 攜帶真實 ECS」行為一致；binary 未開 `--ecs-enable` 時，注入的 option 由 `ServeDNS` 忽略（與 wire 查詢同行為）。
- 容忍但忽略 `cd` 參數（不報錯）；不支援 `do`（DNSSEC）與 `ct`（content-type 覆寫）。
- 更新 DoH 使用手冊（`docs/guides/doh.md` 與 `docs/guides/doh.zh.md`）說明 JSON 格式、參數與範例。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `doh-endpoint`: 新增「以 `application/dns-json` 格式接受查詢並回應」相關需求 —— content negotiation 與格式選擇、JSON 查詢參數解析（`name`/`type`/`edns_client_subnet`）、ECS 經 query 參數注入、JSON 回應 schema 與 ECS scope 回填、`cd` 容忍忽略與 `do`/`ct` 不支援、malformed JSON 查詢的錯誤回應。

## Impact

- Affected specs: doh-endpoint (modified)
- Affected code:
  - New: internal/doh/dnsjson.go, internal/doh/dnsjson_test.go
  - Modified: internal/doh/server.go, docs/guides/doh.md, docs/guides/doh.zh.md
  - Removed: (none)
