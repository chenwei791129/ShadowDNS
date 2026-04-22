## Why

目前 `shadowdns.yaml` 的 `aliases` 區段採 `backup: root` 一對一結構，與舊 `aliases.yaml` 的 `root: [backups...]` 一對多結構相反。使用者普遍把「一個根域名對應多個備援域名」視為自然的心智模型，現行格式讓每個 root 要重複寫 N 行、也讓 diff 追蹤哪些 backup 歸屬同一個 root 變困難。現行同時存在的 legacy `LoadAliases()` 與 `aliases.yaml` fixture 也造成兩份互斥格式並存，容易誤導 contributor。

## What Changes

- **BREAKING**: `shadowdns.yaml` 的 `aliases` 區段 schema 從 `map<backup, root>` 改為 `map<root, list<backup>>`。既有使用者必須把每行 `backup: root` 改寫為對應 root 下的 list item。
- `internal/shadowdnscfg` 的 YAML 解碼改用 `map[string][]string`；root→[backups] 倒轉成內部 backup→root 的邏輯在 loader 內完成，最後仍呼叫 `BuildAliasMap` 做正規化與衝突檢查（重複 backup、self-alias 規則不變）。
- 刪除 `internal/config/aliases.go::LoadAliases()` 及其 9 個 `TestLoadAliases_*` 測試（死碼）。保留 `BuildAliasMap` 與其 unit tests。
- 5 個 integration test 檔案（`test/integration/{helpers,listenon,reload_diff,axfr,notify}_test.go`）改呼叫 `shadowdnscfg.Load()` 讀取新 fixture。
- `testdata/integration/aliases.yaml` 刪除，改為 `testdata/integration/shadowdns.yaml`（新格式）。
- `packaging/shadowdns.yaml.example` 範例改為一對多格式並更新 header 註解。
- `README.md` 的 v0.x breaking change migration note 改為反映最終 schema（`root: [backups]`）。
- `openspec/specs/config-loader/spec.md` 的 `Parse aliases.yaml` Requirement scenarios 改為一對多 YAML；同時合併 L416 與 L800 兩份互相矛盾的重複 Requirement。

## Non-Goals

- 不支援兩種 YAML 格式並存或自動偵測。舊格式直接拒絕，validation error 引導使用者手動遷移。
- 不處理 `aliases.yaml` 獨立檔案的回溯相容；`--aliases` CLI flag 維持 spec 既有規定「不接受」。
- 不改動 `AliasMap` 的內部資料結構（仍是 `map<backup FQDN, root FQDN>`）。
- 不重新設計 `BuildAliasMap` 的函式簽章；只有 loader 入口端解析 YAML 的方式改變。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `shadowdns-config`: 統一 config `aliases` 區段的 YAML schema 改為 `map<root, list<backup>>`；decoder 的結構與 scenarios 需同步更新。
- `config-loader`: `Parse aliases.yaml` Requirement 的 scenarios 從一對一格式改為一對多；同時合併目前 spec 檔中兩份互相矛盾的重複 Requirement。
- `deb-packaging`: `Example configuration files` Requirement 的「Example aliases.yaml is installed」scenario 改為「Example shadowdns.yaml is installed」，對齊實際 packaging（`packaging/shadowdns.yaml.example`）。修正前一次 `ephemeral-txt-api` change 遺留的 spec 漂移。

## Impact

- Affected specs: `shadowdns-config`、`config-loader`、`deb-packaging`
- Affected code:
  - `internal/shadowdnscfg/config.go`（`rawConfig.Aliases` 型別、`Load()` 的 flatten 邏輯）
  - `internal/shadowdnscfg/config_test.go`（所有 aliases YAML literal）
  - `internal/config/aliases.go`（刪除 `LoadAliases`）
  - `internal/config/aliases_test.go`（刪除 9 個 `TestLoadAliases_*`；保留 `TestBuildAliasMap_*`）
  - `test/integration/helpers_test.go`、`listenon_test.go`、`reload_diff_test.go`、`axfr_test.go`、`notify_test.go`（改呼叫 `shadowdnscfg.Load`）
  - `testdata/integration/aliases.yaml`（刪除）
  - `testdata/integration/shadowdns.yaml`（新增）
  - `testdata/integration/README.md`（描述更新）
  - `packaging/shadowdns.yaml.example`（範例與 header 註解）
  - `README.md`（migration note）
- Breaking change: 任何已採用 `shadowdns.yaml` 新 schema 的部署（例如 bench-ns2 測試環境）必須同步改寫 `aliases` 區段，否則服務啟動 / SIGHUP 會拒絕設定。
