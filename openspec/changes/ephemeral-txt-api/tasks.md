## 1. Ephemeral Record Store

- [ ] [P] 1.1 建立 `internal/ephemeral/store.go`：實作 ephemeral record store，包含 `Put`、`Lookup`、`Delete`、`Clear` 方法，使用 `sync.RWMutex` 保護 concurrent access。store ephemeral TXT records in memory with expiration，Lookup 時動態計算剩餘 TTL，expired records are not returned on lookup。實作 TTL 過期策略：lazy eviction + periodic GC 中的 lazy eviction 部分
- [ ] [P] 1.2 建立 `internal/ephemeral/store_test.go`：測試 store 和 retrieve a TXT record、put overwrites existing record、lookup after TTL expiration returns empty、TTL in response is dynamically computed、delete removes a specific ephemeral record、delete of non-existent FQDN is a no-op、clear removes all ephemeral records
- [ ] 1.3 在 store 中實作 periodic garbage collection removes expired records 的 background goroutine（預設 30 秒），接受 `context.Context` 控制生命週期，GC stops on context cancellation
- [ ] 1.4 為 periodic GC 新增測試：expired record is removed by GC、GC stops on context cancellation

## 2. Unified ShadowDNS Config Loader（對應 design：總 shadowdns yaml 設定檔格式）

- [ ] [P] 2.1 實作 Load unified ShadowDNS configuration from a YAML file：建立 `internal/shadowdnscfg/config.go`，定義 `Config` struct（`Aliases map[string]string`、`EphemeralAPI *EphemeralAPIConfig`）與 `Load(path string) (*Config, error)`；使用 `gopkg.in/yaml.v3` strict decoding，未知 top-level key 回傳 error
- [ ] [P] 2.2 建立 `internal/shadowdnscfg/config_test.go`：測試 valid config with both sections、aliases-only config、ephemeral-api-only config、empty aliases map、unknown top-level key fails、missing file fails
- [ ] [P] 2.3 實作 Validate aliases section：在 `internal/shadowdnscfg/` 實作 aliases section 驗證：duplicate backup key fails、self-alias entry fails；提供 scenario 測試覆蓋
- [ ] [P] 2.4 實作 Validate ephemeral_api section：在 `internal/shadowdnscfg/` 驗證 `listen` 可由 `net.SplitHostPort` 解析；`allow` 非空且每筆為合法 IP/CIDR；`token` 為選填；missing listen、empty allow、invalid listen、invalid CIDR 皆回傳 error 並點名欄位
- [ ] [P] 2.5 Parse aliases.yaml（MODIFIED）：遷移 `internal/config/aliases.go`，移除獨立 `aliases.yaml` 解析；改由上層傳入 `shadowdnscfg.Config.Aliases` 建立 alias map；保留既有 duplicate / self-alias 驗證邏輯但改由 `shadowdnscfg` 呼叫（或整併至 shadowdnscfg，視最終 layering 決定）；同步更新 `internal/config/aliases_test.go`

## 3. HTTP API Server（對應 design：HTTP API 設計）

- [ ] 3.1 建立 `internal/api/server.go`：實作 HTTP API server listens on a configured address，使用 `net/http`，掛載 PUT `/v1/txt/{fqdn}` 和 DELETE `/v1/txt/{fqdn}` 路由；server 從 `shadowdnscfg.EphemeralAPIConfig` 建構，`ephemeral_api` section 缺席則不建立 server
- [ ] 3.2 實作 IP ACL enforces source IP restriction middleware（Authentication 流程：IP ACL 先行、token 後驗——ACL 部分）：request from allowed IP is accepted、request from disallowed IP is rejected（HTTP 403）、CIDR range matching
- [ ] 3.3 實作 optional token authentication middleware（Authentication 流程：IP ACL 先行、token 後驗——token 部分）：valid token is accepted、invalid token is rejected（HTTP 401）、missing Authorization header when token is configured 回傳 401、no token configured skips validation
- [ ] 3.4 實作 PUT endpoint creates or updates an ephemeral TXT record handler：FQDN canonicalization（lowercase + trailing dot）、JSON body 解析、TTL clamped to [1, 3600]（TTL below minimum is clamped to 1、TTL above maximum is clamped to 3600）、missing or invalid JSON body returns 400
- [ ] 3.5 實作 DELETE endpoint removes an ephemeral TXT record handler：FQDN canonicalization、delete an existing ephemeral TXT record、delete a non-existent record returns 200（idempotent）
- [ ] 3.6 實作 graceful shutdown of API server：context cancellation 時 stop accepting new connections，5 秒 timeout 等待 in-flight requests
- [ ] [P] 3.7 建立 `internal/api/server_test.go`：測試所有 API endpoint、middleware、error case

## 4. DNS Handler 整合

