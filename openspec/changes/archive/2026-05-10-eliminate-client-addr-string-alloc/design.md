## Context

`internal/server/handler.go` 的 `addrFromRemote` 是 query hot path 上的 client IP 抽取點。當前實作（line 526-540）：

```go
func addrFromRemote(w dns.ResponseWriter) (netip.Addr, error) {
    addr := w.RemoteAddr()
    if addr == nil { return netip.Addr{}, fmt.Errorf("nil remote addr") }
    host, _, err := net.SplitHostPort(addr.String())
    if err != nil { ... }
    ip, parseErr := netip.ParseAddr(host)
    if parseErr != nil { ... }
    return ip, nil
}
```

每筆 NOERROR query 跑 `handler.go:118` ServeDNS 一次、handleBackupQuery 條件再 `handler.go:444` 一次。30k QPS 下：

- `addr.String()` → `"1.2.3.4:5678"`：string header + bytes alloc
- `SplitHostPort(s)` → host substring: 1 alloc
- `ParseAddr(host)` → bytes parsing: CPU + 內部短命物件
- 每秒約 **90k allocs**，全為短命

`miekg/dns@v1.1.72` 的 `response.RemoteAddr()`（server.go:809-820）回傳的 concrete type：

| Path | concrete | 來源 |
|---|---|---|
| UDP（`dns.Server.PacketConn` 為 `*net.UDPConn`） | `*net.UDPAddr` | server.go:813 → SessionUDP.raddr → udp.go:36-37 |
| TCP（`dns.Server.Listener`） | `*net.TCPAddr` | server.go:817 → tcp.RemoteAddr() |
| 通用 PacketConn（pcSession） | 任意 net.Addr | server.go:814-815 — **本專案 unreachable** |

ShadowDNS 透過 `net.ListenPacket("udp", addr)`（`internal/server/listener.go:67`）取得 listener。`net.ListenPacket` 對 "udp" network 必回 `*net.UDPConn`（Go std lib 文件保證）。miekg `serveUDP` 在 server.go:507 做 `lUDP, isUDP := l.(*net.UDPConn)`，命中 `isUDP=true` → `readUDP` 路徑 → `udpSession` 必設。`pcSession` 路徑只在開發者繞過 `Server.PacketConn` 直接傳泛 `net.PacketConn` 才會走，本專案未使用。

PoC 結果（`net.ListenPacket("udp", "127.0.0.1:0")` + 真實 UDP query）：

```
[1] net.ListenPacket("udp", ...) concrete=*net.UDPConn isUDPConn=true
[2] RemoteAddr() concrete=*net.UDPAddr
[3] UDPAddr.IP len=4 bytes=[127 0 0 1] Zone=""
[4] AddrFromSlice naive=127.0.0.1 Is4=true Is4In6=false ; Unmap=127.0.0.1 Is4=true
[5] naive == 4byte-v4? true ; unmapped == 4byte-v4? true
[6] CIDR(127/8) contains naive=true unmapped=true
[7] OLD path == NEW (with Unmap): true
```

`internal/view/matcher.go` ruleMatches 的 IPRule 用 `==`、CIDRRule 用 `Prefix.Contains`（`internal/view/netmatch.go:11, 16`），兩者對 `Addr` 內部 v4 vs v4-mapped 表示敏感。`Unmap()` canonicalize 後與舊路徑 byte-equivalent。

ns2 production 確認全 v4 socket：`198.18.0.8:53`、`127.0.0.1:53`、`127.0.0.54:53`。無 `[::]:53` 雙棧，無 IPv6 link-local 路徑。

## Goals / Non-Goals

**Goals:**

- 消除 `addrFromRemote` hot path 上每 query 3 個字串 alloc（`addr.String()` + `SplitHostPort` host substring + `ParseAddr` 內部）。
- 對 caller 端（`handler.go:118` ServeDNS、`handler.go:444` handleBackupQuery）byte-equivalent — 相同 query 產生相同 `(netip.Addr, error)`。
- `addrFromRemote` 簽章不變。
- 為 type-switch fast path 與 default fallback 各加單元測試。

**Non-Goals:**

- 不動 `addrFromRemote` 對外簽章。
- 不動 `internal/server/handler.go:65, 75, 97, 121, 447` 的 `w.RemoteAddr().String()` log call — 全在 warn/error 分支，非 hot path。
- 不動 `internal/view/matcher.go` 與 `internal/view/netmatch.go`。
- 不處理 IPv6 link-local zone — 接受新路徑丟棄 zone 的行為差異（production 不收 link-local query）。
- 不順便處理 plan §4 Tier B 的 B2 / B4。
- 不評估 `miekg/dns/v2` 遷移。

## Decisions

