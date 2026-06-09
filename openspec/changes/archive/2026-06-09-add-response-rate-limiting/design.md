## Context

ShadowDNS 目前對 UDP 回應沒有任何速率限制。作為公網權威伺服器，它可被當成 DNS 放大／反射攻擊的放大器。BIND 9 以 `rate-limit { ... }` 區塊提供 Response Rate Limiting (RRL)，README 已承諾與 BIND 相容。

既有架構提供了乾淨的整合點：
- handler 的回應全部經由 `internal/server/handler.go` 的 `replyWithAnswer` / `negativeReply` / `replyRcode` 三個函式產生，最終呼叫 `dns.ResponseWriter.WriteMsg`。
- `internal/metrics` 已示範以 `dns.ResponseWriter` wrapper 攔截 `WriteMsg` 的手法（`metrics.ResponseWriter`，於 `ServeDNS` 進入點包住 w）。
- handler 以 `dnsutil.IsUDP(w)` 區分 transport。
- 命中的 zone origin 在 handler 內可取得（`alias.Match.MatchedZone` / `zone.Zone.Origin`），供 name imputation 使用。
- 配置解析集中於 `internal/config`：`ParseOptions` 處理 `options` 區塊的 scalar / braced-list；`parseView` 對未知 view 指令目前 fatal。

RRL 的 ground truth 演算法源自 Vixie & Schryver 的 RRL 技術說明，BIND 實作於 `lib/dns/rrl.c`。本設計對齊其可觀察行為與配置語法；name imputation 與 categorization 的精確細節於實作時對照 `rrl.c` 校準。

## Goals / Non-Goals

**Goals:**

- 緩解 UDP 放大／反射攻擊：對單一 client 位址區段的近乎相同回應施加每秒上限。
- BIND 配置相容：既有 BIND 的 `rate-limit { ... }`（置於 `options`）可直接遷移，子選項與預設值對齊 BIND。
- 對 random-subdomain 洪水有效：透過 name imputation 把同一 zone 下的隨機 NXDOMAIN/NODATA 聚合到單一帳戶。
- 熱路徑零額外配置時零成本、開啟時每回應 O(1) 帳本查詢，無 per-query heap 配置（對齊既有 handler 的效能準則）。
- slip 機制讓正當 client 收到 TC=1 後改走 TCP 重試。
- log-only 試運轉：先觀測再實際限流。

**Non-Goals:**

- per-view RRL scope（v1 僅 `options` 全域；view 內 `rate-limit` warn 並忽略）。
- `qps-scale` 負載自適應收緊（不解析，遇到 warn）。
- TCP 速率限制（TCP 一律放行）。
- `referrals-per-second` 的實際命中（ShadowDNS 不產生 referral；僅為相容而解析）。
- 防禦 random-subdomain 以外的應用層攻擊（RRL 本質上對來源欺騙的反射攻擊最有效）。
- rate-limit 設定的 SIGHUP 熱重載（v1 定為啟動時生效；變更 `rate-limit` 區塊需重啟服務，與既有 SIGHUP reload 的整合不在本變更範圍）。

## Decisions

### 在 ResponseWriter wrapper 的 WriteMsg 收斂點套用限流

新增 `ratelimit.ResponseWriter`，於 `ServeDNS` 進入點包住 w（與 `metrics.NewResponseWriter` 同層）。它在 `WriteMsg(m)` 時：判斷 transport（非 UDP 直接委派）、從 `m` 與自身 `RemoteAddr()` 推導類別、clientIP 與推定 name、呼叫 `Limiter.Decide`，依結果委派原 `WriteMsg`、改寫為 TC=1 截斷後委派、或直接丟棄（回 nil，不寫回應）。

wrapper 必須在 `WriteMsg` 當下才導出 clientIP 與推定 name，**不能在建構時注入**：(a) 早期錯誤回應（NOTIMP/FORMERR/BADVERS）在 handler 解析 clientIP 之前就送出；(b) zone origin 要到 `alias.Detect` 之後才存在，而 wrapper 在進入點就已建好。兩者皆可由 `WriteMsg` 收到的 `dns.Msg` 與 `RemoteAddr()` 完整導出（見「bucket key 的 name imputation 規則」），故 wrapper 與 handler 無狀態耦合。

