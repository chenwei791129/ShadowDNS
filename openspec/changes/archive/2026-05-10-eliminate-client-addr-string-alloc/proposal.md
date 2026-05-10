## Summary

把 `internal/server/handler.go` 的 `addrFromRemote` 從「`addr.String()` → `SplitHostPort` → `netip.ParseAddr`」字串往返路徑，改為對 `dns.ResponseWriter.RemoteAddr()` 做 type assertion 直取 `*net.UDPAddr` / `*net.TCPAddr` 的 `.IP` 欄位（`net.IP`），用 `netip.AddrFromSlice(...).Unmap()` 直接構造 `netip.Addr`。

## Motivation

`addrFromRemote` 在 query hot path 上每筆 query 跑一次（`handler.go:118` ServeDNS 主路徑、`handler.go:444` handleBackupQuery），目前實作（`handler.go:526-540`）每 call 至少 3 個 string alloc：

1. `addr.String()` — 把 `*net.UDPAddr` 格式化為 `"1.2.3.4:5678"`（string header + bytes）
2. `net.SplitHostPort(s)` — 切出 host substring
3. `netip.ParseAddr(host)` — 字串 parse 回 bytes

在 dnspyre 30k QPS × 100 concurrency 的工作負載下，這 ~90k allocs/sec 全是短命物件，貢獻 GC pressure。但更直接的成本是 per-call 字串 formatting + parsing 的 CPU。

`miekg/dns@v1.1.72` 的 `response.RemoteAddr()`（server.go:809-820）UDP 路徑必回 `*net.UDPAddr`、TCP 路徑必回 `*net.TCPAddr`（兩個都是 concrete struct，含 `.IP net.IP` 欄位）。直接 type assert 即可拿 `[]byte`，用 `netip.AddrFromSlice` 一步構造 `netip.Addr`，跳過所有字串往返。

PoC 驗證（`net.ListenPacket("udp", "127.0.0.1:0")` + 真實 UDP query）：
- `RemoteAddr()` concrete type = `*net.UDPAddr` ✓
- `*net.UDPAddr.IP` 在 v4 socket 下為 4 bytes
- `AddrFromSlice([]byte) → Addr.Unmap()` 結果與舊路徑 `String() → ParseAddr` byte-equivalent
- `matchIP`（`==`）與 `matchCIDR`（`Prefix.Contains`）對 4-byte 與 4-byte-Unmap 行為一致

預估 +1-2% QPS（相對保守，依最近三次優化的「實測 < 預估」規律校準；plan §4 B1 原寫 +3-5%）。每筆 query 都受惠，不靠 client repeat。

## Proposed Solution

改寫 `addrFromRemote` 內部實作為 type-switch fast path，舊字串路徑保留為 default arm 的 fallback：

```go
func addrFromRemote(w dns.ResponseWriter) (netip.Addr, error) {
    addr := w.RemoteAddr()
    if addr == nil {
        return netip.Addr{}, fmt.Errorf("nil remote addr")
    }
    switch a := addr.(type) {
    case *net.UDPAddr:
        ip, ok := netip.AddrFromSlice(a.IP)
        if !ok {
            return netip.Addr{}, fmt.Errorf("invalid UDP IP slice length %d", len(a.IP))
        }
        return ip.Unmap(), nil
    case *net.TCPAddr:
        ip, ok := netip.AddrFromSlice(a.IP)
        if !ok {
            return netip.Addr{}, fmt.Errorf("invalid TCP IP slice length %d", len(a.IP))
        }
        return ip.Unmap(), nil
    default:
        // Fallback: 未知 net.Addr 實作（測試 stub、未來 packet conn 變體）走原字串路徑。
        host, _, err := net.SplitHostPort(addr.String())
        if err != nil {
            return netip.Addr{}, fmt.Errorf("parsing remote addr %q: %w", addr.String(), err)
        }
        ip, parseErr := netip.ParseAddr(host)
        if parseErr != nil {
            return netip.Addr{}, fmt.Errorf("parsing IP %q: %w", host, parseErr)
        }
        return ip, nil
    }
}
```

對外簽章 `(dns.ResponseWriter) (netip.Addr, error)` 不變。callers `handler.go:118` 與 `handler.go:444` 透明。

## Non-Goals

- 不動 `addrFromRemote` 對外簽章。
- 不動 `internal/server/handler.go:65, 75, 97, 121, 447` 的 `w.RemoteAddr().String()` 呼叫 — 那 5 處全在 warn/error log 分支，非 hot path（NOERROR 91% 流量不 fire）；改它們只增加 blast radius、無 QPS 收益。
- 不動 `internal/view/matcher.go`（`Matcher.Resolve` / `ruleMatches`）— 上游 `clientIP netip.Addr` 透過 `Unmap()` 已 byte-equivalent 於舊路徑。
- 不動 IPv6 link-local zone 行為。新路徑透過 `*net.UDPAddr.IP` 取 IP，丟掉 `.Zone string` 欄位；舊路徑經 `String()` 帶 zone 走 `ParseAddr`。production 環境（authoritative DNS）不收 link-local query；接受此行為差異並記錄於 design.md。
- 不順便加 `*net.IPAddr` arm — `dns.ResponseWriter` 在 UDP/TCP 兩條路徑外不會回此型別，YAGNI。
- 不順便處理 plan §4 Tier B 的 B2 (Prometheus counter pre-resolve)、B4 (LookupKey conditional ToLower)。

## Alternatives Considered

- **完全移除 fallback default arm**：拒絕。production 命中率 100% type-switch fast path，但 fakeResponseWriter / recordingWriter 等測試 stub 已用 `*net.UDPAddr`，未來若有 stub 故意傳怪型別 default arm 才不會 panic。保留 fallback 是 audit discipline 而非 performance 妥協。
- **全部改 `addr.(*net.UDPAddr).IP` 不加 `Unmap()`**：拒絕。Linux 行為依平台與 socket 設定可能給 16-byte v4-mapped IP，會讓 `view.matchIP`（`==`）與 `view.matchCIDR`（`Prefix.Contains`）出現靜默 miss。`Unmap()` 對 4-byte 是 no-op、對 16-byte v4-mapped 正確 canonicalize，永遠安全（PoC 確認）。
- **新增 `netip.Addr` 的 ResponseWriter wrapper**：拒絕。需動 `dns.ResponseWriter` interface 邊界；blast radius 大、收益相同。
- **遷移到 `miekg/dns/v2`**：plan §4 C3，獨立追蹤；v2 仍 pre-1.0 (2026-05) breaking changes 期間，不混在此 change。

## Impact

- Affected specs: 無觀察行為變化（純內部優化；對外 view-matcher 與 dns-server capability 行為要求 byte-equivalent）。
- Affected code:
  - Modified: internal/server/handler.go
  - Modified: internal/server/handler_test.go (補一個直接 cover type-switch fast-path 與 default fallback 的單元測試)