### 決策 1：Type switch 而非 if-else type assert

**Choice**：`switch a := w.RemoteAddr().(type)` 含 `*net.UDPAddr` 與 `*net.TCPAddr` 兩 arm，default arm 走原字串路徑。

**Rationale**：
- TCP 路徑（AXFR 用 TCP listener）也吃到 `addrFromRemote`，必須涵蓋。
- 兩個 case body 結構相同（`netip.AddrFromSlice(.IP).Unmap()`），但 type 不同；type switch 比兩個 `if ok` 連寫更清楚、更易讀。
- default arm 保留作為 audit fallback：未知 stub 型別走原 string 路徑仍正確，永不 panic。

**Alternatives**：
- 只蓋 UDP（拒絕 — TCP path 仍走慢）。
- 取消 default arm 直接 `panic`（拒絕 — 違反 audit discipline「never panic on input」）。

### 決策 2：必須 `.Unmap()`

**Choice**：對 UDP / TCP 兩 arm 結果都呼叫 `Unmap()`。

**Rationale**：
- `*net.UDPAddr.IP` 與 `*net.TCPAddr.IP` 為 `net.IP`（`[]byte`），長度可能 4（IPv4）或 16（IPv6 / v4-mapped）。
- `netip.AddrFromSlice([]byte)` 對 16-byte v4-mapped（前 10 byte 0、bytes 10-11 為 0xFF）會構造 16-byte v6 form `Addr`，`Is4()=false`。
- 舊路徑 `String()` 對 v4-mapped 會印 `"a.b.c.d"`、`ParseAddr` 拿到 4-byte v4 form `Addr`。
- view matcher 對 `Addr` 用 `==`（matchIP）與 `Prefix.Contains`（matchCIDR），兩者皆 byte-sensitive — v4 與 v4-mapped 不等價。
- `Unmap()` 對 4-byte 是 no-op、對 v4-mapped canonicalize 為 4-byte，永遠等價舊路徑（PoC 確認）。

**Alternatives**：
- 不加 `Unmap`（拒絕 — Linux dual-stack socket 場景下 IP 可能為 16-byte v4-mapped，會引發靜默 view miss）。
- 用 `.Is4In6() ? .Unmap() : <as-is>`（拒絕 — `Unmap()` 內建此判斷，多寫一層 conditional 沒收益）。

### 決策 3：`AddrFromSlice` 的 `ok` bool 必須檢查

**Choice**：

```go
ip, ok := netip.AddrFromSlice(a.IP)
if !ok {
    return netip.Addr{}, fmt.Errorf("invalid UDP IP slice length %d", len(a.IP))
}
```

**Rationale**：
- `netip.AddrFromSlice` 對非 4 / 16 byte slice 回 `(zero Addr, false)`。
- `*net.UDPAddr.IP` 在標準 Go runtime 下永遠 4 / 16 bytes，但檢查是 audit discipline — boundary error 應顯式而非靜默。
- 失敗時回 `(netip.Addr{}, error)`，與舊路徑 boundary error 行為一致，caller 端走 REFUSED 分支（`handler.go:120-126`）。

**Alternatives**：
- 用 `_` 丟掉 ok（拒絕 — `make lint`（`exhaustruct` / `errcheck`-class）會吵；audit discipline 要求 boundary 顯式）。

### 決策 4：default arm 保留完整原 string 路徑

**Choice**：default arm 完整保留 `SplitHostPort + ParseAddr` 邏輯。

**Rationale**：
- production 100% type-switch fast path 命中，default arm 為 dead code in production but live in tests / future stubs。
- 拷貝邏輯而非抽取為 helper：避免增加新 helper function 到外部 package；20 行內聯 vs 多一個 function 表面，可讀性持平。

**Alternatives**：
- 抽 helper `addrFromRemoteSlow`（拒絕 — 增加表面，無收益）。

### 決策 5：error path log 不改

**Choice**：`handler.go:65, 75, 97, 121, 447` 的 `w.RemoteAddr().String()` 不動。

**Rationale**：
- 這 5 處全在 warn/error log 句子內（dns.OpcodeMessage 非 Query、parse 失敗、view 不命中等分支），non-hot path（NOERROR 91% 流量不 fire）。
- 改它們無 QPS 收益、徒增 blast radius。
- log 用 `String()` 會帶 port 與 zone 資訊，對 debug 反而有用。

**Alternatives**：
- 順便改成 `addrFromRemote` 的結果（拒絕 — 該函式可能已 error 才走 log，無 ip 可用；且 log 帶 port 是 debug feature）。

## Implementation Contract

### 觀察行為（必須與舊路徑 byte-equivalent）