替代方案：在 `replyWithAnswer` / `negativeReply` / `replyRcode` 三處各自插入限流。否決理由：邏輯重複、易漏，且類別資訊已完整存在於 `dns.Msg`（rcode + answer + authority），在單一收斂點推導最乾淨。

wrapper 鏈順序：最內層為真實 writer，`ratelimit.ResponseWriter` 包在真實 writer 外、`metrics.ResponseWriter` 之內，使得被 drop 的回應仍能被 metrics 觀測到（metrics 記錄「收到的查詢」，RRL 動作另以專屬計數器記錄）。

### 僅對 UDP 套用速率限制，TCP 一律放行

`ResponseWriter.WriteMsg` 先以 `dnsutil.IsUDP` 判斷；非 UDP 直接委派底層 writer，完全不碰帳本。理由：TCP 來源無法偽造、已完成三向交握，限流只會誤傷大回應的正當 TCP 重試（含 slip 引導過來的重試）。

### token-bucket（credit）帳本演算法

每個帳戶（key 見下）持有一個以「最後更新時間 + 餘額」表示的 credit 計數。回應到達時：依經過秒數回補 `rate` credit／秒、餘額上限 `window × rate`、再扣 1；扣後餘額 < 0 即「超限」。`responses-per-second = 0` 表示該類別不限。實作以單調時鐘（避免時間回跳），帳戶結構固定大小、無指標欄位以利分片 map 重用。

替代方案：固定視窗計數器。否決理由：邊界突刺、且與 BIND 的滾動 credit 語意不符。

### bucket key 的 name imputation 規則

key = `(遮罩後 client 位址, 回應類別, 推定 name)`。位址遮罩採 `ipv4-prefix-length`(/24) 與 `ipv6-prefix-length`(/56)。

推定 name 與 client 位址**都從 wrapper 在 `WriteMsg` 當下可得的資料導出，不由 handler 穿線注入**（理由見下節）：clientIP 取自 `w.RemoteAddr()`（沿用 `addrFromRemote` 的取法）；推定 name 取自回應訊息本身：
- `responses`（正常答案）→ 確切 qname，取自 `m.Question[0].Name`（放大攻擊反覆反射同一大答案）。
- `nxdomains` / `nodata` → 命中的 zone origin，取自 authority 區 SOA 的 owner name（`m.Ns[0]`）——`negativeReply` 必放該 namespace 的 SOA（root 為 `rootZone.Origin`、backup 為 backup origin），其 owner 正是要聚合的 zone origin；使 random-subdomain 洪水聚進單一帳戶而限得住。
- `errors`（含落在所有 zone 之外的 REFUSED，`m.Ns` 為空）→ 空 name，使某 client 區段的所有 error 回應聚進一桶。

此設計使推定 name 與「回應類別」一樣只依賴 `dns.Msg`，無需 handler 在 zone 比對後回填 wrapper 狀態。實作時對照 `lib/dns/rrl.c` 的 `make_key()` 校準，以拉高 BIND parity。

### 從 dns.Msg 推導回應類別

分類器只看 `m.Rcode`、`m.Answer`、`m.Ns`：
- `Rcode == NOERROR && len(Answer) > 0` → `responses`
- `Rcode == NOERROR && len(Answer) == 0` → `nodata`
- `Rcode == NXDOMAIN` → `nxdomains`
- `Rcode ∈ {SERVFAIL, FORMERR, REFUSED, NOTIMP, ...}` → `errors`
- referral（authority 含子區 NS 且 Answer 空）→ ShadowDNS 不產生，分類器不需特別處理。

每個回應同時計入其專屬類別與 `all`（若 `all-per-second > 0`）；任一桶超限即觸發限流動作。

### slip 決策：drop 與 TC=1 截斷的交替

