## 1. Expression parser 的 TDD 契約

- [x] 1.1 在 `internal/shadowdnscfg/envexpand_test.go` 先加入 **Expand required environment variables in YAML string values**、**Apply literal defaults for unset or empty variables** 與 **Expand each string exactly once from left to right** 的 failing table tests，明確覆蓋 non-empty／unset／empty required、default fallback／override、多 expression、環境值與 default 不遞迴；以指定測試的 `go test` 確認實作前失敗。
- [x] 1.2 在 `internal/shadowdnscfg/envexpand_test.go` 先加入 **Preserve escaped and unsupported literal dollar text** 的 failing tests，覆蓋通用 `$$`、`$${NAME}`、`$${NAME:-fallback}`、`$${NAME:?message}`、`$NAME`、一般 `$`、unterminated expression、invalid name 與 unescaped unsupported operators；以指定測試的 `go test` 確認 grammar 邊界尚未實作。
- [x] 1.3 在 `internal/shadowdnscfg/envexpand.go` 實作「**以專用單次 parser 定義有限 expression grammar**」的 package-private parser 與 injected lookup callback，使 1.1–1.2 測試全數通過，並以 `go test ./internal/shadowdnscfg -run 'TestExpand'` 驗證。

## 2. YAML traversal、strict decode 與 fail-safe diagnostics

- [x] 2.1 在 `internal/shadowdnscfg/envexpand_test.go` 先加入 **Isolate environment data from YAML structure** 與 **Preserve strict decoding and semantic validation after expansion** 的 failing tests，覆蓋 mapping key、sequence member、non-string scalar、維持只採用第一份 YAML document 的既有行為、真實 `&anchor`／`*alias` graph，以及含 colon、hash、newline、`---`、tag／anchor-like text 的環境值不能注入節點；以指定測試的 `go test` 確認防護尚未完成。
- [x] 2.2 在 `internal/shadowdnscfg/envexpand.go` 與 `internal/shadowdnscfg/config.go` 實作 **Expand environment expressions before strict unified-config decoding**，依「**使用兩階段 YAML decode 保留資料邊界與 strict decoding**」只處理第一份 document、僅走訪 value-side `!!str` scalars、展開帶 anchor metadata 的 value 並略過 mapping keys／AliasNode，再以 `KnownFields(true)` 解碼；以 2.1 tests 與現有 config tests 全數通過驗證。
- [x] 2.3 在 `internal/shadowdnscfg/envexpand_test.go` 與 `internal/shadowdnscfg/config_test.go` 先加入 **Prevent environment-value disclosure** 的 failing tests，覆蓋 expression error、raw／quoted／escaped／multiline environment values，以及 alias name case-fold／trailing-dot canonicalization後的 downstream validation failure；以 error string 與 captured zap logs 都不含原始或衍生值為驗證條件。
- [x] 2.4 在 `internal/shadowdnscfg/envexpand.go` 與 loader error boundary 實作「**在 loader 邊界使用 fail-safe validation diagnostics**」：先對未展開 node 做 strict structural decode，展開後的 decode／semantic validation 若失敗且曾使用 environment value，僅回報安全變數名稱與 YAML paths，不附原始 downstream cause；以 2.3 tests 及 `go test ./internal/shadowdnscfg` 驗證正規化或轉換後的環境內容也不洩漏。

## 3. 所有 loader 生命週期的整合

- [x] 3.1 在 `cmd/shadowdns/main_test.go` 先加入 **Use the same expansion behavior for every unified-config load** 的 dry-run failing tests：non-empty required 變數成功且不啟動 listener，unset／empty 以錯誤結束且 stdout、stderr 與 captured logs 都不含環境值；以指定 `go test` 驗證。
- [x] 3.2 在 `internal/shadowdnscfg/config_test.go` 與 `cmd/shadowdns/prune_backup_test.go` 驗證「**保持 Load API 與 reload transaction boundary 不變**」：`Load(path, logger)` signature 不變、每次 Load 重新 lookup、prune-backup 經同一路徑取得 expanded config；以 package tests 通過且 production caller 無額外 expansion branch 為驗證條件。
- [x] 3.3 在 `cmd/shadowdns/main_test.go` 先加入 **Re-expand unified-config environment expressions during SIGHUP reload** 的 tests，覆蓋同 process 環境變更後 alias map 等 reloadable state 生效，Ephemeral API／DoH startup-scoped config 不被替換，以及 required 變數 unset／empty 或 downstream validation 失敗時不交換 state、不清除 ephemeral store、failure metric 加一且 log 不洩漏；以指定 reload tests 通過驗證。

## 4. 雙語手冊與導覽

- [x] 4.1 [P] 在 `docs/configuration/shadowdns-yaml.md` 與 `docs/configuration/shadowdns-yaml.zh.md` 實作「**以設定頁、Feature Guide 與 Operations Guide 說明操作契約**」的設定參考：同步記錄 required/default、通用 `$$` escape、變數命名、單次非遞迴、mapping key／non-string exclusions、anchored value 語意、fail-closed 與 fail-safe diagnostics；以中英內容逐項對照及 RFC 2606／RFC 5737 sanitization review 驗證。
- [x] 4.2 [P] 新增 `docs/guides/environment-variables.md` 與 `docs/guides/environment-variables.zh.md`，提供 synthetic process environment 與 Kubernetes Secret → Pod env → ConfigMap YAML 操作流程，明確限定 Secret 用於 `ephemeral_api.token` 等不會被正常記錄的欄位；以雙語內容對照、範例不含真實 infrastructure identifier 與 manual feature-guide rule 驗證。
- [x] 4.3 [P] 新增 `docs/operations/reloading.md` 與 `docs/operations/reloading.zh.md`，實作 **Document process-environment limits for Kubernetes Secret updates**：區分 reloadable alias state、startup-scoped Ephemeral API／DoH config、SIGHUP 可見的 process environment，以及 Secret 更新後必須 rollout restart；以雙語 scenario 對照與 operations-rule content review 驗證。
- [x] 4.4 更新 `mkdocs.yml`，將兩組新頁加入英文 `nav` 與中文 `nav_translations`；執行 `make docs-build`，確認 strict build 無 broken links、navigation mismatch 或雙語頁面問題。

## 5. 整體品質與回歸驗證

- [x] 5.1 執行 `go fmt` 格式化變更後的 Go files，再執行 `make lint` 與 `make test`，確認所有既有 strict config、API token、dry-run、prune-backup 與 SIGHUP reload 行為無回歸。
- [x] 5.2 以 synthetic 設定執行 `go run ./cmd/shadowdns --dry-run`：驗證 non-empty `${API_TOKEN}` 成功，unset 與 empty 皆以非零狀態失敗，輸出只指出 `API_TOKEN` 而不包含值；原始命令與輸出只能使用 RFC 2606／RFC 5737 placeholders。
- [x] 5.3 依 **Implementation Contract** 完成 scope review：確認 in-scope 的 unified YAML callers、grammar、fail-safe diagnostics、reload 與雙語設定／Feature／Operations 文件均有 tests，且未加入 named.conf／zone／CLI expansion、runtime Secret refresh、成功後全域 provenance tracking 或 shell-compatible operators；以 `spectra analyze add-config-environment-expansion --json` 和 `spectra validate add-config-environment-expansion` 通過驗證。
