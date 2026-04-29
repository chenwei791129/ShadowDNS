## 1. Config schema 擴充（Load unified ShadowDNS configuration from a YAML file）

- [x] 1.1 在 `internal/config/aliases.go` 定義 `AliasGroup{ Members []string; RewriteRDATALabels bool }` 物件並更新呼叫端介面，作為 Decision: 旗標傳遞鏈 — alias group → resolver → RewriteRR 的起點，並支撐 spec「Load unified ShadowDNS configuration from a YAML file」要求的解析後內部表示
- [x] 1.2 在 `internal/shadowdnscfg/config.go` 為 `aliases` 欄位實作 custom YAML unmarshaler，落實 Decision: schema 形狀採 "discriminated map" — 既支援舊 list 形式也支援新物件形式（sequence → 視為 members + flag false；mapping → 解析 members + rewrite_rdata_labels，預設 false；未知欄位/缺 members 報錯）
- [x] 1.3 [P] 更新 `internal/config/aliases_test.go` 覆蓋 list 形式、object 形式（含 rewrite_rdata_labels true/false）、混用、缺 members、未知欄位等情境
- [x] 1.4 [P] 更新 `internal/shadowdnscfg/config_test.go` 增加 YAML 解析測試案例對應上述情境
- [x] 1.5 [P] 更新 `testdata/integration/aliases.yaml` 與 `packaging/shadowdns.yaml.example` 提供新 object 形式範例與註解，標示 `rewrite_rdata_labels` 用途

## 2. Anywhere-match 改寫核心

- [x] 2.1 在 `internal/alias/rewrite.go` 新增 `RewriteNameAnywhere(n, root, backup string) string`，落實 Decision: `RewriteNameAnywhere` 採 allocation-free byte-scan 實作：manual byte-scan 找連續 root labels 序列、嚴格 label-boundary、first match wins、使用預先 `Grow` 的 `strings.Builder` 不額外配置 slice
- [x] 2.2 [P] 新增 `internal/alias/rewrite_anywhere_test.go` 覆蓋 in-bailiwick suffix、中段 label、`myroot.com` / `prefixroot.com` 邊界保護、多次出現只改第一次、無匹配原樣回傳、空字串、apex-only 五個以上情境

## 3. Resolver 與 RewriteRR 旗標串接

- [x] 3.1 修改 `internal/alias/rewrite.go` 的 `RewriteRR`：新增 `rewriteRDATALabels bool` 參數，落實 Decision: 採用 B + 顯式 opt-in 旗標（owner 維持 suffix-only，RDATA opt-in 走 anywhere-match）；header owner name 維持呼叫 `RewriteName`（落實 spec「Rewrite owner names in the answer to the original backup zone」owner 規則不受旗標影響）；CNAME / NS / MX / PTR / SRV / SOA 的 RDATA 名稱欄在旗標為 true 時改用 `RewriteNameAnywhere`，false 時維持 `RewriteName`（落實 spec「Apply in-bailiwick rewrite to record values」雙模式）
- [x] 3.2 修改 `internal/alias/override.go` 的 `Resolve` / `ResolveExact` / `ResolveExactNoCNAME` / `ResolveCNAMEFallback` / `ResolveWildcard` / `finalizeBackupRRs` 簽章與內部呼叫，落實 Decision: 旗標傳遞鏈 — alias group → resolver → RewriteRR 中段，把旗標自呼叫端透傳到 `RewriteRR`
- [x] 3.3 修改 `internal/server/handler.go` 與 `internal/server/build.go` 在判定 backup zone 後，從 `AliasGroup` 取得 `RewriteRDATALabels` 並傳入 alias resolve 路徑
- [x] 3.4 [P] 更新 `internal/alias/override_test.go` 既有案例：補齊新參數，並對 owner-rewrite 加旗標 true/false 兩種斷言確認 owner 行為不變

## 4. Integration test 還原 dnspyre 案例

- [x] 4.1 新增 `test/integration/alias_rdata_rewrite_test.go`：以 `root.com` 為 root + `backup.com` 為 backup（`rewrite_rdata_labels: true`）的最小化 zone 結構，驗證對 `host.backup.com.` 的 CNAME 查詢回傳 `host.backup.com.cdn.example.net.`；同時對另一組 `rewrite_rdata_labels: false` 的 backup 驗證舊行為（CNAME target 維持 root.com 字面值）

## 5. 驗證與收尾

- [x] 5.1 `make test` 全綠（含新 unit + integration）；`make lint` 無新警告
- [x] 5.2 [P] 更新 `CHANGELOG.md` 紀錄 schema 擴充與行為差異（v0.x.x 階段不標記 breaking）
