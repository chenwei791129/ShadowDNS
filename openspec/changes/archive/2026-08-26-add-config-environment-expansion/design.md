## Context

目前 `shadowdnscfg.Load` 直接以 `yaml.Decoder` 和 `KnownFields(true)` 將 `shadowdns.yaml` 解碼為內部 raw structs，再執行 alias、Ephemeral API 與 DoH 的語意驗證。啟動、`--dry-run`、prune-backup 子命令與 SIGHUP reload 都呼叫同一 loader；其中 reload 在所有可能失敗的工作完成後才交換 server state。

新增環境變數展開會跨越 YAML parsing、strict decoding、語意錯誤呈現與 reload 生命週期，且 bearer token 的空字串代表停用驗證。因此實作必須避免 raw-byte substitution、缺值降級與錯誤訊息洩密。

## Goals / Non-Goals

**Goals:**

- 在所有 `shadowdnscfg.Load` callers 上提供一致的 `${NAME}`、`${NAME:-default}` 與 `$$` escape 語意。
- 只展開 YAML value-side 的 string scalar，讓任意環境值仍然是單一 scalar data。
- 保留 `KnownFields(true)`、現有 custom unmarshal、現有 multi-document 行為與全部語意驗證。
- required 變數在 unset 或空字串時 fail closed。
- expression 錯誤與任何使用 environment-derived value 後發生的 load/reload validation error 都不揭露環境值。
- 維持 SIGHUP reload 在失敗時保留既有執行狀態的原子性。

**Non-Goals:**

- 不支援 `$NAME`、`${NAME-default}`、`${NAME:?message}`、command substitution 或完整 shell parameter expansion。
- 不支援 recursive expansion；環境值與 default 中的 expression-like 文字只作為 literal data。
- 不展開 mapping key、YAML tag、alias node 或非字串 scalar；帶 anchor metadata 的 value-side string scalar 仍是一般值並會展開。
- 不新增 top-level `env:` section、file-backed Secret reader、Secret polling 或自動 Pod restart。
- 不承諾 SIGHUP 能讀到 Kubernetes 更新後的 env-backed Secret；Pod process environment 不會因此改變。
- 不把所有可展開欄位都視為 Secret，也不改造成功啟動後既有的 listener、path、IP 等 operational logging；Secret 只應注入不會被正常記錄的欄位，例如 `ephemeral_api.token`。

## Decisions

### 使用兩階段 YAML decode 保留資料邊界與 strict decoding

第一階段以 `yaml.Decoder` 解碼第一份 document 至 `yaml.Node`，走訪 tree 並只修改符合以下條件的節點：節點位於 mapping value 或 sequence element、`Kind == yaml.ScalarNode`、`Tag == "!!str"`。Mapping key 與 `AliasNode` 完全跳過；document、mapping 與 sequence 結構不增刪節點。帶 anchor metadata 的 value-side string scalar 依一般 scalar 規則展開；第二階段解碼時，引用它的 alias 取得相同展開值。

處理後以 `yaml.Encoder` 將第一份 node 寫入記憶體 buffer，再以第二個 `yaml.Decoder`、`KnownFields(true)` 解碼至既有 `rawConfig`。這保留 unknown-field rejection 及 `rawAliasGroup.UnmarshalYAML` 的既有行為。

不採 raw YAML bytes replacement，因為包含換行、colon、comment marker、document marker 或 YAML tag-like text 的環境值可能改變文件結構。不直接呼叫 `yaml.Node.Decode(&raw)`，因為它無法套用 parent decoder 的 `KnownFields(true)`。

現行 loader 只解碼第一份 YAML document 並忽略後續 documents。本變更維持該行為：第一階段只取得第一份 document，重新編碼及 strict decode 也只處理該 document，不新增 multi-document validation。

### 以專用單次 parser 定義有限 expression grammar

`internal/shadowdnscfg/envexpand.go` 提供 package-private expression parser 與 YAML traversal helper。Parser 由左至右掃描每個 scalar：

