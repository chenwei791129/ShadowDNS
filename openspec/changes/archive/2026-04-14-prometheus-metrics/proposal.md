## Why

ShadowDNS 目前缺乏運行時可觀測性——無法從外部得知查詢量、回應延遲、回應碼分佈、已載入 zone 數量或 GeoIP 資料庫版本。加入 Prometheus `/metrics` endpoint 能讓營運團隊透過 Grafana 等工具即時掌握服務狀態，並在異常時觸發告警。

## What Changes

- 新增 `internal/metrics` package，定義 8 個 Prometheus metrics（Counter、Histogram、Gauge）
- 在 `ServeDNS` 路徑上埋點，透過 wrapper `ResponseWriter` 統一收集 request/response metrics
- 在 `main.go` 啟動獨立 HTTP server 提供 `/metrics` endpoint
- 新增 `-metrics-addr` flag（預設 `:9153`，設為空字串停用）
- 從 `maxminddb.Reader.Metadata.BuildEpoch` 取得 GeoIP 資料庫建置時間，暴露為 info-style gauge
- 在 `CountryDB` 和 `ASNDB` 新增 `Metadata()` accessor

## Non-Goals

- 不實作 DNS 封包大小 histogram（`request_size_bytes` / `response_size_bytes`）——authoritative DNS 封包大小穩定，監控價值低
- 不加入 `zone` label——alias zones 可能很多，會造成 cardinality 爆炸
- 不加入 `server` label——ShadowDNS 單一 listen address，無需區分
- 不做 push gateway 或 remote write——僅提供標準 pull model
- 不做 DNSSEC DO-bit 統計——目前不支援 DNSSEC

## Capabilities

### New Capabilities

- `prometheus-metrics`: Prometheus `/metrics` HTTP endpoint，暴露 DNS 查詢統計、回應碼分佈、延遲直方圖、zone 載入數量、GeoIP 資料庫資訊、build info 及 panic 計數

### Modified Capabilities

(none)

## Impact

- 新增依賴：`github.com/prometheus/client_golang`（已在 `go.mod` indirect 中，升級為 direct）
- 新增檔案：`internal/metrics/metrics.go`
- 修改檔案：`cmd/shadowdns/main.go`（flag + HTTP server）、`internal/server/handler.go`（埋點）、`internal/view/geoip_country.go`（Metadata accessor）、`internal/view/geoip_asn.go`（Metadata accessor）