- [ ] 4.1 修改 `internal/server/server.go`：在 `Server` struct 新增 ephemeral store 欄位（ephemeral store 獨立於 ServerState）
- [ ] 4.2 修改 `internal/server/handler.go`：擴充 "Listen for DNS queries on UDP and TCP port 53" 的實作，在 `handleRootQuery` 和 `handleBackupQuery` 中，zone lookup 之後、negative reply 之前插入 ephemeral store 查詢（DNS handler 整合點：zone lookup 之後、negative reply 之前）。zone file record takes precedence over ephemeral record，expired ephemeral record is not returned，non-TXT query type is not matched by ephemeral store，ephemeral TXT record is returned when zone has no match（TTL 為剩餘秒數、AA flag set）
- [ ] 4.3 新增 handler 測試：驗證 ephemeral TXT record 查詢整合、zone 優先、過期不回傳、非 TXT type 不查詢

## 5. CLI Flag 切換與 Main 整合（對應 design：CLI flag：`-config` 取代 `-aliases`（不新增 `-api-conf`）、Reload 原子性：全部驗證通過才切換）

- [ ] 5.1 修改 `cmd/shadowdns/main.go`：新增 `flag.StringVar(&opts.ConfigPath, "config", "", "path to unified ShadowDNS YAML config (required)")`；若 `-config` 未提供則 fatal（對應 `shadowdns-config` 的「config file does not exist」語意之外的「flag 未提供」情況）
- [ ] 5.2 修改 `cmd/shadowdns/main.go`：移除 `flag.StringVar(&opts.AliasesPath, "aliases", ...)`（`cmd/shadowdns/main.go:224`）；若偵測到 `-aliases` 被傳入（例如使用 `flag.Visit` 或 custom parser）則 fatal 並提示 user 遷移到 `-config`
- [ ] 5.3 修改 `cmd/shadowdns/main.go` boot 流程：呼叫 `shadowdnscfg.Load(opts.ConfigPath)` 取得 `Config`；用 `Config.Aliases` 建立 alias map；用 `Config.EphemeralAPI` 建立 API server（`nil` 則不建立）；建立 ephemeral store 並傳入 DNS handler
- [ ] 5.4 實作 Atomic reload of unified config on SIGHUP：重新呼叫 `shadowdnscfg.Load`，任一 section 驗證失敗則 log error 並保留舊 ServerState（reload succeeds when all sections valid、reload fails when aliases section invalid、reload fails when ephemeral_api section invalid、reload fails when YAML decoding fails）；全部通過才 atomic swap，之後呼叫 `ephemeralStore.Clear()`
- [ ] 5.5 Dry-run 模式支援：`-dry-run` 搭配 `-config` 時載入並驗證整份 config（包含 ephemeral_api），但不啟動 API server 與 DNS listener

## 6. 打包與範例檔（對應 design：打包範例檔同步）

- [ ] 6.1 新增 `dist/shadowdns.yaml.example`：單一 YAML document，同時含 `aliases`（一組 backup→root 範例）與 `ephemeral_api`（含 `listen`、`allow` 範例、註解說明 `token` 為選填）
- [ ] 6.2 移除 `dist/aliases.yaml.example`
- [ ] 6.3 更新 `packaging/` 底下的 systemd unit：`ExecStart` 由 `-aliases` 改為 `-config /etc/shadowdns/shadowdns.yaml`
- [ ] 6.4 更新 `packaging/` 底下的 deb 安裝腳本與 `nfpm.yaml` 的 `contents` 區段：例檔安裝路徑由 `/etc/shadowdns/aliases.yaml.example` 改為 `/etc/shadowdns/shadowdns.yaml.example`，移除 `aliases.yaml.example` entry
- [ ] 6.5 更新 `README.md` 與 `docs/migration.md`：說明新 `-config` flag、範例檔位置、從舊 `aliases.yaml` 遷移步驟（把原 map 搬進 `aliases:` section）；明列「`-aliases` 不再支援」為 breaking change

## 7. 整合測試

- [ ] 7.1 End-to-end 測試：啟動 ShadowDNS 指向 `testdata/integration/shadowdns.yaml`，API PUT 新增 `_acme-challenge.example.com` TXT record → DNS query 取得 record → TTL 到期後 query 無結果
- [ ] 7.2 Reload 原子性整合測試（aliases section 失敗）：先啟動服務載入合法 config；將 config 改為含重複 backup key；送 SIGHUP；驗證 server 仍使用舊 alias map、舊 API listener 仍正常、ephemeral store 未被清除、log 中有具體錯誤訊息
- [ ] 7.3 Reload 原子性整合測試（ephemeral_api section 失敗）：先啟動服務；將 config 改為 `ephemeral_api.allow` 含 invalid CIDR；送 SIGHUP；驗證 server 仍使用舊 ephemeral_api、ephemeral store 未清除、log 中有具體錯誤訊息
- [ ] 7.4 Reload 原子性整合測試（全部成功）：修改合法 config；送 SIGHUP；驗證新 alias map 生效、ephemeral store 已被清除、log 顯示 reload success
