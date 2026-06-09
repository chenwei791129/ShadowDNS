## Why

ShadowDNS 目前只在 IPv4 transport 上服務 DNS 查詢。`named.conf` 的 `listen-on-v6` 已被 config-loader 解析進 `Options.ListenOnV6`，但位址解析路徑完全忽略它——`ResolveListenAddresses` 只吃 `ListenOn`，且 `expandAnyIPv4` 明確過濾掉所有 IPv6 位址。對需要透過 IPv6 服務查詢的部署而言（RFC 3901 / BCP 91 的雙協族營運指引），這是一個已宣告為 Planned 的功能缺口。

底層其實已經就緒：bind/serve 層（listener.go 的 bindPair）使用 `net.ListenPacket` / `net.Listen`，對 address family 完全中立；handler.go 已有 `addrFamily` 與 `Unmap` 處理 IPv6 client。缺的只是把 `ListenOnV6` 接進位址解析與綁定流程。

## What Changes

- `ResolveListenAddresses` 改為同時解析 `listen-on`（IPv4）與 `listen-on-v6`（IPv6），回傳 IPv4 集合與 IPv6 集合的聯集；BIND 語意對映維持 `listen-on`→v4、`listen-on-v6`→v6。
- 新增 `expandAnyIPv6`：當 `listen-on-v6 { any; }` 時列舉本機 IPv6 介面位址，過濾 link-local（`fe80::/10`），保留 loopback（`::1`），鏡像現有 `expandAnyIPv4` 的逐位址綁定策略。
- IPv6 採「分離 socket」：每個 IPv6 位址各自 bind 一對 UDP+TCP listener（bracket 形式 `[addr]:port`），而非 dual-stack wildcard `[::]`，以避開 `IPV6_V6ONLY` 跨平台預設不一致並維持與既有逐位址模型一致。
- `--listen` flag 行為：`:53`（無 host）形式時並聯 v4 與 v6 集合；`<host>:port`（明確 host）形式維持單一位址逃生艙語意，host 可為 IPv6 literal（如 `[::1]:53`）。
- SIGHUP reload 的 listener drift 偵測納入 IPv6：reload 時解析的位址集合涵蓋 v4∪v6，與啟動時綁定集合比對。
- 預設維持 IPv4-only 向後相容：`listen-on-v6` 缺省或為 `none` 時，產生零個 IPv6 listener，行為與現況完全一致。
- 更新 README：將 IPv6 listener 由 Planned 移至 Supported，並補充 `listen-on-v6` 的支援說明。

## Non-Goals

- **不支援 dual-stack wildcard `[::]` 綁定**：刻意採分離 socket 逐位址綁定，避免 `IPV6_V6ONLY` 預設值跨平台不一致。
- **不支援逗號分隔的 `--listen` 多位址**（如 `--listen 192.168.0.1:53,[::1]:53`）：`--listen` 維持單一位址逃生艙定位；多位址的正規路徑是 `named.conf` 的 `listen-on` / `listen-on-v6`。CLI 多位址若未來有需求，是另一個獨立 change。
- **不新增獨立的 `--listen-v6` flag**：`:53` 形式已能並聯雙協族，無需額外 flag。
- **不改 bind/serve 層與 handler 讀取路徑**：這兩層已 family-agnostic，本 change 不觸碰。
- **不處理 link-local IPv6 的 zone index（`%eth0`）綁定**：link-local 位址在 `any` 展開時直接過濾。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `dns-server`: listener 位址解析與綁定行為擴充至 IPv6——`ResolveListenAddresses` 並聯 v4/v6 集合、新增 `listen-on-v6` 與 `any` 的 IPv6 展開規則、IPv6 採分離 socket 逐位址綁定、`--listen` 的 v6 並聯與單一位址逃生艙語意。reload 時的 listener drift 偵測（既屬此 capability 的 requirement）涵蓋範圍隨之擴充至 IPv4∪IPv6。

## Impact

- Affected specs: `dns-server`
- Affected code:
  - Modified:
    - internal/server/listenaddr.go
    - cmd/shadowdns/main.go
    - internal/server/listenaddr_test.go
    - test/integration/listenon_test.go（直接呼叫 `ResolveListenAddresses`，簽章變更後須更新呼叫端）
    - README.md
    - docs/migration.md（記錄 `--listen`/`listen-on` 優先序與支援的 token 語法，README 交叉引用）
  - New: (none)
  - Removed: (none)
- Dependencies: 無新增；沿用標準函式庫 net 與既有 miekg/dns。
