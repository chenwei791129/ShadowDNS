## Context

`internal/server/handler.go:replyWithAnswer` 是 ShadowDNS 所有成功 DNS 回應共用的輸出路徑（authoritative 回答會走這裡）。目前實作：

```go
m := new(dns.Msg)
m.SetReply(req)
...
m.Answer = answer
if dnsutil.IsUDP(w) {
    maxSize := udpMaxSize(req)
    m.Truncate(maxSize)
}
_ = w.WriteMsg(m)
```

miekg/dns 的 `Msg.Truncate(size)` 內部以 `m.Len()` 估算序列化長度決定是否要丟 RR、設 TC。但 `Len()` **不等於** `Pack()` 的實際輸出大小，尤其在同 owner name 多 RR 場景、以及 `Compress` flag 未設為 true 時兩者偏差更大。實測（48 筆共 owner name TXT，n=72 時）：

- `Pack()` 大小 = 6021 bytes（實際 wire）
- `Truncate(4096)` 觸發點 = n=73（即使 n=72 已超過 4096 bytes wire，TC 仍為 0）
- Client 宣告 EDNS0 buffer=4096 → 收到 6021 bytes UDP packet（違反 RFC 6891）

另外 `replyWithAnswer` 沒有啟用 name compression，48 筆同 owner name TXT 的 wire 達 4029 bytes（壓縮後 2735 bytes，省 34%）。Benchmark 顯示 n ≥ 6 時 compression 速度持平或更快；n=48 時 Pack CPU 快 16%。

此 change 修正這兩個問題。

下游動機：上游 ACME client 使用 shared-bucket CNAME delegation，`parallelism=8 × 6 SANs = 48` 筆 TXT 集中在單一 `_acme-challenge.<shared-bucket>` FQDN。4029 bytes UDP 回應在 internet 路徑（MTU 1500）會 IP fragment，middleware 丟 fragment 導致 Let's Encrypt multi-perspective validation 失敗。完整調查與壓測結果見本 repo 的 `.local/report/20260424-shadowdns-shared-bucket-investigation.md`；壓測驗證程式見 `test/integration/stress_shared_bucket_test.go`。

## Goals / Non-Goals

**Goals:**

- `replyWithAnswer` 啟用 DNS name compression（`m.Compress = true`），所有 UDP/TCP 回應 wire size 縮小，同 owner name 場景尤其顯著。
- UDP 回應嚴格不超過 `udpMaxSize(req)`（client EDNS0 advertised buffer 或 512 bytes），以 `Pack()` 後的實際長度為判準。
- 新增 dns-server spec Scenarios 鎖定上述兩個行為。

**Non-Goals:**

- 不改變 `udpMaxSize(req)` 的決策邏輯（仍讀 OPT.UDPSize 或 fallback 512）。
- 不動 TCP 路徑（TCP response 受 2-byte length prefix 限制，64KB 上限，本就無 UDP 截斷問題）。
- 不碰 `replyRcode`、`negativeReply` 等非 `replyWithAnswer` 路徑（若也有相同問題，另開 change；目前 ephemeral / authoritative 多 RR 主要走 `replyWithAnswer`）。
- 不引入 runtime flag 控制 compression；compression 永遠開啟。
- 不處理 client 不支援 compression pointer decode 的 legacy edge cases（miekg/dns 的 compression 產生 RFC 1035 合規指標，所有合規 DNS client 都能 decode）。

## Decisions

### Decision: Enable DNS name compression unconditionally in `replyWithAnswer`

在 `m.SetReply(req)` 之後、`m.Truncate(...)` / `w.WriteMsg(m)` 之前加 `m.Compress = true`。

**Rationale**：RFC 1035 §4.1.4 compression 是 DNS 標準，所有主流 recursive / validator / client 都必須支援。Benchmark 證實 n ≥ 6 時 compression 更快或持平；shared-owner-name 場景（典型於 ephemeral TXT bucket）省 30-34% wire size，直接減少 fragmentation 觸發面。無 downside 除了 n=1 時多 ~70 ns/op（單次 response 不可感知）。

**Alternatives considered:**

