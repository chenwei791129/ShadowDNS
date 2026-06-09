## Context

ShadowDNS 的 listener 啟動鏈目前為 IPv4-only。位址解析集中在 `internal/server/listenaddr.go` 的 `ResolveListenAddresses`，它只接收 `cfg.Options.ListenOn`（IPv4 token），並透過 `expandAnyIPv4` 列舉介面位址；綁定在 `internal/server/listener.go` 的 `BindMany` / `bindPair`，使用 `net.ListenPacket("udp", addr)` 與 `net.Listen("tcp", addr)`。

當前已就緒的部分（本 change 不觸碰）：

- **bind/serve 層 family-agnostic**：`bindPair` 對位址字串無 family 假設，傳入 bracket 形式的 IPv6 位址（`[::1]:53`）即可綁定。
- **handler 讀取路徑**：`handler.go` 已有 `addrFamily`（回傳 `ipv4`/`ipv6`）與 v4-mapped IPv6 的 `Unmap` 正規化，IPv6 client 來源位址已被正確處理。
- **config 解析**：config-loader 已把 `listen-on-v6` 解析進 `Options.ListenOnV6`，但下游無人讀取。

缺口集中在位址解析：`ResolveListenAddresses` 忽略 `ListenOnV6`，`expandAnyIPv4` 對非 v4 位址 `continue` 跳過，且 `resolveListenOnTokens` 對 IPv6 literal token 以 WARN 跳過。

約束：v0.x 實驗階段僅部署於 bench-ns2，但仍須維持既有 IPv4 部署的零行為變更（向後相容是硬需求，因 reload drift 與 dry-run 摘要都依賴解析結果）。

## Goals / Non-Goals

**Goals:**

- 讓 `named.conf` 的 `listen-on-v6` 真正驅動 IPv6 listener 的綁定。
- IPv6 與 IPv4 解析語意對稱，降低維護者認知負擔。
- 預設（無 `listen-on-v6` 或 `none`）行為與現況逐位元相同。

**Non-Goals:**

- dual-stack wildcard `[::]` 綁定（採分離 socket）。
- 逗號分隔的 `--listen` 多位址。
- 獨立的 `--listen-v6` flag。
- bind/serve 層與 handler 讀取路徑的任何改動。
- link-local IPv6 的 zone index（`%eth0`）綁定。

## Decisions

### 分離 socket 逐位址綁定，而非 dual-stack wildcard

每個 IPv6 位址各自綁定一對 UDP+TCP listener，位址以 bracket 形式 `[addr]:port` 傳入既有 `BindMany`。

- **為何不用 dual-stack `[::]`**：`IPV6_V6ONLY` socket option 預設值跨平台不一致（Linux 多為 0=dual-stack，多數 BSD 為 1=v6-only），會讓「v4 是否也被 `[::]` 接收」變得不可預期，與既有「逐位址、可跳過被佔用的 loopback」模型衝突。逐位址綁定讓 v4 與 v6 各自獨立、行為可預測。
- **既有綁定層無需改動**：`bindPair` 已是 family-agnostic，bracket 位址直接可用。

### ResolveListenAddresses 並聯 IPv4 與 IPv6 集合

`ResolveListenAddresses` 新增 `listenOnV6 []string` 參數，回傳「IPv4 解析集合 ++ IPv6 解析集合」的串接（v4 在前、v6 在後，各自維持首次出現順序）。

- **優先序維持**：`--listen` 帶明確 host（Precedence 1）時仍只回 `{listenFlag}` 並忽略 `listen-on` / `listen-on-v6`——host 可為 IPv6 literal。只有 `:port`（無 host）形式才走並聯。
- **空集合錯誤語意**：當且僅當 v4 與 v6 兩者解析後都為空，才回傳啟動失敗錯誤。其中一族為空（例如 `listen-on-v6 { none; }` 或缺省）而另一族非空時，正常啟動。沿用既有 `noneExplicit` 區分「明確 none」與「全部 token 不支援」的錯誤訊息，v6 各自獨立判斷。

### expandAnyIPv6 的列舉與過濾規則

新增 `expandAnyIPv6`，鏡像 `expandAnyIPv4` 結構，差異在過濾條件：

- 僅納入 `ip.To4() == nil` 且 `ip.To16() != nil` 的位址（真正的 IPv6）。
- 過濾 link-local `fe80::/10`（需 zone index 才能綁定／服務，`any` 展開時無從得知 zone）。
- 保留 loopback `::1`（對稱於 v4 保留 127.x；綁定失敗由 `logBindFailure` 一層處理）。
- 同樣經由可注入的 `ifaceAddrs` 變數，讓測試以 fixture 驅動。

`resolveListenOnTokens` 改為支援 family 參數（或分出 v6 變體）：v6 解析路徑接受 IPv6 literal（`net.ParseIP(tok)` 且 `To4()==nil`），`any` 展開呼叫 `expandAnyIPv6`，`none` 語意不變。v4 路徑維持原狀，IPv6 literal 仍在 v4 路徑被 WARN 跳過（反之 IPv4 literal 在 v6 路徑被 WARN 跳過）。

### --listen 的 IPv6 並聯與單一位址逃生艙語意

- `--listen :53`（無 host）：port 套用到 v4 與 v6 兩個解析集合，並聯回傳。
- `--listen [::1]:53`（明確 v6 host）：`net.SplitHostPort` 解析出 host=`::1`，回傳 `{"[::1]:53"}`，忽略 named.conf 兩個 block。
- 不支援逗號分隔；維持單一 `net.SplitHostPort` 呼叫。flag help text 更新以反映 v6 可用。

