## 1. Metrics Package 基礎建設

- [x] [P] 1.1 建立獨立 `internal/metrics` package（`internal/metrics/metrics.go`），定義 `Metrics` struct 和獨立 `prometheus.Registry`，註冊 8 個 collector：`shadowdns_dns_requests_total` (Counter)、`shadowdns_dns_responses_total` (Counter)、`shadowdns_dns_request_duration_seconds` (Histogram)、`shadowdns_build_info` (Gauge)、`shadowdns_zones_loaded` (Gauge)、`shadowdns_zones_backup` (Gauge)、`shadowdns_geoip_db_info` (Gauge)、`shadowdns_panics_total` (Counter)。提供 `New()` constructor 和 `Handler()` 方法回傳 `http.Handler`
- [x] [P] 1.2 新增 GeoIP Metadata accessor：在 `CountryDB` 和 `ASNDB` 各新增 `Metadata()` 方法回傳 `maxminddb.Metadata`，供 metrics package 讀取 GeoIP database metadata（`BuildEpoch`、`DatabaseType`）

## 2. DNS Handler 埋點

- [x] 2.1 實作 Wrapper ResponseWriter 統一埋點：在 `internal/metrics/metrics.go` 新增 `metricsResponseWriter` wrapper struct，嵌入 `dns.ResponseWriter` 並覆寫 `WriteMsg` 方法，在寫入時記錄 rcode（count DNS responses by rcode and view）和 duration（measure DNS request processing duration）
- [x] 2.2 在 `Server` struct 新增 `Metrics *metrics.Metrics` 欄位，修改 `NewServer` 接受 optional metrics 參數（metrics 注入方式）。在 `ServeDNS` 入口：若 `s.Metrics != nil`，記錄 start time、用 wrapper ResponseWriter 包裝，在 defer 中完成 duration 觀測；呼叫 `RecordRequest` count DNS requests by protocol, family, type, and view；在 panic recovery 中呼叫 `RecordPanic` count panics recovered in DNS handler

## 3. Zone Gauge 與 GeoIP Info

- [x] 3.1 在 `Metrics` 上新增 `SetZones(rootZones, backupZones map[string]map[string]*zone.Zone)` 方法，計算每個 view 的 root/backup zone 數量並更新 gauge（report loaded zone counts per view）。在 `NewServer` 和 `SwapState` 中呼叫
- [x] 3.2 在 `Metrics` 上新增 `SetGeoIPInfo(country *view.CountryDB, asn *view.ASNDB)` 方法，從 `Metadata().BuildEpoch` 轉為 ISO 8601 字串設定 `shadowdns_geoip_db_info` gauge label（expose GeoIP database metadata）

## 4. HTTP Server 與 Flag 整合

- [x] 4.1 `-metrics-addr` flag 控制 HTTP server：在 `main.go` 新增 `-metrics-addr` flag（預設 `:9153`），在 `run()` 中根據 flag 值決定是否建立 `metrics.Metrics` 和啟動 HTTP server expose Prometheus metrics via HTTP endpoint。設定 `shadowdns_build_info` expose build information as a gauge。在 context 取消時 graceful shutdown of metrics HTTP server
- [x] 4.2 將 `prometheus/client_golang` 從 indirect 依賴升級為 direct 依賴（`go get github.com/prometheus/client_golang`）

## 5. 測試

- [x] [P] 5.1 為 `internal/metrics` 撰寫單元測試：驗證 `New()` 建立的 registry 包含所有預期 metric、`RecordRequest` 和 `RecordPanic` 正確 increment counter、`SetZones` 正確更新 gauge、`SetGeoIPInfo` 正確設定 label、`Handler()` 回傳的 HTTP handler 回應 200 且包含 prometheus exposition format
- [x] [P] 5.2 為 `metricsResponseWriter` 撰寫單元測試：驗證 `WriteMsg` 正確記錄 rcode 和 duration，且委託給底層 writer
- [x] [P] 5.3 為 `CountryDB.Metadata()` 和 `ASNDB.Metadata()` 撰寫單元測試
- [x] 5.4 為 `ServeDNS` metrics 埋點撰寫整合測試：發送 DNS 查詢後驗證 counter 和 histogram 有更新，metrics 為 nil 時不 panic
