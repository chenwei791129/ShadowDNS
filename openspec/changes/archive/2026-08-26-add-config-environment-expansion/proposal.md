## Why

Kubernetes 與其他編排環境需要從 Secret 將敏感設定注入程序，但目前 `shadowdns.yaml` 的字串值只能直接寫入設定檔，使 bearer token 等憑證容易落入 ConfigMap 或其他非 Secret 設定來源。ShadowDNS 需要一套範圍有限且 fail-closed 的環境變數展開機制，同時保留既有的 strict YAML decoding、語意驗證與 reload 原子性。

## What Changes

- 在 `shadowdns.yaml` 的字串 scalar value 與字串 sequence element 中支援 `${NAME}` 必填展開；變數未設定或值為空時，整份設定載入失敗。
- 支援 `${NAME:-default}` literal default；變數未設定或值為空時使用 default，否則使用環境值。
- 支援 `$$` dollar escape；`$${NAME}` 與 `$${NAME:-fallback}` 可保留任意 literal braced text。
- 同一個字串可包含多個 expression；變數名稱限定為 `[A-Za-z_][A-Za-z0-9_]*`，展開為單次且不遞迴。
- 僅展開 YAML value-side string scalars；mapping key、tag、alias node 與非字串 scalar 不受影響，環境值不能改變 YAML 文件結構。帶 anchor 的 value scalar 會展開，其 alias reference 取得相同值。
- 啟動、`--dry-run`、prune-backup 與每次 SIGHUP reload 都經過相同展開與驗證流程；reload 失敗時保留原本有效的執行中狀態。
- Expression 錯誤與 environment-backed load/reload validation diagnostics 可指出變數名稱及 YAML 位置，但不得揭露環境值、展開後字串、正規化衍生值或完整設定。
- 雙語手冊新增設定參考、Feature Guide 與 Operations Guide，涵蓋語法、安全欄位選擇、Kubernetes Secret 注入及 Pod restart 語意。

## Capabilities

### New Capabilities

- `config-environment-expansion`: 定義 `shadowdns.yaml` value-side 字串的環境變數語法、fail-closed 行為、YAML 結構隔離、秘密遮罩及生命週期整合。

### Modified Capabilities

- `shadowdns-config`: 統一設定 loader 在 strict decoding 與既有語意驗證前執行安全的字串值展開。
- `sighup-reload`: SIGHUP 重新展開目前 process environment，任何展開或驗證失敗仍維持既有原子失敗語意。

## Impact

- Affected specs: `config-environment-expansion`, `shadowdns-config`, `sighup-reload`
- Affected code:
  - New: `internal/shadowdnscfg/envexpand.go`, `internal/shadowdnscfg/envexpand_test.go`, `docs/guides/environment-variables.md`, `docs/guides/environment-variables.zh.md`, `docs/operations/reloading.md`, `docs/operations/reloading.zh.md`
  - Modified: `internal/shadowdnscfg/config.go`, `internal/shadowdnscfg/config_test.go`, `cmd/shadowdns/main_test.go`, `cmd/shadowdns/prune_backup_test.go`, `docs/configuration/shadowdns-yaml.md`, `docs/configuration/shadowdns-yaml.zh.md`, `mkdocs.yml`
  - Removed: none
- External APIs: no network API or CLI flag changes; the existing `--config` surface gains opt-in expression syntax.
- Dependencies: no new module dependency; implementation uses `gopkg.in/yaml.v3` already present in the project.
- Compatibility: YAML without supported expressions keeps its current parsed result，包括沿用只處理第一份 YAML document 的既有行為。Literal braced text 在此變更後必須以 `$$` escape，例如 `$${NAME:-fallback}`。