超限時依 `slip` 決定動作：`slip == 0` 全部 drop；`slip == 1` 全部回 TC=1 截斷；`slip == n (n≥2)` 對每第 n 個超限回應回截斷、其餘 drop。截斷動作：清空 Answer/Ns/Extra（保留 OPT echo 與 question）、設定 `m.Truncated = true`、保持原 rcode，委派底層 `WriteMsg`。drop 動作：不呼叫底層 `WriteMsg`、回傳 nil。每帳戶維護一個 slip 計數器以實現「每第 n 個」。

### exempt-clients 豁免名單

`exempt-clients { ... }` 為 address-match-list（沿用既有 `view.netmatch` / `config.match` 的位址比對能力）。命中豁免的 client 完全跳過帳本、無條件放行。於 `Decide` 最前段短路。

### log-only 試運轉模式

`log-only yes` 時，`Decide` 照常計算「本來會 drop/slip」的判定並記錄（log + 專屬計數器），但回傳「放行」讓回應正常送出。供上線前評估閾值是否會誤傷正當流量。

### 解析 options 內的 rate-limit 區塊與相容性處理

`ParseOptions` 新增 `rate-limit` case，委派新檔的巢狀 block 解析器（仿 `ParseLogging` 的簽章手法），填入 `OptionsBlock.RateLimit`（新欄位，型別為指標以區分「未配置」與「配置了」）。子選項解析：所有 BIND 子選項皆解析並驗證範圍（對齊 BIND 預設）；`qps-scale` 子選項遇到即 warn 並忽略（不存欄位）；未知子選項沿用既有 warn-and-skip。`parseView` 偵測到 view 內 `rate-limit` 時 warn 並忽略（不 fatal）。

### 新增 RRL Prometheus 計數器

於 `internal/metrics` 新增計數器，標籤含類別（responses/nxdomains/nodata/errors）與動作（dropped/slipped/exempted/logonly_would_drop），供觀測限流成效與調參。

### 帳本資料結構與容量管理

帳本為固定上限的分片 hash（分片以降低鎖競爭），總量介於 `min-table-size`(500) 與 `max-table-size`(20000)。容量達上限時以老化／LRU 風格淘汰最久未用帳戶（對齊 BIND 的 table 行為）。GC 由查詢路徑攤提或背景 ticker 進行，不阻塞熱路徑。

## Implementation Contract

**Behavior（operator 可觀察）：**
- 當 `options` 內存在 `rate-limit { responses-per-second N; ... }` 且 `N > 0`：對單一 client 位址區段、同一推定 name 的同類別 UDP 回應，每秒超過 N 個的部分被 drop 或回 TC=1（依 slip）。TCP 回應不受影響。
- 未配置 `rate-limit` 區塊時：行為與現狀完全一致，熱路徑無額外成本。
- `log-only yes`：所有回應照常送出，但「本來會限流」事件被記錄並計入 logonly 計數器。
- `exempt-clients` 命中的來源：回應永不被限流。
- view 內 `rate-limit` 或 `qps-scale` 子選項：啟動時 warn 並忽略，不阻擋啟動。

**Interface / data shape：**
- `internal/config`：`OptionsBlock` 新增 `RateLimit *RateLimitConfig` 欄位；`RateLimitConfig` 含 `ResponsesPerSecond`、`ReferralsPerSecond`、`NodataPerSecond`、`NxdomainsPerSecond`、`ErrorsPerSecond`、`AllPerSecond`、`Window`、`Slip`、`IPv4PrefixLength`、`IPv6PrefixLength`、`ExemptClients`、`LogOnly`、`MaxTableSize`、`MinTableSize`，各欄位帶 BIND 預設。
- `internal/ratelimit`：`Limiter` 型別，建構自 `RateLimitConfig`；`Decide(clientIP netip.Addr, category Category, name string) Action`，`Action ∈ {Allow, Drop, Slip}`；`Category` 列舉 responses/nodata/nxdomains/errors。
- `internal/ratelimit`：`ResponseWriter` 包裝 `dns.ResponseWriter`，建構時僅帶 `*Limiter`；`WriteMsg` 在當下自身 `RemoteAddr()` 取 clientIP、自 `m` 導出類別與推定 name 後實作上述決策（不在建構時注入 clientIP / zone origin）。
- 分類器：`ClassifyResponse(m *dns.Msg) Category`；推定 name 導出：`ImputedName(m *dns.Msg, category Category) string`（responses 用 `m.Question[0].Name`、nxdomains/nodata 用 `m.Ns` 內 SOA owner、errors 用空字串）。

