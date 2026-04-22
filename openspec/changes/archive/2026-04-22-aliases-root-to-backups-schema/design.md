## Context

ShadowDNS 的 alias 功能早期由獨立的 `aliases.yaml` 檔案承載，YAML 結構為 `root: [backups...]`（一對多），由 `internal/config/aliases.go::LoadAliases()` 讀取。

`ephemeral-txt-api` change 將設定統一到 `shadowdns.yaml` 時，`aliases` 區段的 schema 被改成 `backup: root`（一對一），由 `internal/shadowdnscfg/config.go::Load()` 經 `config.BuildAliasMap()` 建出 alias map。生產程式碼 (`cmd/shadowdns/main.go:60,308`) 目前只走 `shadowdnscfg.Load()` 路徑。

然而 legacy 程式仍大量殘留：
- `internal/config/aliases.go::LoadAliases()` 與其 9 個 `TestLoadAliases_*` 測試仍在 repo 內（死碼）。
- 5 個 integration test 檔案（`test/integration/{helpers,listenon,reload_diff,axfr,notify}_test.go`）仍呼叫 `LoadAliases`、仍使用 `testdata/integration/aliases.yaml` fixture（舊格式）。
- `openspec/specs/config-loader/spec.md` 中同時存在兩份 `Parse aliases.yaml` Requirement（L416-515 舊、L800-827 新），後者明文禁止 `--aliases` flag，與前者互相矛盾。

使用者回饋現行一對一格式不符合心智模型：「一個根域名對應多個備援」在讀寫與 diff review 上都比「每行一個 backup→root」直觀。又因兩種格式在 repo 並存，contributor 容易誤會哪個才是現行語意。

本次 change 目的：把 `shadowdns.yaml` 的 `aliases` schema 切回一對多，並同步清掉 legacy code path，讓 repo 只剩單一資料來源。

## Goals / Non-Goals

**Goals:**

- `shadowdns.yaml` 的 `aliases` 區段使用 `map<root, list<backup>>` 結構，與使用者心智模型一致。
- 內部 `AliasMap`（`map<backup FQDN, root FQDN>`）資料結構**不變**，僅 YAML 入口的解析方式改變。
- 正規化規則維持現狀：重複 backup（同一個 backup 掛在兩個 root 下）、self-alias、空字串、含空白字元，皆拒絕。
- 移除 `LoadAliases` 路徑與 `testdata/integration/aliases.yaml` fixture，让 repo 只有一個 loader、一個 YAML schema。
- Spec `config-loader` 合併兩份重複的 `Parse aliases.yaml` Requirement，scenarios 以新 YAML 例示。

**Non-Goals:**

- 不支援新舊兩種 YAML 格式並存或 auto-detect。舊格式以 validation error 拒絕。
- 不提供 `aliases.yaml` 獨立檔案的回溯相容；`--aliases` CLI flag 保持 spec 規定「不接受」。
- 不改動 `BuildAliasMap` 的函式簽章與內部 `AliasMap` 型別。
- 不調整 ephemeral_api 區段或其他 `shadowdns.yaml` 欄位。
- 不提供遷移工具（migration script）。使用者自行把 `backup: root` 行整理成 `root: [backups...]` 區塊。

## Decisions

### 選擇一對多 schema（`root: [backups]`）而非保留一對一

選擇：`aliases: map<root, list<backup>>`。

原因：
- 符合使用者心智模型：一個根域名天然會有多個備援域名，此寫法在 review 時能一眼看出每個 root 涵蓋哪些 backups。
- 與 ShadowDNS 在文件中描述 alias 概念的方式一致（「root + backups」而非「backup → root」）。
- 早期 `aliases.yaml` 即採此格式；社群部署如果曾經用過 `aliases.yaml`，會對這種結構較熟悉。

替代方案：
- 維持一對一 `backup: root`：每個 root 要重複寫多行，diff review 時無法直觀看出 root 涵蓋範圍，與使用者心智模型衝突。
- 混合格式（value 同時接受 string 與 list）：解析邏輯複雜，失去單一來源的優點。拒絕。

### 解析邏輯在 `shadowdnscfg.Load()` 內展開，不引入新 public API

選擇：`shadowdnscfg/config.go` 的 `rawConfig.Aliases` 改為 `map[string][]string`；在 `Load()` 內把 root→[backups] 倒轉成 `map[string]string`（backup→root），再傳給既有的 `config.BuildAliasMap`。倒轉時仍檢查「同一 backup 出現在兩個 root 下」的衝突，錯誤訊息引用原始 YAML 的 root key。

原因：
- `BuildAliasMap` 既有的正規化、重複偵測、self-alias 檢查、錯誤訊息格式都可以直接沿用，無需改動 signature 或行為。
- `internal/config/aliases.go` 的 `LoadAliases` 即將刪除，倒轉邏輯不需要額外放一個 public helper——直接展開在 loader 內 20 行內完成即可。

