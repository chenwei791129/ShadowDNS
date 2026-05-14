## Context

目前 NOTIFY 的 NS target 解析流程（[internal/transfer/notify.go:22-52](internal/transfer/notify.go#L22-L52) + [cmd/shadowdns/main.go:567-605](cmd/shadowdns/main.go#L567-L605)）：

1. `NotifyTargets()` 回傳 `[]string`，每筆為 FQDN 主機名
2. `dispatchNotifies()` 把 `target + ":53"` 直接傳給 `transfer.SendNOTIFY()`
3. `SendNOTIFY()` 呼叫 `dns.Exchange(msg, "host:53")`，`miekg/dns` 內部用 `net.Dial("udp", ...)`
4. `net.Dial` 把 host 交給 Go resolver → 系統 `/etc/resolv.conf`

權威 DNS 主機典型部署：僅對內網開放、防火牆阻擋對外 UDP:53 / 沒有設定遞迴 resolver。結果：**每個 NS target 都在系統解析階段就 timeout**，不論該 NS 的 A record 是否在自己的 zone 資料裡。

BIND9 的實作（RFC 1996 §3.3 & named 原始碼 `lib/ns/notify.c`）：優先從自己權威資料的 in-zone glue 取得 IP，沒有 glue 才會嘗試 out-of-zone 解析（而多數 production 設定根本不做 out-of-zone 解析）。

已載入的 zone 資料 `*zone.Zone` 結構中，`z.Records[name]` 是 `map[string]*qtypeStore`（單 qtype 內聯 / 多 qtype promote 雙態），對外查詢統一走 `z.Lookup(owner, qtype)` API，A / AAAA glue 查找是 O(1)。Owner key 依 RFC 4343 強制 lowercase（見 [internal/zone/zone.go:74-103](internal/zone/zone.go#L74-L103)），lookup 端需自行 `strings.ToLower(dns.Fqdn(host))` 折大小寫。所需資料「全部在手邊」。

## Goals / Non-Goals

**Goals:**

- NOTIFY target IP 解析優先從 in-zone glue 取得，不觸發系統 resolver
- 在只開放內網的部署上，NOTIFY 到**有內網 IP glue** 的 target 必須能實際送達
- 當 NS target 無 glue 時，行為**可預測**：明確 skip 並記錄原因，不沉默通過也不 timeout
- 多 IP glue（IPv4 + IPv6、或同族多 IP）對每個 IP 各發一次，提升可達性
- Log 新增 `source` 欄位，運維能立即分辨「發給 IP」vs 「因無 glue 跳過」

**Non-Goals:**

- 實作遞迴 resolver 或呼叫外部 resolver（會把原問題搬回來；若未來需要 out-of-zone NOTIFY target，交由 `also-notify` 顯式指定 IP 解決）
- 改變 NOTIFY 的重試策略、backoff、或整體 deadline
- 新增 NOTIFY 相關的 config directive（`also-notify`、`notify-source` 等）
- 跨 reload 快取解析結果（每次從 `*zone.Zone` 取即可）
- 調整 WARN / DEBUG 的 log severity 策略（由 `notify-toggle` 處理整體噪音問題）

## Decisions

### `NotifyTargets()` 回傳型別改為結構

改為：

```go
type NotifyTarget struct {
    Host string        // NS hostname (FQDN), 保留做 log/debug 用
    IPs  []netip.Addr  // 從 in-zone glue 取得的 A / AAAA IPs，可能為空
}

func NotifyTargets(z *zone.Zone) []NotifyTarget
```

**Rationale:**
- 呼叫端需要同時知道「target 主機名」（log / 語意）與「實際該連哪些 IP」（實作）
- `[]netip.Addr` 比 `[]net.IP` 在 Go 1.18+ 是官方推薦型別：值型別、可比較、IPv4/IPv6 統一
- 空 `IPs` 明確代表「無 glue」，語意清晰不靠 nil 表達

**Alternatives considered:**

- **回傳 `map[string][]netip.Addr`**（拒絕）：hostname 可能重複、順序丟失，log 也較難印「原 NS」對應關係
- **保持 `[]string`，另外加 `ResolveGlue(z, host)` helper**（拒絕）：呼叫端要配對兩次查詢，de-dup 邏輯會分散，IP 缺失時要多一次特判

### 無 glue 時 skip，**不** fallback 到系統 resolver

當 `NotifyTarget.IPs` 為空（out-of-bailiwick NS、或 in-bailiwick 但缺 A/AAAA glue）：

- `dispatchNotifies()` 對該 target 只記一筆 `debug` log（含 `source: "skipped-no-glue"`），不發 NOTIFY、不退而求其次
- 不呼叫 `net.Dial(hostname)` 或任何會觸發系統解析的路徑

**Rationale:**
- Fallback 會在缺外網的部署上重現原本的 timeout 噪音，違反本 change 的 Why
- 「沒 glue 就不發」是明確可預測的語意，符合 RFC 1996 §3.3 的 "MAY" 精神（NOTIFY 本就是 best-effort）
- 真的有外網可達 slave 的場景，正確解法是顯式 `also-notify { ip; };`——那是另一個 change 的 scope

**Alternatives considered:**

- **無 glue 時退到系統 resolver**（拒絕）：重現原問題
- **無 glue 時 fail startup**（拒絕）：NOTIFY 是 best-effort，不該阻塞啟動

### 多 IP glue 的處理：每個 IP 各發一次

若 `ns21.example.com` 同時有 A（IPv4）與 AAAA（IPv6）glue，或多筆 A（anycast / 多台 slave 共用 hostname），對 **每個 IP 各發一次 NOTIFY**。

**Rationale:**
- IPv4/IPv6 雙棧：單一族失敗不影響另一族
- 多台 slave：各自獨立的 DNS daemon，都需要收到 NOTIFY
- 程式實作代價低：de-dup key 從 `(origin, host)` 擴到 `(origin, host, ip)`，goroutine 數增加可忽略（原本 host 層級已經是 goroutine-per-target）

**Alternatives considered:**

- **只發第一個 IP**（拒絕）：無法處理多 slave 共用 hostname，也無法處理雙棧其中一個不通的狀況
- **僅發 AAAA 若存在，否則 A**（拒絕）：雙棧部署中某一族不通時 NOTIFY 就全滅，比原本糟

### De-dup key 從 `(origin, target)` 擴為 `(origin, host, ip)`

[cmd/shadowdns/main.go:577-587](cmd/shadowdns/main.go#L577-L587) 目前用 `key{origin, target}` 跨 view de-dup。加入 IP 後 key 要再包含 IP，否則「A zone 的 ns2 解到 10.0.0.1、B zone 的 ns2 也解到 10.0.0.1」這種 cross-zone case 會重複發（罕見但不需要）。

**Rationale:**
- 語意精確：同一個 (zone, host, ip) 對才是同一筆 NOTIFY
- Cross-view 的 de-dup 行為維持不變（同 zone 在多 view 仍只發一次）
- key 大小只多一個 `netip.Addr`，是值型別、map key 友善

### Glue 查找同 zone 優先，不跨 zone

僅在收到查詢的同一個 `*zone.Zone` instance 內，呼叫 `z.Lookup(owner, dns.TypeA)` 與 `z.Lookup(owner, dns.TypeAAAA)` 取 RR slice（owner 透過 `dnsutil.LookupKey(host)` 做 FQDN + lowercase 折大小寫）。不去別的 zone 尋找（即使內部載入了 `ns1.example.com.` zone 的資料，查 `example.test.` 的 NS 時也不跨查）。

**Rationale:**
- 符合 DNS 語意：glue record 的定義就是「與 NS 記錄同 zone 的 A/AAAA」
- 走 `z.Lookup` 而非直接戳 `z.Records[...]`，可順帶處理單/多 qtype 內聯結構的 `*qtypeStore` 雙態，不需要 helper 自己感知儲存層細節
- Owner 須 lowercase 折大小寫：zone 索引依 RFC 4343 將 owner key 強制 lowercase，但 RR 本身保留原 case；NS RDATA 帶入時若沒折大小寫會錯過 hit
- 實作單純、可預測、沒有跨 zone 視野/view 的邊界問題
- 跨 zone 查找等價於實作內部 resolver，屬於 Non-Goal

**Note:** `*dns.A` 的 `A` 欄位是 `net.IP`、`*dns.AAAA` 的 `AAAA` 欄位也是 `net.IP`；helper 需透過 `netip.AddrFromSlice()`（或 `netip.AddrFrom4` / `netip.AddrFrom16` 對特定長度）轉成 `netip.Addr`。轉換失敗（不應發生但要防）時跳過該 RR。

### Log 新增 `source` 欄位

本專案 logger 已遷移至 `go.uber.org/zap`（見 commit `0cda89a refactor(logging): migrate from log/slog to go.uber.org/zap`），現有 NOTIFY 失敗 log 形如：

```go
logger.Sugar().Warnw("NOTIFY failed",
    "zone", origin,
    "target", targetAddr,
    "attempt", attempt+1,
    "err", err.Error(),
)
```

本 change 在所有 NOTIFY 相關 log（per-attempt warn / 最終失敗 warn / no-glue debug）一致新增 `source` 欄位：

```go
logger.Sugar().Warnw("NOTIFY failed",
    "zone", origin,
    "target", host,
    "ip", ipStr,
    "source", "glue",            // 或 "skipped-no-glue"
    "attempt", attempt+1,
    "err", err.Error(),
)
```

No-glue skip log（debug 等級）：

```go
logger.Sugar().Debugw("NOTIFY skipped: no in-zone glue",
    "zone", origin,
    "target", host,
    "source", "skipped-no-glue",
)
```

**Rationale:**
- 運維看 log 時能立即分辨「已嘗試送 IP」vs「根本沒送」
- 與 `notify-toggle` 的 INFO log（`notify enabled: true/false (source: ...)`) 語意對齊——`source` 這個 key 在 NOTIFY 相關 log 表示「決策來源」
- 沿用既有 zap Sugar `Warnw`/`Debugw` 介面，與 codebase 其它 log 風格一致；不再使用 `log/slog`

## Risks / Trade-offs

- **[Risk] Out-of-bailiwick NS 在本 change 後完全拿不到 NOTIFY**
  → Mitigation：記 `debug` log 讓問題可被發現；README 說明需搭配未來的 `also-notify` directive；本 change 的 Non-Goal 明確聲明

- **[Risk] 使用者 A record 寫錯（glue 指到不可達 IP），NOTIFY 仍會 timeout**
  → Mitigation：這是資料錯誤非解析錯誤，既有 WARN log 已夠；`notify-toggle` 可做為緊急止血

- **[Trade-off] `NotifyTargets` 回傳型別變更是 package-level API break**
  → 該函式只有 `dispatchNotifies` 與測試呼叫，無外部使用者；`internal/` package 不在公開 API 範圍，可自由調整

- **[Trade-off] de-dup key 從 2 欄位擴到 3 欄位，map memory 略增**
  → 在 production 規模下可忽略（hundreds of zones × few NSes = few thousand entries）

- **[Trade-off] 若未來補做 `also-notify`，本 change 的 `NotifyTarget.IPs` 結構要再擴（或另開欄位表達 "explicit from config"）**
  → 可接受：`also-notify` 會是獨立 change，屆時一併調整型別；現在過度設計反而增加複雜度
