## 1. 新增位址解析 helpers（internal/server）

- [x] 1.1 [P] 在 `internal/server/listenaddr.go` 新增 `ResolveListenAddresses(listenFlag string, listenOn []string) ([]string, error)`，實作 design 中的 "Listen-on 來源與優先順序" 三段 precedence，也就是 spec 的 "Derive listen address set from named.conf listen-on" requirement 的主體
- [x] 1.2 [P] 在同檔新增 `expandAnyIPv4()` 實作 design 的 "`any` 展開規則"：呼叫 `net.InterfaceAddrs()`、過濾 IPv6、過濾 link-local（`169.254.0.0/16`）、保留 loopback（含 aliases）
- [x] 1.3 [P] 新增 `parseListenFlag()` helper 實作 design 的 "Port 解析與預設 port"：用 `net.SplitHostPort` 拆出 host/port，供 listen-on 分支組合 `net.JoinHostPort(ip, port)`
- [x] 1.4 [P] 在 `internal/server/listenaddr_test.go` 加測：override 分支 / listen-on 分支 / fallback-any 分支、unsupported token（`!addr`、`port N`、ACL name）被 WARN 並 skip、`none` 產生 fatal error、port 從 `--listen` 繼承到 listen-on 位址
- [x] 1.5 [P] 在 listenaddr_test.go 以可注入的 addr provider 測 expandAnyIPv4：固定介面位址列表→期望過濾結果

## 2. Server struct 與 BindMany（實作 "`Server` struct 改為持有 listener slice"）

- [x] 2.1 在 `internal/server/server.go` 把 `udp *dns.Server` / `tcp *dns.Server` 改為 `listeners []listenerPair`（含 `addr string`、`udp *dns.Server`、`tcp *dns.Server`）
- [x] 2.2 在 `internal/server/listener.go` 新增 `BindMany(addrs []string) error`，實作 design 的 "部分 bind 失敗的容錯語意"、覆蓋 spec requirement "Tolerate per-address bind failures"：per-address 以 UDP+TCP atomic pair 綁定；其中一邊失敗時 Close 另一邊並計為該位址失敗
- [x] 2.3 BindMany 失敗 log 路徑：為 `127.0.0.0/8` 位址的 `EADDRINUSE` 加 systemd-resolved hint（`DNSStubListener=no`）
- [x] 2.4 BindMany 聚合邏輯：任何位址綁成功即啟動；全部失敗回 fatal error 並包含 attempted 數量
- [x] 2.5 把既有 `Bind(addr string)` 改為 `BindMany([]string{addr})` 的薄 wrapper，保持既有單位址測試路徑可用
- [x] 2.6 改寫 `Serve(ctx)` 對每個 `listenerPair` 各開兩個 goroutine（UDP + TCP），任一 goroutine 回 error 觸發整體 shutdown；`Shutdown()` 對所有 listener 並行關閉
- [x] 2.7 新增 `UDPAddrs() []net.Addr` / `TCPAddrs() []net.Addr`；保留 `UDPAddr()` / `TCPAddr()` 回第一個成功綁定位址，支援既有 `internal/server/server_test.go`、`test/integration/axfr_test.go`、`test/integration/helpers_test.go` 不動

## 3. Server 層測試（BindMany 行為）

- [x] 3.1 [P] 在 `internal/server/server_test.go` 加：BindMany 兩個 `127.0.0.1:0` ephemeral port，兩者皆成功；`UDPAddrs()` 回兩筆
- [x] 3.2 [P] 加：BindMany 其中一個位址故意用已佔 port（預先起一個 `net.ListenPacket` 搶走），驗證該位址 WARN 且 server 仍成功啟動 — 覆蓋 "Tolerate per-address bind failures" 的 partial-fail scenario
- [x] 3.3 [P] 加：BindMany 全部位址都故意佔走，驗證回 fatal error 且 error message 包含 attempted 位址數
- [x] 3.4 [P] 加：UDP 成功、TCP 失敗時，原本 UDP 已開的 socket 應被 Close（用 `ss` / `lsof` 無法在 Go 測試中直接驗證；以 pre-bound TCP port + 新 UDP port 模擬該 race 並驗證該位址計為失敗）
- [x] 3.5 [P] 加：`127.0.0.0/8` 位址 `EADDRINUSE` 的 WARN log 必須包含 "DNSStubListener=no" 字串（用 `slog` test handler 抓 log output 驗證）

