## 1. Ephemeral Record Store

- [x] [P] 1.1 建立 `internal/ephemeral/store.go`：實作 ephemeral record store，包含 `Put`、`Lookup`、`Delete`、`Clear` 方法，使用 `sync.RWMutex` 保護 concurrent access。store ephemeral TXT records in memory with expiration，Lookup 時動態計算剩餘 TTL，expired records are not returned on lookup。實作 TTL 過期策略：lazy eviction + periodic GC 中的 lazy eviction 部分
- [x] [P] 1.2 建立 `internal/ephemeral/store_test.go`：測試 store 和 retrieve a TXT record、put overwrites existing record、lookup after TTL expiration returns empty、TTL in response is dynamically computed、delete removes a specific ephemeral record、delete of non-existent FQDN is a no-op、clear removes all ephemeral records
- [x] 1.3 在 store 中實作 periodic garbage collection removes expired records 的 background goroutine（預設 30 秒），接受 `context.Context` 控制生命週期，GC stops on context cancellation
- [x] 1.4 為 periodic GC 新增測試：expired record is removed by GC、GC stops on context cancellation

## 2. Unified ShadowDNS Config Loader（對應 design：總 shadowdns yaml 設定檔格式）

- [x] [P] 2.1 實作 Load unified ShadowDNS configuration from a YAML file：建立 `internal/shadowdnscfg/config.go`，定義 `Config` struct（`Aliases map[string]string`、`EphemeralAPI *EphemeralAPIConfig`）與 `Load(path string) (*Config, error)`；使用 `gopkg.in/yaml.v3` strict decoding，未知 top-level key 回傳 error
- [x] [P] 2.2 建立 `internal/shadowdnscfg/config_test.go`：測試 valid config with both sections、aliases-only config、ephemeral-api-only config、empty aliases map、unknown top-level key fails、missing file fails
- [x] [P] 2.3 實作 Validate aliases section：在 `internal/shadowdnscfg/` 實作 aliases section 驗證：duplicate backup key fails、self-alias entry fails；提供 scenario 測試覆蓋
- [x] [P] 2.4 實作 Validate ephemeral_api section：在 `internal/shadowdnscfg/` 驗證 `listen` 可由 `net.SplitHostPort` 解析；`allow` 非空且每筆為合法 IP/CIDR；`token` 為選填；missing listen、empty allow、invalid listen、invalid CIDR 皆回傳 error 並點名欄位
- [x] [P] 2.5 Parse aliases.yaml（MODIFIED）：遷移 `internal/config/aliases.go`，移除獨立 `aliases.yaml` 解析；改由上層傳入 `shadowdnscfg.Config.Aliases` 建立 alias map；保留既有 duplicate / self-alias 驗證邏輯但改由 `shadowdnscfg` 呼叫（或整併至 shadowdnscfg，視最終 layering 決定）；同步更新 `internal/config/aliases_test.go`

## 3. HTTP API Server（對應 design：HTTP API 設計）

- [x] 3.1 建立 `internal/api/server.go`：實作 HTTP API server listens on a configured address，使用 `net/http`，掛載 PUT `/v1/txt/{fqdn}` 和 DELETE `/v1/txt/{fqdn}` 路由；server 從 `shadowdnscfg.EphemeralAPIConfig` 建構，`ephemeral_api` section 缺席則不建立 server
- [x] 3.2 實作 IP ACL enforces source IP restriction middleware（Authentication 流程：IP ACL 先行、token 後驗——ACL 部分）：request from allowed IP is accepted、request from disallowed IP is rejected（HTTP 403）、CIDR range matching
- [x] 3.3 實作 optional token authentication middleware（Authentication 流程：IP ACL 先行、token 後驗——token 部分）：valid token is accepted、invalid token is rejected（HTTP 401）、missing Authorization header when token is configured 回傳 401、no token configured skips validation
- [x] 3.4 實作 PUT endpoint creates or updates an ephemeral TXT record handler：FQDN canonicalization（lowercase + trailing dot）、JSON body 解析、TTL clamped to [1, 3600]（TTL below minimum is clamped to 1、TTL above maximum is clamped to 3600）、missing or invalid JSON body returns 400
- [x] 3.5 實作 DELETE endpoint removes an ephemeral TXT record handler：FQDN canonicalization、delete an existing ephemeral TXT record、delete a non-existent record returns 200（idempotent）
- [x] 3.6 實作 graceful shutdown of API server：context cancellation 時 stop accepting new connections，5 秒 timeout 等待 in-flight requests
- [x] [P] 3.7 建立 `internal/api/server_test.go`：測試所有 API endpoint、middleware、error case

