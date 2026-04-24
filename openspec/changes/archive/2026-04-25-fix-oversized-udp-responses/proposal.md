## Why

實測 `internal/server/handler.go` 的 `replyWithAnswer` 在高 RR 數量下同時踩到兩個問題：

1. **協定違規**：client 宣告 EDNS0 buffer = 4096 bytes，ShadowDNS 卻回應 6021 bytes UDP packet（TC=0）。`m.Truncate(4096)` 實際用 miekg/dns 的 `Len()` 估算，未反映 `Pack()` 的真實 wire size，導致服務超出 client 的 advertised budget。違反 RFC 6891 對 requestor's UDP payload size 的語意。
2. **過大 response 觸發 IP fragmentation**：沒有套用 DNS name compression，48 筆同 owner name TXT 回應達 4029 bytes。`> 1500 MTU` 會在網際網路路徑上 fragment，而許多 middleware 會丟 DNS UDP fragments，造成 ACME DNS-01 批量簽發失敗（已在下游 ACME client 的 shared-bucket 部署生產重現；完整調查與壓測結果見 `.local/report/20260424-shadowdns-shared-bucket-investigation.md`，壓測驗證程式見 `test/integration/stress_shared_bucket_test.go`）。

兩個問題在同一個 `replyWithAnswer` 路徑，合併修正。

## What Changes

- `internal/server/handler.go:replyWithAnswer` 在 `Pack` / `WriteMsg` 前設 `m.Compress = true`，啟用 RFC 1035 §4.1.4 name compression。
- `replyWithAnswer` 的截斷邏輯改為**以 `Pack()` 後的 wire size 為準**嚴格不超過 `udpMaxSize(req)`，仍由 miekg/dns 負責 dropping RRs 與設定 TC=1，但改以實際序列化大小檢驗上限。
- 新增 dns-server requirement Scenarios 確保：(a) UDP response 不得超過 client 宣告的 EDNS0 buffer；(b) 同 owner name 多 RR 回應使用 compression。
- Benchmark 已確認：n ≥ 6 RRs 時 compression 速度持平或更快；n=48 shared owner 場景 CPU 快 ~16%、wire size 省 ~34%。

## Non-Goals

- 不改任何 DNS 查詢/路由/view-matching 行為。
- 不動 TCP 回應路徑（TCP 本就不受 EDNS0 buffer 限制）。
- 不引入運行時可調整的 compression 開關；一律啟用。
- 不改 `udpMaxSize` 對未宣告 EDNS0 client 的預設 512 bytes 行為。
- 不處理 fragmentation 的 client-side rescue（TCP fallback 機制既已存在）。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `dns-server`: 現有 "Response exceeding UDP limit sets TC flag" Scenario 的語意需收緊，改為以 `Pack()` 實際 wire size 為判準，並新增 compression 啟用與 EDNS0 budget 嚴格符合的 Scenarios。

## Impact

- Affected specs:
  - Modified: `openspec/specs/dns-server/spec.md`
- Affected code:
  - Modified: `internal/server/handler.go`（`replyWithAnswer` 啟用 compression + 嚴格 truncate）
  - Modified: `internal/server/handler_test.go`（若存在）或 `test/integration/` 下新增整合測試覆蓋新 Scenarios
- Benchmarks: Apple M4 實測 n=48 shared-owner TXT, compression ON 時 Pack 3391 ns/op（vs OFF 4047 ns/op），ops/sec +16%，wire size 2735 B（vs 4029 B）。