- `${NAME}`：以 `os.LookupEnv` 取得非空值；unset 或 empty 回傳錯誤。
- `${NAME:-default}`：環境值 unset 或 empty 時使用 literal default；否則使用環境值。
- `$$`：輸出單一 literal `$`，且後續文字不在同一次處理中重新掃描；因此 `$${NAME}`、`$${NAME:-fallback}` 與 `$${NAME:?message}` 分別保留 literal `${NAME}`、`${NAME:-fallback}` 與 `${NAME:?message}`。
- `$NAME` 與不構成上述 grammar 的一般 `$` 保持 literal。
- `NAME` 必須符合 `[A-Za-z_][A-Za-z0-9_]*`。
- 未終止 expression、空名稱、unsupported operator 或 invalid name 回傳含變數名稱（若可安全辨識）及 YAML line/column 的錯誤。

Default 截至第一個 `}`；default 內的 expression-like text 不遞迴處理。Parser 將尚未消費的後續文字照常接回輸出，因此 `${PRIMARY:-${SECONDARY}}` 在 `PRIMARY` unset 時得到 literal `${SECONDARY}`。這是刻意有限的設定語法，不模擬 shell quoting、nested-brace parsing 或 operator semantics。

不採 `os.ExpandEnv`，因為它將 missing variable 變成空字串、不支援本變更的 fail-closed 與 escape contract，也無法產生安全且帶位置的錯誤。

### 在 loader 邊界使用 fail-safe validation diagnostics

Traversal 記錄每個 environment expression 的安全變數名稱與 YAML path／line／column，但不把 environment value 放入 diagnostics。Required/default lookup 錯誤只回報變數名稱與位置。

Loader 先對未展開的 node 執行既有 strict structural decode，以安全地保留 unknown-field 與 type errors；expression 仍是 string scalar，不妨礙結構驗證。展開後再 strict decode 並執行 semantic validation。只要本次確實使用了 non-empty environment value，第二次 decode 或 semantic validation 若失敗，loader 不回傳或記錄原始 downstream error，改回傳 fail-safe diagnostic：指出 expanded configuration validation failed、涉及的變數名稱與 YAML paths，但不包含原始值、展開後 scalar、正規化衍生值或 wrapped cause。

這比事後以字串替換 raw／quoted／escaped values 更可靠：alias、DNS 或 URL validators 可能 case-fold、canonicalize 或以其他形式轉換輸入，字串 redactor 無法列舉所有衍生表示。代價是 environment-backed invalid config 的錯誤細節較少；操作者可用安全的變數名稱與 YAML path 定位設定。

成功的 `Load` 必須在回傳 `Config` 中包含展開值，否則功能無法運作。本安全契約只涵蓋 expression/load/reload failure diagnostics，不宣稱成功後所有 callers 都保留 provenance 或禁止記錄非秘密 operational values。文件明確要求 Secret 只注入 `ephemeral_api.token` 等不會被正常記錄的欄位。

### 保持 Load API 與 reload transaction boundary 不變

`Load(path string, logger *zap.Logger) (*Config, error)` signature 不變，environment lookup 由每次呼叫建立的新 resolver 執行。啟動、`--dry-run`、prune-backup 與 SIGHUP reload 不需新增 expansion 分支。

測試 helper 可接受 injected lookup function，以便 deterministic unit tests；production `Load` 固定使用 process environment。SIGHUP 的 `reload` 仍在 `srv.SwapState` 前呼叫 loader，因此 expansion 或後續驗證失敗沿用既有 failure metric、error logging 與 old-state preservation。

Alias map 等既有 reloadable server state 會使用重新展開後的值。Ephemeral API 與 DoH listeners/config objects 仍維持現有 startup-scoped 行為：SIGHUP 會重新展開並驗證其設定，但不替換已啟動的 API/DoH server；既有 drift/restart 語意不因本變更擴大。

### 以設定頁、Feature Guide 與 Operations Guide 說明操作契約

雙語 `shadowdns.yaml` 設定頁記錄 grammar、escape、fail-closed、非遞迴與 fail-safe diagnostics。新增雙語 Feature Guide，集中說明 process environment、Kubernetes Secret 注入與安全欄位選擇。新增雙語 Operations reload 頁，區分 config-file SIGHUP reload 與 Pod process environment。