## 4. DNS Handler 整合

- [x] 4.1 修改 `internal/server/server.go`：在 `Server` struct 新增 ephemeral store 欄位（ephemeral store 獨立於 ServerState）
- [x] 4.2 修改 `internal/server/handler.go`：擴充 "Listen for DNS queries on UDP and TCP port 53" 的實作，在 `handleRootQuery` 和 `handleBackupQuery` 中，zone lookup 之後、negative reply 之前插入 ephemeral store 查詢（DNS handler 整合點：zone lookup 之後、negative reply 之前）。zone file record takes precedence over ephemeral record，expired ephemeral record is not returned，non-TXT query type is not matched by ephemeral store，ephemeral TXT record is returned when zone has no match（TTL 為剩餘秒數、AA flag set）
- [x] 4.3 新增 handler 測試：驗證 ephemeral TXT record 查詢整合、zone 優先、過期不回傳、非 TXT type 不查詢

## 5. CLI Flag 切換與 Main 整合（對應 design：CLI flag：`--config` 取代 `--aliases`（不新增 `--api-conf`）、Reload 原子性：全部驗證通過才切換）

- [x] 5.1 修改 `cmd/shadowdns/main.go` 的 `registerServerFlags`：新增 `f.StringVar(&opts.ConfigPath, "config", "", "path to unified ShadowDNS YAML config (required)")`；於 `RunE` 或 `PersistentPreRunE` 早期檢查 `opts.ConfigPath == ""` 時回傳 error 讓 cobra fatal（對應 `shadowdns-config` 的「config file does not exist」語意之外的「flag 未提供」情況）
- [x] 5.2 修改 `cmd/shadowdns/main.go` 的 `registerServerFlags`：移除 `f.StringVar(&opts.AliasesPath, "aliases", ...)`（目前位於 `cmd/shadowdns/main.go:237`）；cobra 會自動對未註冊的 `--aliases` 回報 `unknown flag: --aliases` 並以 usage 訊息終止，因此不需額外偵測或客製化錯誤訊息
- [x] 5.3 修改 `cmd/shadowdns/main.go` boot 流程：呼叫 `shadowdnscfg.Load(opts.ConfigPath)` 取得 `Config`；用 `Config.Aliases` 建立 alias map；用 `Config.EphemeralAPI` 建立 API server（`nil` 則不建立）；建立 ephemeral store 並傳入 DNS handler
- [x] 5.4 實作 Atomic reload of unified config on SIGHUP：重新呼叫 `shadowdnscfg.Load`（使用 server process 啟動時記下的 `opts.ConfigPath`），任一 section 驗證失敗則 log error 並保留舊 ServerState（reload succeeds when all sections valid、reload fails when aliases section invalid、reload fails when ephemeral_api section invalid、reload fails when YAML decoding fails）；全部通過才 atomic swap，之後呼叫 `ephemeralStore.Clear()`
- [x] 5.5 Dry-run 模式支援：`--dry-run` 搭配 `--config` 時載入並驗證整份 config（包含 ephemeral_api），但不啟動 API server 與 DNS listener
- [x] 5.6 確認 `shadowdns reload` 子命令（`cmd/shadowdns/reload.go`）維持只接受 `--named-conf`，**不新增 `--config`**；子命令職責僅為送 SIGHUP，實際重新載入 unified config 由 server process 負責

