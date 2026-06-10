<!--
Each task description MUST state:
- the behavior or contract being delivered (what is observably true when the
  task is complete), and
- the verification target that proves completion (test, CLI invocation,
  analyzer check, manual assertion, or content review).

File paths are supporting context for locating the work, never the task
itself. "Edit file X" is not a valid task — it is missing both behavior and
verification.
-->

## 1. 實作：修改 histogram bucket

- [x] [P] 1.1 在 `internal/metrics/metrics.go` 的 `New()` 函式中，將 `requestDuration` 的 `Buckets` 欄位從 `prometheus.DefBuckets` 改為 `[]float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1}`（選用 DNS 場景自訂 buckets，符合設計決策「選用 DNS 場景自訂 buckets」）。完成後 `make test` 通過，且 `/metrics` endpoint 輸出中 `shadowdns_dns_request_duration_seconds_bucket` 的 `le` 標籤包含 `le="0.0001"` 且不含 `le="0.25"` 等原 DefBuckets 邊界。驗證：`make test`；手動執行 `go run ./cmd/shadowdns --named-conf <path> --config <path>`（兩者皆為必填 flag，metrics 預設監聽 `:9153`）並 `curl http://localhost:9153/metrics | grep dns_request_duration_seconds_bucket` 確認 bucket 邊界。

## 2. 規格更新：Measure DNS request processing duration

- [x] [P] 2.1 確認 `openspec/changes/dns-latency-histogram-buckets/specs/prometheus-metrics/spec.md` 中「Measure DNS request processing duration」requirement 已正確列出新 bucket 值，且三個 scenario（Query duration is recorded、Sub-millisecond query falls in 100µs–1ms buckets、Metrics endpoint exposes correct bucket boundaries）均以 SHALL/WHEN/THEN 格式描述，符合 delta spec 規格。驗證：`spectra analyze dns-latency-histogram-buckets` 無 Critical/Warning；`spectra validate "dns-latency-histogram-buckets"` 通過。