所有新頁同步加入 `mkdocs.yml` 的英文 nav 與中文 `nav_translations`。範例只使用 synthetic token、RFC 2606 domains 與 RFC 5737 addresses。

文件明確指出：更新 Kubernetes Secret 不會修改既有 container process environment，env-backed Secret 變更需要 rollout restart；單獨發送 SIGHUP 不會取得更新後的 Secret。

## Implementation Contract

**Observable behavior**

- 任一 `shadowdns.yaml` value-side string scalar 或 string sequence element 可使用支援的 expression grammar。
- `${NAME}` 在 non-empty environment value 時替換；unset 或 empty 時整份設定載入失敗。
- `${NAME:-default}` 在 unset 或 empty 時使用 literal default，在 non-empty 時使用 environment value。
- `$$` escape 一個 dollar sign，可保留任意 literal braced text；同一 scalar 中多個 expression 依序處理一次。
- Mapping keys、non-string scalars 與 YAML structure 的解析結果不因 expansion 改變；anchored value scalars 會展開，alias references 取得相同值。
- YAML 無 expression 時，包含後續 YAML documents 被忽略的既有行為在內，回傳設定與目前 loader 相同。

**Interface and data shape**

- Public Go API 仍為 `shadowdnscfg.Load(path, logger)`，CLI 仍使用現有 `--config` 與 `--dry-run` flags。
- 設定檔新增的唯一 surface 是 value-side string grammar；不新增 YAML 欄位。
- Package-private helper 接受 scalar text、YAML location 與 lookup callback，回傳 expanded text、使用的安全變數名稱與 error；YAML traversal 聚合 paths 供 fail-safe validation diagnostics。

**Failure modes**

- Malformed、unsupported、unset-required 或 empty-required expression 使 load 失敗，錯誤包含安全的變數名稱與 YAML position。
- Expansion 後的值仍由現有 validators 拒絕 invalid CIDR、IP、URL、host/port 或 path。
- 使用 environment value 後的 downstream decode/semantic error 只回報安全變數名稱與 YAML paths，不包含 downstream cause 或任何 environment-derived representation。
- SIGHUP reload 失敗時不交換 server state、不清空 ephemeral store，並記錄一次 reload failure。

**Acceptance criteria**

- `go test` 覆蓋 parser grammar、真實 YAML anchor/alias graph、YAML traversal、strict decode、既有 multi-document 行為、結構注入防護、fail-safe diagnostics、dry-run、prune-backup 與 reload failure preservation。
- `go fmt`、`make lint`、`make test` 與 `make docs-build` 全部通過。
- 以 `${API_TOKEN}` 執行 dry-run 時，non-empty value 成功；unset 或 empty value 以非零狀態結束，輸出只含 `API_TOKEN` 而不含其值。

**Scope boundaries**

- In scope：unified ShadowDNS YAML 的 string values、所有既有 `shadowdnscfg.Load` callers、雙語設定／Feature／Operations 文件與對應 tests。
- Out of scope：`named.conf`、zone files、CLI flag value expansion、runtime Secret refresh、成功後全域 provenance tracking、所有 operational log 的全面遮罩、shell-compatible grammar及新的設定來源。

## Risks / Trade-offs

- [重新編碼 node 可能改變 YAML 呈現格式] → 重新編碼只存在記憶體且不覆寫來源檔；測試鎖定 semantic result 與 strict rejection，而非 formatting。
- [Fail-safe diagnostic 隱藏 downstream validator 細節] → 保留變數名稱與 YAML path 供定位，安全優先於回顯可能經轉換的環境內容。
- [Anchored scalar 展開會讓 alias reference 同步取得展開值] → 以真實 anchor/alias graph 測試鎖定此一致語意，文件明確說明。
- [Literal `${...}` 的既有設定在升級後變成 expression] → 通用 `$$` escape 可保留任意 braced text；沒有 `$` expression 的設定完全相容。
- [使用者誤認 SIGHUP 可刷新 Kubernetes Secret] → Operations Guide 以明確 restart 範例和限制說明區分 config-file reload 與 process environment。
