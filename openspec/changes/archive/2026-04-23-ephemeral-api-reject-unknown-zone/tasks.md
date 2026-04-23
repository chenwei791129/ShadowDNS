## 1. API 模組加入 zone 檢查

- [x] 1.1 在 `internal/api/server.go` 定義 `ZoneLister` 型別（`func() []string` 或同名 interface），並將其加為 `NewServer` 的新必填參數（型別 + 欄位寫在 `Server` struct）
- [x] 1.2 在 `internal/api/server.go` 實作 zone-membership 輔助函式（使用 `dnsutil.IsInZone` 遍歷 lister 回傳的 canonical origin 清單）
- [x] 1.3 在 `handlePut` 中，於 TTL clamp 之後、`s.store.Put` 之前呼叫該輔助函式；若 FQDN 不落在任何 origin 下，呼叫 `writeError(w, 422, ...)` 並 return，確保 PUT rejects FQDNs outside every configured zone
- [x] 1.4 確認 `handleDelete` 沒有被加上相同檢查（維持冪等，對應 spec 的 "DELETE is not subject to the zone-membership check" scenario）

## 2. 整合到主程式

- [x] 2.1 [P] 在 `cmd/shadowdns/main.go` 建構 `apiSrv` 的呼叫點上，傳入一個 closure，內部讀取 `srv.CurrentState()` 並將 `RootZones` 與 `BackupZones` 的所有 view/origin 扁平化為去重過的 canonical origin 清單（同時覆蓋 "Zone added/removed via SIGHUP reload" scenarios 所仰賴的動態 snapshot 行為）
- [x] 2.2 [P] 更新 `internal/shadowdnscfg`／`cmd/shadowdns` 啟動順序註解或 doc comment，說明 API server 依賴 `*server.Server` 的 state snapshot（若既有程式需調整順序或補註解）

## 3. 測試

- [x] 3.1 [P] 在 `internal/api/server_test.go` 新增單元測試：ZoneLister 提供 `example.com.` 時，PUT `_acme-challenge.exmaple.com` 得到 422 且 store 未改動；PUT `_acme-challenge.example.com` 得到 200（涵蓋 "PUT for FQDN outside every loaded zone" 與 "PUT for FQDN inside a loaded root zone" scenarios）
- [x] 3.2 [P] 在 `internal/api/server_test.go` 新增單元測試：ZoneLister 提供 backup zone origin `backup.com.` 時，PUT `_acme-challenge.foo.backup.com` 得到 200（涵蓋 "PUT for FQDN inside a loaded backup zone" scenario）
- [x] 3.3 [P] 在 `internal/api/server_test.go` 新增單元測試：PUT 目標等於 zone origin apex（`example.com`）時得到 200（涵蓋 "PUT for FQDN equal to a zone origin" scenario）
- [x] 3.4 [P] 在 `internal/api/server_test.go` 新增單元測試：DELETE 對 out-of-bailiwick FQDN 仍回 200（涵蓋 "DELETE is not subject to the zone-membership check" scenario）
- [x] 3.5 [P] 在 `internal/api/server_test.go` 新增驗證順序測試：oversize value 先於 zone 檢查回 400；IP ACL 拒絕先於 zone 檢查回 403（涵蓋 "Zone-membership check runs after existing validations" scenario）
- [x] 3.6 在 `cmd/shadowdns/main_ephemeral_test.go` 新增端對端測試：啟動時 lister 回空清單，PUT 得 422；lister mutator 增加 origin 後重跑 PUT 得 200；減少 origin 後得 422（涵蓋 "Zone added/removed via SIGHUP reload" scenarios；務必避免並行修改 lister 的資料競爭）

## 4. 文件與 smoke

- [x] 4.1 [P] 更新 `docs/ephemeral-api.md`（若存在）說明 422 的新錯誤路徑與觸發條件
- [x] 4.2 [P] 檢視 `scripts/smoke.sh` 是否需要調整（目前 smoke 使用 `--dry-run`，預期不受影響；若有實際 PUT 才需改）

## 5. 驗證

- [x] 5.1 `make lint` 通過
- [x] 5.2 `make test` 通過（race detector 開啟）
- [x] 5.3 `make smoke` 通過