替代方案：
- 新增 `BuildAliasMapFromRoots(map[string][]string)` public helper：多一個 API surface 但沒有第二個呼叫者。違反 YAGNI。拒絕。
- 把 `BuildAliasMap` 簽章改為吃 `map[string][]string`：需要改動現存的 unit tests，並讓「同一個 backup 寫在同一個 root 底下兩次」與「同一 backup 在兩個 root 下」兩種狀況的錯誤訊息路徑糾纏。拒絕。

### 直接 breaking change，不提供 deprecation window

選擇：舊 `backup: root` 格式由 YAML decoder 直接拒絕（走正常 strict-decode 錯誤路徑，會產出 `yaml: unmarshal errors` 類型訊息指向該行）；`README.md` 的 migration note 直接改為最終形態，不提 deprecation 流程。

原因：
- 現行 `shadowdns.yaml` 格式是很新的 schema（`ephemeral-txt-api` change 帶入），已知部署只有 bench-ns2 測試環境。
- Deprecation window（兩格式並存 + warning）要多寫分支判斷與測試，對單一已知部署代價不划算。
- 使用者已經確認走方案 1（直接換）。

替代方案：
- 兩格式並存一個 release，發 deprecation warning：多 ~30 行程式與對應測試，僅為未知的外部使用者。拒絕。

Risk 由 Migration Plan 的操作流程承擔（先 push code → 立刻手動更新 bench-ns2 上的 `shadowdns.yaml`）。

### Integration tests 改用 `shadowdnscfg.Load()` 而非直餵 `BuildAliasMap`

選擇：integration tests 建立新 `testdata/integration/shadowdns.yaml` fixture，改呼叫 `shadowdnscfg.Load()` 讀取 `cfg.Aliases`。

原因：
- 5 個 integration test 檔案的定位是「端到端接近生產 wiring」。讓它們走 `shadowdnscfg.Load()` 跟生產程式碼 (`cmd/shadowdns/main.go`) 的 config 載入路徑一致，能在日後 `shadowdnscfg` 有變更時自動得到覆蓋。
- Fixture 用真實 YAML 格式也能順便驗證 YAML schema 本身可被解析。

替代方案：
- 讓 tests 直接呼叫 `config.BuildAliasMap(map[string]string{...})` 跳過 YAML：更快但失去 YAML schema 的 integration 覆蓋。拒絕。

### Spec 清理範圍限於本次 change 觸及的 Requirement

選擇：只合併 `config-loader/spec.md` 中兩份重複的 `Parse aliases.yaml` Requirement（L416-515 舊的、L800-827 新的）；不清理檔案內其他重複/過期結構。

原因：
- 本次 change 的 scope 是 aliases schema；動到其他 Requirement 會讓 change 膨脹。
- 其他重複 Requirement（若存在）應另開 change 處理。

## Risks / Trade-offs

- **[Risk]** bench-ns2（或任何已用新格式的部署）在 ShadowDNS 升級後 systemd 啟動失敗或 SIGHUP reload 拒絕新設定。→ **Mitigation**：Migration Plan 要求 push 新 binary 前先用新 schema 改寫現場 `shadowdns.yaml`；SIGHUP reload 本身設計即是「validation 失敗則保留前一個有效狀態」，最差情況不會讓 DNS 服務中斷。
- **[Risk]** 有未被發現的外部 script / 部署流程仍在依賴 `aliases.yaml` 獨立檔案。→ **Mitigation**：此情境的錯誤在現行 code 就已經存在（`cmd/shadowdns/main.go` 已不讀 `aliases.yaml`）。本次 change 不再惡化，只是正式移除 loader。
- **[Trade-off]** 使用者心智模型一致性 vs. 「改動已發佈 schema」成本：為了前者接受後者，代價由一份 README migration note 與一次性部署動作吸收。
- **[Risk]** Strict YAML decode 對使用者寫錯格式（例如把 list 寫成單一 string）的錯誤訊息品質取決於 `gopkg.in/yaml.v3` 預設行為，可能不夠友善。→ **Mitigation**：`shadowdnscfg.Load` 已包裝錯誤訊息為 `parsing config %q: %w`，涵蓋型別不符錯誤；額外使用者教育由 `packaging/shadowdns.yaml.example` 的 header 註解與 README 承擔。

## Migration Plan

1. 合併 PR 後，立刻到 bench-ns2 手動把 `/etc/shadowdns/shadowdns.yaml` 的 `aliases` 區段從 `backup: root` 行整理成 `root: [backups]` 區塊。
2. 執行 `release-shadowdns` skill 部署新 binary。新 binary 啟動時如果讀到舊格式會立即 fail 並保留前一版本 systemd unit 狀態。
3. Rollback 策略：若部署後發現問題，可先 `systemctl stop shadowdns`、把 `shadowdns.yaml` 還原為舊格式、降回前一版 binary；alias map 的內部結構不變，資料 shape 一致，不需要其他資料遷移。

## Open Questions

（無。方案 1 與格式選擇已由使用者確認。）
