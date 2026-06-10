## Context

`shadowdns_dns_request_duration_seconds` 是 ShadowDNS 在 DNS query hot path 上觀測的 histogram metric，定義於 `internal/metrics/metrics.go`，透過 `prometheus.NewHistogramVec` 建立，label 為 `view`。

目前使用 `prometheus.DefBuckets`，其值為：
`.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10`（單位：秒）

最小 bucket 邊界為 5ms。Authoritative DNS server 的正常查詢處理時間通常在 50µs–2ms 之間，絕大多數請求落在 DefBuckets 最小邊界（5ms）以下，導致幾乎所有請求都被統計進同一個 bucket，P99/P95 延遲在 Grafana 上幾乎無從分辨。

**並行 change 說明**：`reload-coverage-and-metrics` change 同樣修改 `internal/metrics/metrics.go`，但它新增的是 reload 相關 metric（宣告），與本 change 修改的 histogram bucket 參數屬於不同程式碼段，無衝突。兩個 change 可獨立合併；若 merge 順序上先於本 change，diff 仍可乾淨 apply。

## Goals / Non-Goals

**Goals:**
- 將 `shadowdns_dns_request_duration_seconds` 的 bucket 邊界換成涵蓋 100µs–100ms 的 DNS 場景自訂值。
- 更新 `prometheus-metrics` spec 中對應 requirement，正式記錄新 bucket 規格。
- 確保換 bucket 後 unit test 仍通過（現有測試檢查 bucket 存在性即可）。

**Non-Goals:**
- 不修改 histogram 的 label（`view`）或命名（`shadowdns_dns_request_duration_seconds`）。
- 不提供可設定 bucket 的 CLI flag（v0.x.x 實驗階段不需要）。
- 不提供 Grafana dashboard migration guide（v0.x.x 實驗階段可接受資料斷層）。
- 不修改 `reload-coverage-and-metrics` change 所涉及的任何 reload metric。
- 不觸及其他 metric 的 bucket（如其他 histogram）。

## Decisions

### 選用 DNS 場景自訂 buckets

**決策**：採用以下 10 個 bucket（單位：秒）：

```
0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1
```

對應為：100µs、250µs、500µs、1ms、2.5ms、5ms、10ms、25ms、50ms、100ms。

**理由**：
- Authoritative DNS 正常延遲集中在 100µs–1ms，需要在此區間有足夠解析度。
- 100ms 作為最大 bucket 可捕捉明顯異常（例如 GeoIP 查詢暫停、I/O stall）；超過 100ms 的請求已是嚴重異常，+Inf 即可。
- 以對數刻度均勻分佈（每級約 2.5 倍），與 Prometheus 官方建議的 `prometheus.ExponentialBuckets` 精神一致，同時保留特定對齊點（0.5ms、1ms、10ms、100ms）以利 recording rule 撰寫。
- Bucket 數量 10 個，符合 Prometheus 效能建議（< 20 buckets）。

**評估替代方案**：
- `prometheus.ExponentialBuckets(0.0001, 2.5, 10)`：與手動列舉結果幾乎相同，但自動計算容易因浮點誤差使邊界值不直觀（例如 0.009765...），改為手動明確列舉以利 spec 精確描述。
- 保留 DefBuckets 並加上細粒度前綴（0.0001, 0.0005, 0.001 + DefBuckets）：bucket 總數 14 個，且包含 5s/10s 等對 DNS 無意義的極端值，被排除。
- Prometheus LinearBuckets：線性刻度無法同時覆蓋 100µs–100ms 的四個數量級，被排除。

## Implementation Contract

**行為**：修改後，`/metrics` endpoint 輸出中 `shadowdns_dns_request_duration_seconds_bucket` 的 `le` 標籤值將為 `0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, +Inf`，取代原本的 `0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, +Inf`。

**實作範圍**：
- 在 `internal/metrics/metrics.go` 的 `New()` 函式中，將 `prometheus.NewHistogramVec` 的 `Buckets` 欄位從 `prometheus.DefBuckets` 改為明確的 `[]float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1}`。
- 在 `openspec/changes/dns-latency-histogram-buckets/specs/prometheus-metrics/spec.md` 中，以 MODIFIED Requirements 更新 `Measure DNS request processing duration` 的規格描述，移除「預設 Prometheus buckets」字眼並列出新 bucket 值。

**驗收標準**：
- `make test` 通過（race detector 開啟）。
- `make lint` 無新 warning。
- 執行 `go run ./cmd/shadowdns --named-conf <path> --config <path>`（`--named-conf` 與 `--config` 為必填 flag；metrics endpoint 預設即監聽 `:9153`）後，`curl http://localhost:9153/metrics | grep dns_request_duration_seconds_bucket` 輸出中包含 `le="0.0001"` 且不含 `le="0.25"`（0.25 屬於 DefBuckets 但不在新 bucket 集合中）。

**範圍邊界**：
- 僅修改 `internal/metrics/metrics.go` 中 `requestDuration` 的 `Buckets` 欄位，以及上述 delta spec。
- 既有測試無需更新（現有 unit test 以 `m.Gather()` 驗證 metric 存在性，不 assert bucket 邊界）。依專案 `tdd: true` 偏好，在 `internal/metrics/metrics_test.go` 新增兩個測試：assert bucket 邊界集合（spec scenario「Metrics endpoint exposes correct bucket boundaries」）與依 spec Example 表格的 bucket 歸屬參數化測試。
- 不修改 `reload-coverage-and-metrics` change 所觸及的任何宣告。

## Risks / Trade-offs

- [風險] 既有 Grafana recording rules 如果以固定 `le` 值計算 histogram_quantile，bucket 消失後會產生 NaN。→ 緩解：v0.x.x 實驗階段，目前 ns2 為測試部署，無生產告警依賴此 metric；接受此風險，未來建 Grafana dashboard 時再以新 bucket 為準。
- [風險] 改動 `internal/metrics/metrics.go` 與 `reload-coverage-and-metrics` change 有檔案衝突的可能。→ 緩解：兩個 change 修改的是不同行（bucket 參數 vs. 新 metric 宣告）；若 merge 產生 conflict，以 diff 手動解決即可，範圍極小。
