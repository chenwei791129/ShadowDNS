## Why

ShadowDNS 是 authoritative-only 的權威 DNS 伺服器，暴露在公網上即面臨 DNS 放大／反射攻擊：攻擊者偽造受害者來源 IP、反覆發送會產生大回應的查詢，把 ShadowDNS 當成流量放大器轟炸受害者。README 將 Response Rate Limiting (RRL) 列為 planned feature 並承諾與 BIND 相容；目前 ShadowDNS 對 UDP 回應沒有任何速率上限，無法緩解此類攻擊。本變更補上這道防線，並讓既有 BIND 部署能直接沿用其 `rate-limit { ... }` 配置遷移過來。

## What Changes

- 新增 `internal/ratelimit` package，實作 BIND 相容的 token-bucket（credit）速率限制帳本：每個 `(遮罩後 client 位址, 回應類別, 推定 name)` 帳戶每秒回補 credit、上限為 `window × rate`，回應扣 credit，餘額為負時依 `slip` 決定 drop 或回 TC=1 截斷。
- 在一個 `dns.ResponseWriter` wrapper 的 `WriteMsg` 收斂點套用限流（沿用既有 `metrics.ResponseWriter` 手法），從 `dns.Msg` 推導回應類別，避免邏輯散落在多個 reply 函式。
- **僅對 UDP 套用**；TCP 一律放行（TCP 無法偽造來源，且已完成三向交握）。
- 支援的回應類別：`responses`（正常答案）、`nxdomains`、`nodata`、`errors`，以及不分類別的 `all` 總上限。
- name imputation 規則：`responses` 用 qname 聚合；`nxdomains` / `nodata` 用命中的 zone origin 聚合（使 random-subdomain 洪水落入同一桶而限得住）；`errors`（含 REFUSED）用空 name 聚合。
- 解析 BIND 相容的 `rate-limit { ... }` 區塊（置於 `options`），支援子選項 `responses-per-second`、`referrals-per-second`、`nodata-per-second`、`nxdomains-per-second`、`errors-per-second`、`all-per-second`、`window`、`slip`、`ipv4-prefix-length`、`ipv6-prefix-length`、`exempt-clients`、`log-only`、`max-table-size`、`min-table-size`，預設值對齊 BIND（rps=0 停用、window=15、slip=2、v4=/24、v6=/56、table 20000/500）。
- `referrals-per-second`：解析以維持 BIND 相容，但 ShadowDNS 為 authoritative-only 且丟棄 sub-delegation NS、不產生 referral 回應，故此類別內部永不命中。
- `qps-scale`：**不解析**；於 `rate-limit` 區塊內遇到即發出 warning 並忽略（負載自適應功能正交且需額外全域 QPS 量測，本變更不納入）。
- view 內出現 `rate-limit`：發出 warning 並忽略（v1 僅支援 `options` 全域 scope），不視為 fatal，維持配置遷移友善度。
- `log-only yes` 時只記錄「本來會 drop/slip」而不實際限流，供上線前試運轉。
- 新增 Prometheus 計數器：依類別與動作（dropped / slipped / log-only would-drop）統計。

## Non-Goals (optional)

(none — 設計取捨記於 design.md)

## Capabilities

### New Capabilities

- `response-rate-limiting`: UDP 回應速率限制的核心演算法與執行——token-bucket 帳本、回應類別分類、name imputation、slip 截斷 vs drop 決策、exempt-clients 豁免、log-only 試運轉、帳本容量管理。

### Modified Capabilities

- `config-loader`: 新增解析 `options` 內的 `rate-limit { ... }` 巢狀區塊與其子選項；對 view 內 `rate-limit` 與 `qps-scale` 子選項發出 warning 並忽略。
- `prometheus-metrics`: 新增 RRL 相關計數器（依類別與動作）。

## Impact

- Affected specs: `response-rate-limiting`（新增）、`config-loader`（修改）、`prometheus-metrics`（修改）
- Affected code:
  - New:
    - internal/ratelimit/limiter.go
    - internal/ratelimit/table.go
    - internal/ratelimit/classify.go
    - internal/ratelimit/writer.go
    - internal/config/ratelimit.go
  - Modified:
    - internal/config/options.go
    - internal/config/zones.go
    - internal/server/handler.go
    - internal/server/server.go
    - internal/metrics/metrics.go
    - cmd/shadowdns/main.go
    - README.md
  - Removed: (none)