### SIGHUP reload drift 偵測納入 IPv6

`cmd/shadowdns/main.go` 的 `ResolveListenAddresses` 呼叫都改傳 `cfg.Options.ListenOnV6`。drift 偵測既有以 `AddrSetEqual` 比對「啟動綁定集合 vs 重新解析集合」的邏輯不變——因兩側都涵蓋 v4∪v6，drift 判斷自然涵蓋 v6 介面增刪。

### dry-run 預覽 bind 集合

為讓 `--dry-run` 能在不啟動任何 listener 的前提下回報實際會綁定的位址（含 v6），啟動路徑的 `ResolveListenAddresses` 解析上移至 dry-run early-return 之前一次性完成，正常綁定路徑重用同一 `listenAddrs`（不重複解析）；dry-run 摘要新增 `listen_addrs` 欄位。因此 `main.go` 內的解析呼叫由原本「啟動綁定」與「reload drift」兩處，調整為「啟動前一次性解析（dry-run 與綁定共用）」與「reload drift」兩處——解析點仍是兩處，僅啟動側的解析時機提前。

## Implementation Contract

**Behavior（營運者觀察）：**

- `named.conf` 含 `listen-on-v6 { any; };` 時，ShadowDNS 在每個非 link-local 的本機 IPv6 介面位址（含 `::1`）綁定 UDP+TCP listener，並以該 transport 回應 DNS 查詢。
- `listen-on-v6 { 2001:db8::1; };` 時，僅在該位址綁定。
- `listen-on-v6 { none; }` 或缺省時，不綁定任何 IPv6 listener；IPv4 行為與現況完全相同。
- `--listen [::1]:5353` 時，僅綁定 `[::1]:5353`（v4 與兩個 named.conf block 皆忽略）。
- 個別 IPv6 位址綁定失敗（如位址不存在）記 WARN 並跳過，只要至少一個 listener 成功即啟動——沿用既有 `BindMany` 語意。
- SIGHUP reload 後，若本機 IPv6 介面位址集合相對啟動時有變動，drift 以既有「記 WARN、保留現有 listener、不重新綁定」方式處理。

**Interface / data shape：**

- `ResolveListenAddresses(listenFlag string, listenOn []string, listenOnV6 []string, logger *zap.Logger) ([]string, error)` — 新增第三個參數 `listenOnV6`；回傳值仍為 bracket-normalised 的 `host:port` 字串切片。
- 新增 `expandAnyIPv6() ([]string, error)`，回傳 IPv6 位址字串（非 bracket 形式，由呼叫端以 `net.JoinHostPort` 包裝）。
- v6 token 解析支援 `any` / `none` / IPv6 literal 三類；其餘 token（含 IPv4 literal、`!addr`、ACL 名稱）記 WARN 跳過。

**Failure modes：**

- v4 與 v6 解析集合皆空 → 回傳啟動錯誤（沿用既有兩種訊息，並為 v6-only-none 情境提供對應訊息）。
- 單一位址綁定失敗 → WARN 跳過，非致命。
- 不支援的 v6 token → WARN 跳過，非致命。

**Acceptance criteria：**

- `internal/server/listenaddr_test.go` 既有 IPv6 placeholder 測試（`TestResolveListenAddresses_IPv6LiteralTokenSkipped`、`TestExpandAnyIPv4_FiltersIPv6`）改寫／新增，涵蓋：v6 `any` 展開過濾 fe80::/10 保留 ::1、v6 literal 解析、v4∪v6 並聯順序、`--listen [::1]:port` 逃生艙、純 `none` v6 與非空 v4 共存正常啟動。
- `make test`（race detector）通過。
- `make smoke`（`--dry-run`）以含 `listen-on-v6` 的 named.conf 摘要列出 v6 位址。

**Scope boundaries：**

- In scope：`internal/server/listenaddr.go` 的解析邏輯、`cmd/shadowdns/main.go` 兩處解析呼叫接線（啟動側解析上移至 dry-run 前共用、reload drift）、dry-run 摘要新增 `listen_addrs`、flag help text、`scripts/smoke.sh` 注入 v6 directive、`internal/server/listenaddr_test.go` 測試、README 的 Planned→Supported 更新。
- Out of scope：`listener.go` bind/serve 層、`handler.go` 讀取路徑、config-loader 解析（已完成）、逗號分隔 `--listen`、dual-stack wildcard。

## Risks / Trade-offs

- [分離 socket 在 `any` 展開時，若介面有大量 IPv6 位址（如多個 GUA + ULA）會綁定較多 socket] → DNS server listener 數量本就有界，且與 v4 `any` 同模型；可接受。
- [`::1` 保留可能在某些容器環境綁定失敗] → 沿用 `logBindFailure` WARN 跳過，非致命，與 v4 loopback 處理一致。
- [`ResolveListenAddresses` 簽章變更會影響既有呼叫端與測試] → 呼叫端僅 main.go 兩處 + 測試；屬機械式更新，編譯期即可捕捉遺漏。v0.x 階段簽章變更可接受（不標記 breaking）。
- [link-local 過濾意味 fe80:: 位址無法服務] → 屬刻意 Non-Goal；需要時營運者可顯式列出 GUA/ULA 位址。