## 6. 打包與範例檔（對應 design：打包範例檔同步）

- [x] 6.1 新增 `dist/shadowdns.yaml.example`：單一 YAML document，同時含 `aliases`（一組 backup→root 範例）與 `ephemeral_api`（含 `listen`、`allow` 範例、註解說明 `token` 為選填）
- [x] 6.2 移除 `dist/aliases.yaml.example`
- [x] 6.3 更新 `packaging/` 底下的 systemd unit：`ExecStart` 由 `--aliases /etc/shadowdns/aliases.yaml` 改為 `--config /etc/shadowdns/shadowdns.yaml`（cobra 遷移後已是雙破折號格式，只需替換 flag 名稱與路徑）
- [x] 6.4 更新 `packaging/` 底下的 deb 安裝腳本與 `nfpm.yaml` 的 `contents` 區段：例檔安裝路徑由 `/etc/shadowdns/aliases.yaml.example` 改為 `/etc/shadowdns/shadowdns.yaml.example`，移除 `aliases.yaml.example` entry
- [x] 6.5 更新 `README.md` 與 `docs/migration.md`：說明新 `--config` flag、範例檔位置、從舊 `aliases.yaml` 遷移步驟（把原 map 搬進 `aliases:` section）；明列「`--aliases` 不再支援」為 breaking change

## 7. 整合測試

- [x] 7.1 End-to-end 測試：啟動 ShadowDNS 指向 `testdata/integration/shadowdns.yaml`，API PUT 新增 `_acme-challenge.example.com` TXT record → DNS query 取得 record → TTL 到期後 query 無結果
- [x] 7.2 Reload 原子性整合測試（aliases section 失敗）：先啟動服務載入合法 config；將 config 改為含重複 backup key；送 SIGHUP；驗證 server 仍使用舊 alias map、舊 API listener 仍正常、ephemeral store 未被清除、log 中有具體錯誤訊息
- [x] 7.3 Reload 原子性整合測試（ephemeral_api section 失敗）：先啟動服務；將 config 改為 `ephemeral_api.allow` 含 invalid CIDR；送 SIGHUP；驗證 server 仍使用舊 ephemeral_api、ephemeral store 未清除、log 中有具體錯誤訊息
- [x] 7.4 Reload 原子性整合測試（全部成功）：修改合法 config；送 SIGHUP；驗證新 alias map 生效、ephemeral store 已被清除、log 顯示 reload success

## 8. 多值 TXT 支援 delta（對應 design：多值 TXT 儲存結構、HTTP API 設計：PUT add-or-refresh、DELETE 全清；specs：Store ephemeral TXT records in memory with expiration、Delete removes all ephemeral entries for an FQDN、PUT endpoint adds or refreshes an ephemeral TXT value、DELETE endpoint removes all ephemeral TXT entries for an FQDN、Listen for DNS queries on UDP and TCP port 53 的多 RR 回應）

- [x] 8.1 修改 `internal/ephemeral/store.go`：把 `records` 由 `map[string]entry` 改為 `map[string][]entry`；`Record` 新增批次型別（建議 `type RecordSet []Record`，或 `Lookup` 直接回傳 `[]Record`）；`Lookup` 回傳所有未過期 entry 的 slice（零筆時回 nil, false）；`Put` 以 value 相等作為判斷條件：相符就更新 `expireAt`，不相符就 append；實作「Delete removes all ephemeral entries for an FQDN」——`Delete(fqdn)` 改為 `delete(s.records, canonical)`（整個 FQDN 一次移除）；`gcSweep` 迭代每個 slice 過濾過期項，slice 變空時移除該 FQDN key；維持 `sync.RWMutex` 保護
- [x] 8.2 更新 `internal/ephemeral/store_test.go`：
  - 新增 `TestStore_PutAppendsDistinctValues`：同 FQDN 先後 Put 兩個不同 value，Lookup 回傳兩筆
  - 新增 `TestStore_PutSameValueRefreshesTTL`：同 FQDN 同 value 第二次 Put 不累積 entry，但 `expireAt` 被更新（可透過注入 clock 驗證 remaining TTL）
  - 新增 `TestStore_DeleteRemovesAllEntriesForFQDN`：同 FQDN 有三筆 value，Delete 後 Lookup 為空
  - 新增 `TestStore_PerEntryExpiration`：同 FQDN 兩筆 value 各有獨立 TTL，時間推進後只有一筆被保留
  - 調整既有 `TestStore_LookupTTLIsDynamic`、`TestStore_LookupAfterExpirationReturnsEmpty` 等以 `[]Record` 新 API 比對
