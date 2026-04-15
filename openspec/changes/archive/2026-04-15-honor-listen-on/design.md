## Context

ShadowDNS 目前的 listener 層（[internal/server/listener.go](../../../internal/server/listener.go)）對 UDP 使用 `net.ListenPacket("udp", listenAddr)`、對 TCP 使用 `net.Listen("tcp", listenAddr)`，兩者皆以單一位址字串為目標。`cmd/shadowdns/main.go` 把 `--listen :53`（展開為 `0.0.0.0:53`）直接餵給 `srv.Bind()`。在 Ubuntu 24.04 / Debian 12 等預設啟用 systemd-resolved 的環境，stub listener 已經佔住 `127.0.0.53:53` 與 `127.0.0.54:53`，`0.0.0.0:53` 的 wildcard bind 因 kernel 的 overlap 檢查回 `EADDRINUSE`，shadowdns 直接啟動失敗。BIND9 不會踩這個地雷，因為它讀 `named.conf` 的 `listen-on` 指令、對每個解析出來的具體位址各開 socket，個別 bind 失敗不中斷其他 listener。

config-loader 已經把 `options.listen-on { ... }` 解析進 `config.OptionsBlock.ListenOn []string`（[internal/config/options.go:14](../../../internal/config/options.go#L14)），但這個欄位目前在 dns-server 端完全沒有被消費 — 是個已建好但未接通的管道。本設計把管道接通，並同時改變 listener 的物理形態（單 socket → 多 socket）。

Stakeholders：

- Ops（在 Ubuntu/Debian 上部署的人）— 啟動失敗的第一線受害者
- 從 BIND 遷移的使用者 — 期待 `named.conf` 的語意被 honor
- 開發者 — 內部 listener API（`Server.Bind`、`Server.UDPAddr`、`Server.TCPAddr`）會變動，測試需要更新

## Goals / Non-Goals

**Goals:**

- `named.conf` 的 `listen-on { ... }` IPv4 指令（`any` / 顯式位址列表 / `none`）被 dns-server 實際使用
- 個別位址 bind 失敗不致命；能綁多少就綁多少，全部失敗才 fatal
- `--listen` CLI flag 保留 override 能力（用於測試、特殊部署）
- 啟動 log 清楚顯示每個 listener 實際綁在哪個 IP 上
- 既有的 listener API (`UDPAddr()` / `TCPAddr()`) 使用點不大幅重寫 — 維持 single-address 語意給測試，另外暴露 slice 版本

**Non-Goals:**

- IPv6（`listen-on-v6`）消費 — parser 照常輸出 `ListenOnV6`，server 層這次不讀
- BIND 的 `port N` 語法、address match list 進階語法（`!addr`、ACL 參照）、`interface` keyword
- 動態介面 rescan（`interface-interval`）— 靠現有 SIGHUP reload 路徑處理變動
- 針對 SO_REUSEPORT 的多 process load balancing — 與本題無關

## Decisions

### Listen-on 來源與優先順序

```
┌──────────────────────────────────────────────────────────────┐
│ 位址來源解析（在 cmd/shadowdns/main.go 啟動或 reload 時）    │
│                                                              │
│  --listen 有顯式 host component？（e.g. "127.0.0.1:5353"）   │
│    ├─ 是 → [override] 只用 --listen 單一位址，忽略 listen-on │
│    └─ 否（":PORT" 形式，如 ":53"、":5353"、":0"）↓           │
│                                                              │
│  named.conf 的 options.ListenOn 非空？                       │
│    ├─ 是 → 用 listen-on 展開的 IP × --listen 的 port         │
│    └─ 否 → [fallback] 用 "any" 展開 × --listen 的 port       │
└──────────────────────────────────────────────────────────────┘
```

`--listen` 從「bind 目標」改為「override hint + port hint」。**若使用者顯式傳入 host component，`listen-on` 被完全忽略**（避免「我明明指定了還被 config 覆寫」的困惑）。`:PORT` 形式視為「host 未指定、port 指定」，此時 `listen-on` 接手 host（IP），port 從 `--listen` 繼承。Port 部分：`listen-on` 無 port 資訊（本 change 不支援 `port N`），一律採用 `--listen` 所帶的 port（預設 53）。

這個規則讓 `--listen :0`（test 用 ephemeral port）+ `listen-on { 127.0.0.1; }` 能正確綁在 `127.0.0.1:0`，同時 `--listen 127.0.0.1:0` 也仍然 override（整個位址由 flag 決定）。

Alternative：完全用 `listen-on`，丟棄 `--listen`。**拒絕原因**：既有測試、整合測試、故障排查都依賴 `--listen 127.0.0.1:0` 指定 ephemeral port，移除會大幅增加測試改動面。保留為 override 是成本最低的相容策略。

### `any` 展開規則

`listen-on { any; };`（或 `listen-on` 未指定時，視為隱含 `any`）展開為：**呼叫 `net.InterfaceAddrs()`、過濾 IPv4、包含 loopback（含 `127.0.0.x` aliases）、過濾掉 link-local (`169.254.0.0/16`) 與 IPv6**。

關鍵細節：
- **包含 loopback aliases**：systemd-resolved 的 `127.0.0.53` 會出現在 enumeration 中、會嘗試 bind、會失敗、會被 graceful-skip。這是 BIND 的行為，我們模仿。
- **過濾 link-local**：`169.254.x.x` 對 DNS server 通常無意義且會帶來雜訊
- **過濾 IPv6**：本 change 範圍內不處理；`net.InterfaceAddrs()` 回傳的 `*net.IPNet` 用 `ip.To4() != nil` 判斷

Alternative：模仿 BIND 用 `getifaddrs(3)` 直接拿 interface name + address，可過濾 `down` 介面。**拒絕原因**：Go 的 `net.InterfaceAddrs()` 已經只回傳 `up` 介面的位址，夠用；引入 C interop 不划算。

### 部分 bind 失敗的容錯語意

```
addrs := resolveListenAddresses(opts, cfg)   // 展開後的位址清單
bound := []boundListener{}
for _, addr := range addrs {
    udp, tcp, err := bindPair(addr)
    if err != nil {
        logger.Warn("listener bind failed, skipping",
            "addr", addr, "err", err)
        continue
    }
    bound = append(bound, boundListener{udp, tcp, addr})
    logger.Info("listener bound", "addr", addr)
}
if len(bound) == 0 {
    return fmt.Errorf("no listeners bound (tried %d addresses)", len(addrs))
}
```

**UDP 與 TCP 是同一個位址的 atomic pair**：若 UDP 成功但 TCP 失敗（或反之），視為該位址整體失敗，已開的那一半需要 Close 掉再 continue。理由：RFC 7766 要求 TCP fallback，單 UDP 可服務但行為不完整，會造成診斷噩夢。

Alternative：UDP / TCP 獨立計數。**拒絕原因**：見上，半服務的位址比完全沒綁更難除錯。

### `Server` struct 改為持有 listener slice

```go
type Server struct {
    // ... existing fields ...

    // listeners 儲存所有已綁的 UDP/TCP 成對 listener。
    // 由 Bind* 方法填入，Serve 對每個 pair 開兩個 goroutine（UDP + TCP）。
    listeners []listenerPair
}

type listenerPair struct {
    addr string        // 人類可讀的 "10.0.0.1:53"
    udp  *dns.Server   // PacketConn 已綁
    tcp  *dns.Server   // Listener 已綁
}
```

API 變動：

| 現行 API | 變更後 |
|---------|--------|
| `Bind(listenAddr string) error` | 保留但標為 legacy，行為 = 單一位址綁一對 listener |
| （新增）`BindMany(addrs []string) error` | 主要入口；per-address bind，部分失敗容錯 |
| `UDPAddr() net.Addr` | 回傳**第一個**成功綁的 UDP 位址（for test 相容）；全失敗時 nil |
| `TCPAddr() net.Addr` | 同上 |
| （新增）`UDPAddrs() []net.Addr` | 回傳所有成功綁的 UDP 位址 |
| （新增）`TCPAddrs() []net.Addr` | 同上 |
| `Serve(ctx)` | 改為對 `listeners` 中每個 pair 各開 2 goroutine；任一 goroutine 回錯誤仍觸發整體 shutdown |
| `Start(ctx, listenAddr string) error` | 保留，內部呼叫 `Bind` + `Serve` |

`Bind(addr)` 仍存在是為了測試相容 — 所有既有測試用 `ListenAddr: "127.0.0.1:0"` 的路徑會走 `Bind` 單位址路徑，不需改。`main.go` 切換用 `BindMany`。

Alternative：直接刪 `Bind` / `UDPAddr()` / `TCPAddr()`，全部改成 slice 版。**拒絕原因**：增加測試改動面而沒有收益；遷移路徑更陡。

### Port 解析與預設 port

`--listen` 可能的形式：`:53`（= `0.0.0.0:53`）、`1.2.3.4:53`、`1.2.3.4:0`（ephemeral port for test）、`[::]:53`（v6，本 change 不處理）。`listen-on` token 只是裸 IP（或 `any` / `none`）。

解析步驟：
1. 用 `net.SplitHostPort(opts.ListenAddr)` 拿出 `host, port`
2. Override 分支：直接用 `opts.ListenAddr` 當 bind 目標
3. listen-on 分支：對每個展開後的 IP 組合 `net.JoinHostPort(ip, port)`

這保留了測試用 ephemeral port 的能力：`--listen 127.0.0.1:0` 永遠走 override 分支。

### SIGHUP reload 的處理

既有 SIGHUP reload 目前不重綁 listener。本次**保持不變** — reload 只會重新載 zones/aliases，不會因 `listen-on` 改變而重開 socket。理由：重綁 listener 會有 downtime 風險；使用者若改了 `listen-on` 可接受 `systemctl restart`。這個決定寫進 spec 當明示 non-requirement，避免未來有人期待 reload 能換綁。

## Risks / Trade-offs

- **Risk**: systemd-resolved 的 `127.0.0.53:53` 仍會出現在 WARN log 裡，使用者可能以為是問題
  → **Mitigation**: WARN 訊息明確寫「skipping, continuing with other addresses」；若該位址是 `127.0.0.53` 或 `127.0.0.54` 且 error 是 `EADDRINUSE`，可在 hint 裡加一句「this is likely systemd-resolved; safe to ignore or set `DNSStubListener=no`」
- **Risk**: 新增網卡後不會自動 pick up，使用者可能覺得「BIND 有 interface-interval 你們卻沒有」
  → **Mitigation**: docs / release notes 明寫「變更網路介面後請 `systemctl reload shadowdns`」；未來另開 change 評估是否加 rescan
- **Risk**: 測試改動面：`UDPAddr()` / `TCPAddr()` 在 `axfr_test.go` / `helpers_test.go` / `server_test.go` 多處使用
  → **Mitigation**: 保留 single-addr 語意（回第一個成功），測試路徑不動；新增 slice 版給需要的新測試
- **Trade-off**: `--listen` 從「綁定目標」變成「override hint」在語意上微妙，文件必須寫清楚
  → **Mitigation**: `--listen` 的 flag help 文字更新為 "override named.conf listen-on (default: use listen-on from named.conf)"；release notes 強調這一點
- **Trade-off**: Log 量從 1 筆 `"shadowdns ready" listen=:53` 變成 N+1 筆（每 IP 一筆 INFO + ready），大型機器可能 10+ 筆
  → **Mitigation**: 可接受；DEBUG 級別分離不值得複雜化

## Migration Plan

本 change 是單一 release 內的原子變更，無階段性：

1. Release notes 列出 BREAKING 行為（預設 bind 行為改變；新增網卡不自動 pick up）
2. 部署升級前 ops 檢查 `named.conf` 的 `listen-on`（若無則隱含 `any`，行為與之前最接近）
3. 若啟動後發現某些位址沒綁（例如 IP 在 `net.InterfaceAddrs()` 看不到），用 `--listen` override 暫時救場
4. Rollback：降回前一版 `.deb` 即可，`named.conf` 格式向前相容

## Open Questions

- 是否要對 `127.0.0.53` / `127.0.0.54` 的 `EADDRINUSE` 特別加 systemd-resolved hint？傾向：**加**，成本低且對使用者很有幫助，但可以留到 apply 階段視 log 清晰度再決定
