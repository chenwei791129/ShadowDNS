## Why

shadowdns 目前用單一 wildcard socket（`net.ListenPacket("udp", ":53")`）綁 `0.0.0.0:53`，只要機器上任何一個具體 IP（例如 systemd-resolved 佔的 `127.0.0.53:53`）已被佔用，整個 server 就會因 `EADDRINUSE` 啟動失敗 — 即使對外介面 IP 完全沒衝突。BIND9 在同一台機器上能啟動成功，因為它讀取 `named.conf` 的 `listen-on` 指令、對每個位址各開一個 socket，單一位址 bind 失敗只會 log warning 並跳過其他成功的繼續服務。

shadowdns 的定位是「讀 named.conf 做 view-based DNS」，但目前完全忽略 `named.conf` 裡已被 parser 解析出來的 `options.ListenOn` 欄位，啟動位址只靠 CLI `--listen` 旗標指定。這是個 behavior promise 缺口，且在 systemd-resolved 預設啟用的 Linux 發行版（Ubuntu 24.04、Debian 12 等）上直接造成部署阻塞。

## What Changes

- 讓 `dns-server` honor `named.conf` 的 `options { listen-on { ... }; };` 指令：
  - `listen-on { any; };`（或未指定）→ 列舉本機所有非 IPv6 介面位址，每個位址各開 UDP + TCP socket
  - `listen-on { a.b.c.d; ... };` → 對列出的每個位址各開 UDP + TCP socket
  - `listen-on { none; };` → 不開 IPv4 listener（僅對 v6 變體有意義；本次僅 IPv4 範圍內把 `none` 視為「不監聽」）
- 單一位址 bind 失敗時 log WARN 並繼續綁其他位址，而非 fatal exit；**當所有配置位址都失敗才回傳錯誤**
- `--listen` CLI flag 改為**覆寫 (override) 語意**：若使用者顯式傳入非預設值，以該值為準並忽略 `listen-on`（向後相容，方便測試與 opt-out）
- 啟動 log 改為每個成功 bind 的位址各一筆 INFO（格式：`msg="listener bound" proto=udp addr=10.0.0.1:53`），方便 ops 確認實際綁了哪些 IP
- **BREAKING**（對執行環境而非 API）：預設綁定行為改變 — 從「單一 `0.0.0.0` wildcard」變成「per-address per-interface」。在啟動後新增的網卡/IP 不會被自動 pick up（BIND 用 `interface-interval` 定期 rescan，本次**不**納入範圍，屆時以 `SIGHUP` 重新整理即可）

## Non-Goals

- **IPv6 支援 (`listen-on-v6`)**：本次僅處理 IPv4 `listen-on`。v6 parser 欄位保留但 server 端暫不消費；之後另開 change 處理
- **BIND 的 port override 語法 (`listen-on port 5353 { ... };`)**：本次一律使用 DNS 預設 port 53（或沿用 `--listen` 指定的 port）。Port override 之後視需要再加
- **BIND 的 address match list 進階語法**：`!addr` 排除、ACL 參照（`listen-on { !10.0.0.1; any; };`、`listen-on { trusted-net; };`）不支援，解析時若遇到會 WARN 並忽略該 token
- **動態介面重新掃描**：不實作 BIND 的 `interface-interval` 週期性 rescan；網卡變動需要 SIGHUP 觸發 reload（reload 行為延續既有 SIGHUP 路徑）
- **`interface` keyword**（BIND 少用的介面名指定）：不支援
- **Source IP matching on response**：per-address socket 天然解決這個問題（socket 綁在具體 IP，回應自然從該 IP 出去），但本次不把它列為顯式需求、不加額外測試驗證；設為 implementation side effect

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `dns-server`: 監聽位址行為從「單一 wildcard socket」變成「honor `listen-on` 做 per-address bind，個別 bind 失敗不致命」。新增 requirement 描述 `listen-on` 來源、`any` 展開規則、部分 bind 失敗的容錯行為，以及 `--listen` flag 的 override 語意

## Impact

- Affected specs: `dns-server`（modified — 新增/修改 binding 相關 requirement）
- Affected code:
  - `cmd/shadowdns/main.go` — 將 `opts.ListenAddr` 從「綁定目標」改為「override hint」；把 `cfg.Options.ListenOn` 傳進 server；新增 per-address INFO log
  - `internal/server/listener.go` — `Bind(listenAddr string)` 簽章改為 `Bind(addrs []string)` 或新增並列的 `BindMany`；維護 `[]*dns.Server` 而非單一 UDP/TCP pair；`Serve` 等候所有 goroutine；`UDPAddr()`/`TCPAddr()` 改回 slice 或保留第一個 for backcompat（由 design.md 決定）
  - `internal/server/server.go` — 若 `Server` struct 持有單一 udp/tcp 欄位，改為 slice
  - `internal/server/` — 新增 interface enumeration helper（包裝 `net.InterfaceAddrs()`，過濾 IPv6 與 link-local）與 listen-on token 展開 helper
  - `cmd/shadowdns/main_test.go` / `internal/server/server_test.go` / `test/integration/*` — 測試既有用 `127.0.0.1:0` 的路徑仍須 work；新增 listen-on 展開 / 部分失敗 / override 測試
- Affected ops：
  - Ubuntu/Debian 搭 systemd-resolved 預設 stub listener 的部署**不再需要關閉 stub** 即可啟動，降低踩坑面
  - 啟動 log 數量增加（每 IP 一筆），需同步更新 `deb-packaging` / `packaging/` 裡的 troubleshooting 文件（若有）