- [x] 8.3 修改 `internal/server/handler.go` 的 `lookupEphemeralTXT`：取得 Lookup 回傳的 `[]Record` 後，為每筆 record 合成一個 `dns.TXT` RR（owner 相同、各自 TTL、每筆的 `Txt` 為單一字串的 slice），集合成一個 `[]dns.RR` 回傳；保持 `qtype != TypeTXT || s.EphemeralStore == nil` 的 short-circuit
- [x] 8.4 更新 `internal/server/handler_ephemeral_test.go`：新增 `TestEphemeral_MultipleTXTRRsReturned` 驗證同 FQDN 兩筆 value 在 answer section 合成兩個 TXT RR；既有單筆測試保留（驗證多值退化為單筆時仍正確）
- [x] 8.5 修改 `internal/api/server.go` 的 PUT handler（Requirement: PUT endpoint adds or refreshes an ephemeral TXT value）：
  - Response body 新增 `count` 欄位：呼叫 store 取得目前 FQDN 的 entry 總數（可在 Put 後再 Lookup 並取 len）
  - 不再有「overwrite existing」行為；append-or-refresh 由 store 負責
- [x] 8.6 修改 `internal/api/server.go` 的 DELETE handler（Requirement: DELETE endpoint removes all ephemeral TXT entries for an FQDN）：語意不變（仍是 store.Delete(fqdn)），但文件/註解明確標示這是全清；DELETE response 不需回 count
- [x] 8.7 更新 `internal/api/server_test.go`：
  - 新增 `TestServer_PUT_AppendsSecondValue`：先後 Put 兩個不同 value，response 第二次 `count=2`
  - 新增 `TestServer_PUT_SameValueRefreshesNoDuplicate`：相同 value 重複 Put，`count` 維持 `1` 且 store 內只有一筆
  - 新增 `TestServer_DELETE_RemovesAllEntries`：先 Put 兩個不同 value，DELETE 後 store 為空
  - 調整既有 `TestServer_PUT_UpdateOverwrites`：其「第二次 Put 不同 value 就取代第一次」的舊語意不再成立，重新命名為 append 測試或刪除
- [x] 8.8 End-to-end 整合測試（task 7.1 的延伸）：在 `cmd/shadowdns/main_ephemeral_test.go` 新增 `TestEphemeralTxtApi_MultiValueEndToEnd`：PUT 兩筆不同 value → dig 看到兩個 TXT RR → DELETE 後 NXDOMAIN
- [x] 8.9 更新 `docs/ephemeral-api.md`：
  - PUT response body 加上 `count` 欄位說明
  - 新增 multi-value 區段：說明 append-or-refresh 語意、ACME wildcard+apex 同時驗證的案例、回應多筆 TXT RR 的行為
  - DELETE 區段加註：「移除該 FQDN 下所有 ephemeral entries；不觸及 zone file」
- [x] 8.10 確認 `cmd/shadowdns/main.go` 的 boot 與 reload 流程無須調整（ephemeral store 建構與清除邏輯已正確，此 delta 不改動 ServerState 或 reload 原子性）
