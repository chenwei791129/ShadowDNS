## Context

ShadowDNS 是一個 authoritative DNS server，目前所有運行時狀態僅透過 structured log 輸出。營運上缺乏量化指標來監控查詢量、延遲分佈和回應碼，也無法追蹤 GeoIP 資料庫的版本狀態。

現有架構：所有 DNS 查詢進入 `Server.ServeDNS`（handler.go），經 view matching、zone lookup、alias resolution 後回應。回應透過三個路徑送出：`replyRcode`、`replyWithAnswer`、`negativeReply`。GeoIP DB 在 `view` package 中載入，`maxminddb.Reader` 持有 `Metadata` 欄位含 `BuildEpoch`。

`prometheus/client_golang` 已存在於 `go.mod` indirect 依賴中（nfpm 引入），升級為 direct 不增加新的傳遞依賴。

## Goals / Non-Goals

**Goals:**

- 提供 8 個 Prometheus metrics 覆蓋流量、延遲、回應碼、zone 狀態、GeoIP 版本、panic 計數
- 透過獨立 HTTP server 在可設定 port 上提供 `/metrics` endpoint
- 埋點對 DNS 查詢效能影響可忽略
- 支援 hot reload 後自動更新 zone gauge

**Non-Goals:**

- 不實作封包大小 histogram（authoritative DNS 封包大小穩定，監控價值低）
- 不加入 `zone` 或 `server` label（避免高 cardinality）
- 不做 push gateway / remote write
- 不做 DNSSEC DO-bit 統計

## Decisions

### 建立獨立 `internal/metrics` package

所有 Prometheus collector 定義集中在 `internal/metrics/metrics.go`。對外暴露一個 `Metrics` struct 和相應的 `Record*` 方法。`server` package 呼叫 `metrics.Record*` 方法，不直接操作 prometheus 型別。

**替代方案**：將 metrics 定義散落在各 package。拒絕原因：分散管理增加維護成本，且 metric 命名前綴不容易統一。

### Wrapper ResponseWriter 統一埋點

在 `ServeDNS` 入口將 `dns.ResponseWriter` 包裝為 `metricsResponseWriter`，攔截 `WriteMsg` 呼叫以記錄 rcode 和 response duration。這樣 `replyRcode`、`replyWithAnswer`、`negativeReply` 三個回應路徑不需要個別修改。

wrapper struct 嵌入原始 `dns.ResponseWriter`，僅覆寫 `WriteMsg` 方法。在 `WriteMsg` 中記錄 rcode 後委託給底層 writer。duration 從 `ServeDNS` 入口的 `time.Now()` 到 `WriteMsg` 呼叫的時間差計算。

**替代方案**：在每個 reply helper 手動呼叫 metrics。拒絕原因：三個回應路徑 + panic recovery 路徑共四處，容易遺漏且不易維護。

### `-metrics-addr` flag 控制 HTTP server

新增 `-metrics-addr` flag，預設 `:9153`（與 CoreDNS 慣例一致）。設為空字串時完全停用 metrics HTTP server，不建立任何 prometheus collector，適合測試或 embedded 場景。

HTTP server 在 `run()` 中 DNS listener bind 後啟動，使用 `prometheus.NewRegistry()`（非 default registry）避免混入 Go runtime 預設指標。在 context 取消時 graceful shutdown。

**替代方案**：使用 default registry。拒絕原因：default registry 自帶 `go_*` 和 `process_*` 指標，首次版本保持乾淨，只暴露 ShadowDNS 自有指標。

### GeoIP Metadata accessor

在 `CountryDB` 和 `ASNDB` 各新增 `Metadata()` 方法，回傳 `maxminddb.Metadata`。metrics package 在啟動時讀取 `BuildEpoch` 轉為 ISO 8601 字串設定 `shadowdns_geoip_db_info` gauge label。

### Metrics 注入方式

`Server` struct 新增 `Metrics *metrics.Metrics` 欄位（pointer，nil 代表停用）。`NewServer` 接受 optional `Metrics` 參數。`ServeDNS` 在入口檢查 `s.Metrics != nil`，為 nil 時跳過所有埋點，保持與現有測試的相容性。

zone gauge 在 `NewServer` 和 `SwapState` 時更新。

## Risks / Trade-offs

- **[風險] Histogram bucket 選擇不當** → 使用 `prometheus.DefBuckets`（.005 到 10 秒），涵蓋 DNS 查詢的合理範圍。若實際使用中發現不合適，後續調整 bucket 不影響相容性。
- **[風險] Hot reload 期間 gauge 暫時不一致** → `SwapState` 在 atomic swap 後立即更新 zone gauge，窗口極短（微秒級），Prometheus 15 秒抓取間隔不會觀察到。
- **[取捨] 獨立 registry vs default registry** → 犧牲了 Go runtime 指標（`go_goroutines` 等），換取乾淨的指標命名空間。使用者可透過 process exporter 另外收集 runtime 指標。