## 4. 接通 cmd/shadowdns/main.go

- [x] 4.1 在 `cmd/shadowdns/main.go` `run()` 中呼叫 `server.ResolveListenAddresses(opts.ListenAddr, cfg.Options.ListenOn)`，把結果傳入 `srv.BindMany()`；`opts.ListenAddr` 從 bind target 改為 override hint（實作 "Listen-on 來源與優先順序" 的接線）
- [x] 4.2 `--listen` flag 的 help 文字更新為 `"override named.conf listen-on (default: use listen-on from named.conf; falls back to all IPv4 interfaces if listen-on is absent)"`
- [x] 4.3 啟動成功 log 路徑：對每個成功綁定的位址各發一筆 INFO `"listener bound" proto=udp|tcp addr=<ip:port>`，實作 spec 中修訂版的 "Listen for DNS queries on UDP and TCP port 53" requirement；既有 `"shadowdns ready" listen=<addr>` 改為帶 `bound_count=<N>`
- [x] 4.4 SIGHUP reload 路徑：在 reload handler 中重新呼叫 `ResolveListenAddresses`，若結果與目前 bound set 不同，發 INFO log `"listen-address changes require restart"`；**不**重綁 listener — 實作 design 的 "SIGHUP reload 的處理"
- [x] 4.5 在 `cmd/shadowdns/main_test.go` 加 run-level 測試：`cfg.Options.ListenOn = []string{"127.0.0.1"}` + 預設 `--listen :0` → run 應走 listen-on 分支並能啟動；另一組設 `--listen 127.0.0.1:0` + `ListenOn = []string{"10.0.0.1"}` → 應走 override 分支、忽略 ListenOn

## 5. 整合測試

- [x] 5.1 [P] 在 `test/integration/` 新增一個 test case：named.conf 設 `listen-on { 127.0.0.1; };`、不傳 `--listen`、啟動後驗證 `dig @127.0.0.1` 可正常查詢（end-to-end 驗證 "Derive listen address set from named.conf listen-on" 的主要路徑）
- [x] 5.2 [P] 新增 test case：named.conf 設 `listen-on { 127.0.0.1; 127.0.0.2; };`（取決於 CI 環境是否有 127.0.0.2；若沒有，改驗證 `listen-on { 127.0.0.1; 127.255.255.1; };` 其中一個失敗仍啟動）
- [x] 5.3 [P] 驗證 `test/integration/helpers_test.go` 與 `test/integration/axfr_test.go` 的 `UDPAddr()` / `TCPAddr()` 使用點仍 work（single-addr path）

## 6. 文件與 release notes

- [x] 6.1 [P] 在 `docs/migration.md` 新增一節「Listen address behavior change」：說明預設從 `0.0.0.0:53` wildcard 改為 per-address 列舉、新增網卡需 restart、`--listen` override 的保留語意
- [x] 6.2 [P] 若 `packaging/` 下有 troubleshooting 文件提到 systemd-resolved 衝突 workaround，改為備註「新版本不需此 workaround」— **no-op**: `packaging/` 僅有 systemd unit + example configs + postinstall.sh，不含 troubleshooting 文件；無處可改
- [x] 6.3 [P] 在 `README.md` 簡短提到 listen-on honoring；若專案有 CHANGELOG / release-notes 機制則補一筆「Honor `listen-on` from named.conf, per-address bind with graceful failure」— README 加了一行；CHANGELOG 由 release-please 根據 commit message 自動管理
