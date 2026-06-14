## Why

ShadowDNS 目前的 `/metrics` 端點在可觀測性上有三個明確缺口，已透過競品深度研究（CoreDNS、BIND 9/bind_exporter、PowerDNS Authoritative、Knot DNS）查證：

1. **缺少 Go runtime 與 process 指標。** `internal/metrics` 以 `prometheus.NewRegistry()` 建立自訂 registry，且只註冊 11 個 ShadowDNS 自訂 collector，未註冊 `NewGoCollector()` 與 `NewProcessCollector()`。結果 `/metrics` 完全沒有 `go_*`（goroutine、heap、GC）與 `process_*`（RSS、CPU、file descriptor）系列。所有競品都暴露這些指標——任何 dashboard 的 CPU / 記憶體 / goroutine / FD 面板都依賴它們，缺了就無法「開箱即用」。

2. **招牌的 ECS-aware view 選擇沒有任何可觀測信號。** ShadowDNS 以 EDNS Client Subnet（RFC 7871）驅動 geo view 為差異化特性，但目前無法得知：ECS 在查詢中的攜帶率、ECS 選項的分類健康度（valid / opt-out / malformed），以及各 view 有多少查詢實際使用了 ECS 衍生的 geo 位址。四個競品沒有一個提供「view 選擇來源」的觀測——這是 ShadowDNS 可佔的差異化空白。

3. **沒有開箱即用的 Grafana dashboard。** CoreDNS（#14981）、BIND（#12309）、PowerDNS（#20445）、Knot（#14989）皆提供社群或官方 dashboard，ShadowDNS 沒有，使用者需自行從零組裝面板與 PromQL。

## What Changes

- 在 `internal/metrics` 的自訂 registry 上註冊 `collectors.NewGoCollector()` 與 `collectors.NewProcessCollector()`，使 `/metrics` 一併暴露標準 `go_*` 與 `process_*` 系列。
- 新增兩個 DNS 指標，純粹以 handler 既有狀態計算，不改動 view matcher 熱路徑：
  - `shadowdns_dns_ecs_queries_total{family, status}`——ECS 選項分類計數（`status` ∈ `valid` / `opt_out` / `malformed`），用以計算 ECS 攜帶率與健康度。
  - `shadowdns_dns_view_selected_total{view, ecs_geo}`——成功解析 view 的查詢計數，`ecs_geo` ∈ `true` / `false` 表示該查詢的 view 解析是否有 ECS 衍生 geo 位址參與。
- 新增 `grafana/shadowdns-overview.json`——可直接 import 的 Grafana dashboard，涵蓋流量、延遲、rcode、ECS/view、rate-limit、reload/zone、runtime、panics 面板。**不**打包進 `.deb`。
- 新增雙語操作手冊頁 `docs/operations/monitoring.md`（與其 `.zh.md`），說明 Prometheus scrape 設定與 dashboard import 步驟；更新 `docs/index.md` 比較表與 `README.md` 功能列表反映 dashboard 已提供。

## Non-Goals (optional)

- **不變更** `shadowdns_dns_request_duration_seconds` 的 histogram bucket。既有 bucket 集合（100µs–100ms）是 2026-06-10 經 trace 記錄的刻意決定，明確排除秒級桶；對純記憶體權威伺服器而言 p99 落在 sub-ms 至低 ms，反轉此決定無足夠效益。
- **不做** geo-vs-ACL 的 view 選擇 provenance（即「view 是被 country/ASN geo rule 還是 ip/cidr ACL rule 選中」）。這需要改動 `Matcher.Resolve` 的回傳契約與 view matcher 熱路徑，風險與 perf-guard 成本較高；列為後續獨立 change。
- **不修** zone transfer（AXFR/IXFR）計進 `view="refused"` 的指標污染，**不新增** per-zone SOA serial gauge、zone transfer / NOTIFY 計數、request/response size histogram。這些競品具備但非 ShadowDNS 賣點，列為後續 change。
- **不**將 dashboard JSON 打包進 `.deb`，**不**提供 Prometheus alerting rules（本次僅 dashboard 與指標）。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `prometheus-metrics`: 新增 Go runtime / process collector 暴露、ECS 選項分類計數指標、view 選擇之 ECS-geo 參與計數指標。

## Impact

- Affected specs: `prometheus-metrics`
- Affected code:
  - New:
    - grafana/shadowdns-overview.json
    - docs/operations/monitoring.md
    - docs/operations/monitoring.zh.md
  - Modified:
    - internal/metrics/metrics.go
    - internal/server/handler.go
    - mkdocs.yml
    - docs/index.md
    - docs/index.zh.md
    - README.md
  - Removed: (none)
