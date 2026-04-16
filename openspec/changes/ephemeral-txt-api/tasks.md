## 1. Ephemeral Record Store

- [ ] [P] 1.1 建立 `internal/ephemeral/store.go`：實作 ephemeral record store，包含 `Put`、`Lookup`、`Delete`、`Clear` 方法，使用 `sync.RWMutex` 保護 concurrent access。store ephemeral TXT records in memory with expiration，Lookup 時動態計算剩餘 TTL，expired records are not returned on lookup。實作 TTL 過期策略：lazy eviction + periodic GC 中的 lazy eviction 部分
- [ ] [P] 1.2 建立 `internal/ephemeral/store_test.go`：測試 store 和 retrieve a TXT record、put overwrites existing record、lookup after TTL expiration returns empty、TTL in response is dynamically computed、delete removes a specific ephemeral record、delete of non-existent FQDN is a no-op、clear removes all ephemeral records
- [ ] 1.3 在 store 中實作 periodic garbage collection removes expired records 的 background goroutine（預設 30 秒），接受 `context.Context` 控制生命週期，GC stops on context cancellation
- [ ] 1.4 為 periodic GC 新增測試：expired record is removed by GC、GC stops on context cancellation

## 2. API Config 載入

- [ ] [P] 2.1 建立 `internal/api/config.go`：實作 load API configuration from a YAML file（API 設定檔格式：YAML），解析 `listen`、`allow`、`token` 欄位，validate ACL entries at load time（檢查 IP/CIDR 格式），missing listen field fails，empty allow list fails
- [ ] [P] 2.2 建立 `internal/api/config_test.go`：測試 valid config with all fields、valid config without token、missing listen field fails、empty allow list fails、invalid CIDR in allow list、mixed valid IPv4 and CIDR entries

## 3. HTTP API Server

- [ ] 3.1 建立 `internal/api/server.go`：實作 HTTP API server listens on a configured address（HTTP API 設計），使用 `net/http`，掛載 PUT `/v1/txt/{fqdn}` 和 DELETE `/v1/txt/{fqdn}` 路由
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

## 5. Main 整合與 Reload 行為

- [ ] 5.1 修改 `cmd/shadowdns/main.go`：新增 `-api-conf` CLI flag，API server is not started when flag is absent。載入 API config（YAML 格式）、建立 ephemeral store、啟動 API server（獨立 port）
- [ ] 5.2 實作 reload 行為：SIGHUP reload 時呼叫 ephemeral store `Clear()`，清除所有 ephemeral records（reload 行為設計決策）
- [ ] 5.3 Dry-run 模式支援：`-dry-run` 搭配 `-api-conf` 時驗證 API config 但不啟動 API server
- [ ] 5.4 整合測試：end-to-end 驗證 API 新增 TXT record → DNS query 取得 record → TTL 到期後 query 無結果
