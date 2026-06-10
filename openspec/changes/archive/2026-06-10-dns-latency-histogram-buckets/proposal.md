## Why

`shadowdns_dns_request_duration_seconds` 目前使用 Prometheus 預設 buckets（`DefBuckets`），最細粒度只到 5ms。Authoritative DNS 的正常處理延遲落在亞毫秒級，現有 buckets 無法呈現 P99 延遲細節，在 Grafana 上退化幾乎難以辨識。

## What Changes

- 將 `shadowdns_dns_request_duration_seconds` histogram 的 bucket 邊界，從 Prometheus `DefBuckets` 改為專為 DNS 場景設計的自訂值，涵蓋約 100µs 到 100ms（以秒表示）。
- 更新 `prometheus-metrics` spec 中關於此 histogram 的 requirement，移除「使用預設 Prometheus buckets」的描述，改為「使用 DNS 場景適用的自訂 buckets」。
- **注意**：bucket 邊界變更屬於 metric 中斷性修改——既有的 Grafana dashboard recording rules 與歷史時序資料的 bucket 標籤將不再對齊。本專案處於 v0.x.x 實驗階段，此類變更可接受。

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `prometheus-metrics`：修改 `shadowdns_dns_request_duration_seconds` histogram 的 bucket 邊界規格——從「使用預設 Prometheus buckets」改為「使用 DNS 場景自訂 buckets（100µs–100ms 範圍）」。

## Impact

- Affected specs: `prometheus-metrics`（delta spec）
- Affected code:
  - Modified: `internal/metrics/metrics.go`