**Failure modes：**
- `Limiter == nil`（未配置）：`ResponseWriter` 不被掛上，或 `Decide` 視為全 Allow。
- wrapper 無法自 `RemoteAddr()` 解析 client IP：fail-open（視為 Allow、不建帳戶），不阻斷回應、不 panic。
- 配置子選項超出 BIND 範圍（如 slip > 10、prefix-length 越界）：解析時回 fatal error，與既有 `ParseOptions` 數值驗證一致。
- 帳本達 `max-table-size`：淘汰最久未用帳戶，不阻塞、不 panic。

**Acceptance criteria：**
- 單元測試：token-bucket 在固定注入時鐘下，第 N+1 個同類別同 key 回應在一秒內被判超限；window 內回補正確。
- 單元測試：name imputation——同 zone 下不同隨機 qname 的 NXDOMAIN 聚進同一 key；不同 qname 的正常答案各自獨立。
- 單元測試：slip=0/1/2 的 drop vs 截斷比例正確；截斷回應設 TC=1、保留 OPT、清空 Answer。
- 單元測試：TCP 回應不經帳本；exempt-clients 命中放行；log-only 放行但記事件。
- 配置測試：合法 `rate-limit` 區塊解析出正確欄位與預設；view 內 `rate-limit`、`qps-scale` 觸發 warn 而不 fatal；越界值 fatal。
- 分類器測試：各 rcode/answer 組合映射到正確 Category。
- QPS 非回歸壓測（ns1 為 client、ns2 為標的）：先取 pre-RRL baseline，部署後在「未配置 rate-limit」與「配置但上限極大不觸發限流」兩種狀態下，median QPS 皆 ≥ baseline 的 95%；後者壓測期間 dropped/slipped 計數器維持為 0。

**Scope boundaries：**
- In scope：`internal/ratelimit` 新 package、UDP WriteMsg 收斂點整合、`options` 內 `rate-limit` 解析、RRL 計數器、README 特性表更新。
- Out of scope：per-view scope、qps-scale、TCP 限流、referral 命中、packaging 範例 config 之外的部署文件改動。

## Risks / Trade-offs

- [name imputation 規則若與 `rrl.c` 不一致，BIND parity 下降] → 實作時對照 `lib/dns/rrl.c` 的 `make_key()` 校準，並以測試固定預期行為。
- [帳本鎖競爭拖慢熱路徑] → 分片 map 降低競爭；未配置時完全不掛 wrapper，零成本。
- [slip 截斷把正當 client 推去 TCP，增加 TCP 負載] → 此為 RRL 設計本意（截斷即引導 TCP 重試）；slip 預設 2 平衡 drop 與引導。
- [RRL 對 random-subdomain（water torture）攻擊緩解有限] → 已於 Non-Goals 標明；name imputation 對「同 zone NXDOMAIN 洪水」有效，但對跨多 zone 的攻擊仍有限。
- [誤設過低閾值誤傷正當流量] → 提供 log-only 試運轉與依類別計數器供調參。

## Migration Plan

- 純新增功能，未配置 `rate-limit` 時行為不變，可安全部署。
- 建議部署流程：先以 `log-only yes` 上線觀測 would-drop 計數器數日，確認不誤傷後改為實際限流。
- Rollback：移除 `options` 內的 `rate-limit` 區塊並 reload／重啟，即回到無限流狀態。
- 對齊專案實驗階段策略：先部署於 ns2 驗證。

## Open Questions

- `lib/dns/rrl.c` 中 `nodata` 與 `errors` 的精確 name imputation（是否一律 zone origin / 空 name，或另有規則）需於實作時對原始碼確認。
- SIGHUP 熱重載：已定案為 v1 不納入（啟動時生效，見 Non-Goals）；後續若有需求再開獨立 change。