- **Conditional compression**（只在 response > 某 threshold 時開）：程式碼複雜度增加、測試路徑 2 倍、benchmark 顯示 n=1 的額外成本微不足道。排除。
- **只在 UDP 路徑開**：TCP 回應也會進 AXFR + cross-zone queries，壓縮同樣有益且 zero-risk。開 UDP 專屬實屬人為拆分。排除。

### Decision: Strict UDP truncation based on `Pack()` wire size

將現行 `m.Truncate(maxSize)` 改為一個封裝函式 `truncateForUDP(m, budget)`，語意：

1. 先嘗試 `Pack()`，若 `len(packed) <= budget` 直接寫入。
2. 否則 while `len(packed) > budget && len(m.Answer) > 0`：丟掉 `m.Answer` 末筆、設 TC=1、重新 `Pack()` 驗證。
3. 若丟到 `m.Answer` 為空仍超出（極端情境，例如 question section 過大），維持 TC=1 並回 header-only 截斷 packet。

**Rationale**：miekg/dns 原生 `Truncate(size)` 用 `Len()` 估算，已被實測證實會放行超出 budget 的回應。以 `Pack()` 實際 wire size 迭代驗證，保證「輸出到 socket 的 bytes 永不超過 `udpMaxSize(req)`」，符合 RFC 6891 §6.2.5 對 requestor's advertised UDP payload size 的契約。

**Alternatives considered:**

- **改用 `Pack()` 的輸出大小調用原生 `Truncate`**：miekg/dns `Truncate` API 只接受 size、內部靠 `Len()`；調一次後再 pack 可能仍超標。必須自行迴圈驗證。
- **改用 miekg/dns 未釋出的 `TruncateByPackedSize`**（假想 API）：不存在。排除。
- **給 upstream miekg/dns 發 PR 修 `Truncate`**：路徑長、release 無法掌控；先在 ShadowDNS 本地修正，上游有需要再另議。

### Decision: Compression 開啟順序在 Truncate 之前

`m.Compress = true` 必須在第一次 `Pack()` 之前設好。新封裝 `truncateForUDP` 內每次 `Pack()` 都依 `m.Compress` 編碼，確保 "size budget check" 檢的是壓縮後的 wire size。

**Rationale**：若順序相反（先 truncate 再 compress），會丟掉「若壓縮後其實塞得下」的 RR，造成不必要的 TC=1、多觸發 TCP retry。順序固定下來讓行為可預期。

### Decision: 修改 dns-server spec 而非建立新 capability

既有 `dns-server` spec 已包含 "Response exceeding UDP limit sets TC flag" 的 Scenario（實作違反此 spec）。此 change 收緊該 Scenario 語意、新增 compression + strict-budget 相關 Scenarios 作為 delta，避免 spec 膨脹成獨立 `udp-response-sizing` capability。

**Rationale**：此行為屬 dns-server 職責的核心（"serve DNS queries"），不值得拆成獨立 capability。delta 修訂保留 trace history。

## Risks / Trade-offs

- `[Risk]` miekg/dns 版本升級時 `Pack()` 行為微變可能影響 truncate 迴圈的收斂特性（例如 OPT 編碼差異）。→ Mitigation：truncate 迴圈以 `len(m.Answer) > 0` 作守衛，無限大 question / authority section 不會無限迴圈；tests 覆蓋「n=0 answers + oversize header 情境 → TC=1 header-only packet」。
- `[Risk]` Compression 對某些舊實作的 DNS client（< 1990 年代）可能不支援 pointer decode。→ Mitigation：RFC 1035 是 1987 年發布，compression 是 DNS 基礎設施的一部分；ShadowDNS 已處在現代 ACME / recursor 生態，非目標 clients。
- `[Risk]` `truncateForUDP` 在極端 large RRset 下可能多次 Pack（O(n) RRs dropped → O(n) Pack calls）。→ Mitigation：每次 `Pack()` 在 n=100 TXT 上 ~8 µs；最壞情況 n 次 Pack 共 ~800 µs，仍 << 典型 DNS RTT。實務上只需 1-3 次即可收斂。測試要確認迴圈不失控。
- `[Trade-off]` `m.Compress = true` 在 n=1 response 慢 ~70 ns/op。→ 不可感知，接受。

## Migration Plan

此 change 無資料面變更、無 config 變更、無 API 變更。部署即生效。回滾策略：revert commit。無向後相容議題。