| 輸入 | 舊行為 | 新行為（必須相同） |
|---|---|---|
| `w.RemoteAddr() == nil` | `(zero Addr, error("nil remote addr"))` | 相同 |
| UDP 4-byte v4（`*net.UDPAddr.IP=[1,2,3,4]`，Zone=""）| `Addr 1.2.3.4 Is4=true` | 相同（`AddrFromSlice → Unmap` no-op）|
| UDP 16-byte v4-mapped（`*net.UDPAddr.IP` 0...0 ff ff a b c d）| `Addr a.b.c.d Is4=true`（`String()` 印 `"a.b.c.d"`，ParseAddr 給 4-byte）| 相同（`Unmap` canonicalize 為 4-byte）|
| UDP 16-byte 純 v6 | `Addr <v6> Is6=true` | 相同 |
| TCP path（`*net.TCPAddr`）| 同 UDP 邏輯 | 相同 |
| 未知 stub（測試或未來 PacketConn 變體）| 原字串路徑 | 由 default arm 接住，邏輯不變 |
| IPv6 link-local（`Zone="eth0"`）| 帶 zone 的 Addr | **不帶 zone**（記錄在 Non-Goals）|

### 介面

- `addrFromRemote(w dns.ResponseWriter) (netip.Addr, error)` — 簽章不變。
- 兩個 caller `handler.go:118`（ServeDNS）與 `handler.go:444`（handleBackupQuery）不需改動。
- 兩個測試 stub `internal/metrics/writer_test.go:fakeResponseWriter` 與 `internal/server/handler_test.go:recordingWriter` 已用 `*net.UDPAddr`，命中新 fast path，不需改動。

### 失敗模式

- `addr == nil` → `(Addr{}, error)`，caller 走 REFUSED。
- UDP/TCP arm `AddrFromSlice` 回 `ok=false` → `(Addr{}, error("invalid {UDP|TCP} IP slice length N"))`，caller 走 REFUSED。
- default arm `SplitHostPort` 或 `ParseAddr` error → 與舊路徑相同 error 訊息格式。

### 驗收條件

- `make test`（含 race detector）全綠，包含新增的 type-switch 單元測試。
- `make lint`（`golangci-lint`）clean。
- `make smoke`（`shadowdns --dry-run`）clean。
- 新增單元測試覆蓋：(1) `*net.UDPAddr` with 4-byte IP（hot path）、(2) `*net.UDPAddr` with 16-byte v4-mapped（Unmap 必要性）、(3) `*net.TCPAddr` with 4-byte IP、(4) `addr == nil` 早回、(5) default arm fallback（用 `*net.IPAddr` 之類非 UDP/TCP stub）。
- 部署到 `bench-ns2`、從 `bench-ns1` 跑 dnspyre `-c 100 -d 3m` CNAME 工作負載 + 30s pprof：(a) NXDOMAIN/REFUSED rate 與 baseline 變化 ≤ 0.05pp（行為等價驗證）；(b) `addr.String` / `SplitHostPort` / `netip.ParseAddr` 在 pprof flame graph 上消失或顯著下降；(c) QPS 變化記錄（預期 +1-2%，亦可能 sub-threshold）。

### 範圍邊界

**In scope**：
- internal/server/handler.go（addrFromRemote 函式 body）
- internal/server/handler_test.go（新增單元測試）

**Out of scope**：
- internal/server/handler.go 的其他函式
- internal/server/listener.go、internal/view/、internal/metrics/
- 任何 capability 行為變化

## Risks / Trade-offs

- **Linux UDP socket 給 16-byte v4-mapped IP** → 透過 `Unmap()` 處理。PoC 在 macOS dev box 確認 4-byte input 下 Unmap 為 no-op；Linux 行為若不同（給 16-byte），`Unmap` 仍正確 canonicalize。新增的 v4-mapped 測試 case 會 catch 此差異。
- **未知 ResponseWriter 實作走 default arm** → 行為與舊路徑相同（保留原 string 邏輯）；無 panic 風險。
- **IPv6 zone 訊息丟失** → production 不收 link-local query；test env 也不用。已標 Non-Goal；若未來真有需求，補一個 `*net.UDPAddr.Zone` 處理 arm。
- **效能不達預期 +1-2%** → 即便 sub-threshold，code 簡化（拿掉 3 個 string formatting / parsing call）+ alloc 數量減少（GC pressure 降低）有獨立價值。比照 `eliminate-zone-origin-concat` 與 `migrate-geoip-to-mmdb-v2` 的「branch-c outcome 仍 commit」處理。
- **fmt.Errorf 在 fast path 上仍 alloc** → 只在 boundary error 路徑（`AddrFromSlice ok=false`）才 alloc，hot path 不 fire。可接受。
