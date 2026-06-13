## Context

`internal/config/zones.go` 的 `loadFile` 以頂層指令分派：目前只接受 `options`、`include`、`view`、`logging`，其餘（含頂層 `zone`）一律回傳 "unsupported directive" 致命錯誤。view 解析結果累積在 `Config.Views`，下游（`internal/server.BuildState`、`view.Matcher`、query log、AXFR/NOTIFY、prune-backup）全部以 `range cfg.Views` 消費，沒有任何「至少一個 view」的假設。BIND 的行為是：沒有任何 view 時，頂層 zone 自動歸入隱含的 `_default` view（match-clients 等同 any）正常服務；一旦定義了任何 view，所有 zone 都必須位於 view 內，混用是設定錯誤。

## Goals / Non-Goals

**Goals:**

- 無 view 的 named.conf（頂層 zone）可以啟動並正常服務，行為對齊 BIND 的隱含 `_default` view。
- view 與頂層 zone 混用時以致命錯誤拒絕啟動，錯誤訊息指出違規 zone 的檔案與行號。
- 合成行為完全收斂在 config-loader 內，下游程式碼零改動。

**Non-Goals:**

- 不放寬 GeoIP 載入條件：那是已立案、排定先行 apply 的 `geoip-optional` change 的範圍。本 change 受益於它——合成的 `_default` view 只含 `any` 規則，落入 geoip-optional 的「無 geo 規則免 `geoip-directory`/mmdb」分支，因此本 change 的測試與手冊都以 geoip-optional 已落地為前提撰寫。
- 不改變「無 view 也無 zone」設定的行為（維持啟動成功、全部 REFUSED）。
- 不實作 BIND 的其他 view 內隱含行為（如 `match-destinations`、per-view options 繼承）。
- 不調整 `--dry-run` 摘要格式（合成後 views 計數自然為 1，毋須特別標示）。

## Decisions

### 在 LoadNamedConf 後處理階段合成 _default view

頂層 zone 在解析期間先累積到 `Config` 的暫存欄位（例如非匯出的 top-level zones slice，或匯出欄位但文件註明僅供載入期使用），`LoadNamedConf` 在 `loadFile` 遞迴完成後做後處理：若 `len(Views) == 0 && len(topLevelZones) > 0`，合成 `View{Name: "_default", MatchClients: []MatchRule{AnyRule}, Zones: topLevelZones}` 附加到 `Views`。後處理必須在 `warnShadowedViews` 之前執行，維持「shadow 警告看到的是最終 view 清單」的不變式（單一合成 view 當下無差別，但順序是未來不能破壞的前提）。理由：混用偵測必須看到「整份設定（含所有 include）」的全貌才能做到宣告順序無關，邊解析邊判斷會漏掉「view 在後、zone 在前」或分散在不同 include 檔的情形。替代方案（在 `loadFile` 內逐檔判斷）被否決，因為 include 遞迴讓單檔視角不完整。

### 混用偵測為宣告順序無關的致命錯誤

後處理時若 `len(Views) > 0 && len(topLevelZones) > 0`，回傳錯誤，格式沿用既有解析錯誤慣例：`<source>:<line>: zone "<name>" declared at top level but <N> view(s) are defined; when any view is present all zones must be declared inside views`，其中 source/line 取第一個頂層 zone 的宣告位置。理由：BIND 對混用同樣是 named-checkconf 階段的硬錯誤；報第一個違規點足以讓使用者修正，不需列舉全部。

### 頂層 zone 重用 parseZone、合成 view 重用既有型別

頂層 `zone` 分支直接呼叫既有的 `parseZone`（含 type master 檢查、相對 file 路徑解析），不複製解析邏輯。這也意味著沿用 parseZone 的「解析期就地解析」語意：相對路徑以「解析到該 zone 當下已讀到的 options.directory」為基底，options 區塊宣告在 zone 之前才生效，否則退回宣告檔所在目錄——與 view 內 zone 的既有行為完全一致，不為頂層 zone 另設後處理路徑。合成的 `_default` 是普通的 `config.View` 值，`Line`/`Source` 取第一個頂層 zone 的位置以利日誌追溯；zone body 缺 `type` 或缺 `file` 的容忍行為與 view 內既有行為一致。頂層 zone 名稱重複維持與 view 內相同的容忍（不新增 fatal 驗證、所有條目都保留在合成 view 的 zone 清單中），但合成時對每個重複名稱輸出一條 Warn——列出該名稱所有宣告的 source:line、說明服務層以最後一筆宣告為準（BuildState 的 map 寫入後者覆蓋前者）。理由（Warn 而非 fatal/靜默）：BIND named-checkconf 對同 view 重複 zone 是硬錯誤，viewless 平鋪清單是最容易出現重名的形態，完全靜默的 last-wins 對遷移者是陷阱；但直接 fatal 會比照出 view 內重名也該 fatal 的一致性問題，超出本 change 範圍。理由（重用既有型別）：下游（BuildState、matcher、AXFR、query log、metrics）對 `_default` 一視同仁，不需特殊分支。可觀察的表面是 query log 的 view 欄位會輸出 `view _default:`、Prometheus metrics 的 view label 會出現 `_default` 值（zone 檔名由操作者的 `file` 指令決定，與 view 名稱無關，不受影響）。

### 不保留 _default 為保留字

使用者顯式宣告 `view "_default"` 時視為普通 view，不檢查、不警告。理由：合成只發生在「零 view 區塊」時,兩者不可能同時存在，名稱碰撞無從發生；加保留字檢查是多餘的相容性破壞。

## Implementation Contract

**行為**：

1. named.conf（或其 include）只含頂層 zone、無任何 view 區塊 → 啟動成功，所有頂層 zone 經由名為 `_default` 的 view 服務，任何來源 IP 的查詢都匹配該 view（等同 `match-clients { any; }`）；因 `_default` 不含 geo 規則，搭配先行的 geoip-optional，這類設定不需要 `geoip-directory` 與 mmdb。
1a. 頂層 zone 名稱重複 → 啟動成功（不 fatal），合成時每個重複名稱輸出一條 Warn，內容含 zone 名稱、該名稱全部宣告的 source:line、以及「服務時以最後一筆宣告為準」的說明。
2. 任一 view 區塊與任一頂層 zone 並存（不論順序、不論檔案）→ `LoadNamedConf` 回傳致命錯誤，訊息含第一個頂層 zone 的 source 路徑、行號、zone 名稱；`--dry-run` 同樣失敗。
3. 無 view 且無 zone → 行為與現行版本完全相同（啟動成功、查詢 REFUSED），不合成 `_default`。
4. 既有「全部 zone 都在 view 內」的設定 → 解析結果與現行版本位元等價（回歸不變）。

**介面／資料形狀**：`config.Config.Views` 介面不變；合成的 view 為 `View{Name: "_default", MatchClients: [AnyRule], Zones: <頂層 zone 依宣告順序>}`，MatchClients 使用與解析 `match-clients { any; };` 完全相同的規則型別。頂層 zone 的 `type` 限制與 view 內相同（僅 master）；`file` 相對路徑沿用 parseZone 解析期語意（options 在前以 options.directory 為基底，否則退回宣告檔目錄），與 view 內 zone 一致。

**失敗模式**：混用錯誤是 `LoadNamedConf` 的回傳錯誤（fatal，啟動與 SIGHUP reload 皆然——reload 失敗時沿用既有「保留舊設定繼續服務」行為）；頂層 zone 的型別／語法錯誤沿用 `parseZone` 既有錯誤。無新增的靜默降級。

**驗收**：`internal/config/zones_test.go` 新增測試——(a) 純頂層 zone 設定解析出單一 `_default` view 且 zone 順序保持、MatchClients 規則型別與解析 `any` 相同；(b) 混用（zone 前 view 後、view 前 zone 後、跨 include 檔）三種排列皆回錯誤且訊息含 source:line；(c) 空設定（只有 options）解析成功且 `len(Views) == 0`；(d) 頂層 zone 的相對 file 路徑解析（options 在前）與 view 內行為一致；(e) 重複頂層 zone 名稱解析成功、合成 view 保留全部條目、以 zap observer 斷言每個重複名稱恰一條 Warn 且訊息含所有宣告位置。`test/integration` 新增無 view 情境的端到端測試（沿用 geoip-optional 建立的跳過 GeoIP helper 變體，fixture 不含 geoip-directory 與 mmdb）：以合成 `_default` view 服務的 zone，對任意來源 IP 的 A 查詢取得權威回應（覆蓋 matcher／查詢路徑，dry-run 測不到這段）。`make test`、`make lint`、`make smoke` 通過；手動以無 view 的 named.conf 對 `--dry-run` 驗證摘要顯示 views=1。

**範圍邊界**：in scope = `internal/config/zones.go` 與其測試、`test/integration` 新增無 view 端到端測試、named-conf 手冊頁（雙語）、視檢視結果可能微調的 docs/index.md 與 README.md 功能描述。out of scope = internal/server、internal/view、cmd/shadowdns 的任何程式碼改動；GeoIP 載入條件；packaging 範例設定（維持示範 view 用法）；query log 對 `_default` 的抑制（見 Risks，留待後續 change 決定）。

## Risks / Trade-offs

- [query log 每行無條件輸出 `view _default:`，而 BIND 在無 view 設定下的查詢日誌不含 view 子句——從無 view BIND 遷移者的下游 log parser 可能因多出的欄位解析失敗] → 手冊 named-conf 頁明確記載此已知格式差異（含 metrics view label 會出現 `_default`）；是否抑制 `_default` 輸出留待後續 change，不在本次範圍。
- [頂層 zone 宣告在 options 區塊之前時，相對 file 路徑以宣告檔目錄而非 options.directory 解析（BIND 對指令順序不敏感）] → 與 view 內 zone 的既有解析語意一致，屬既有限制的延伸而非新行為；手冊以「options 區塊置於 zone 宣告之前」為建議寫法。
- [現有部署若 named.conf 誤含頂層 zone（先前會被 unsupported directive 擋下），升級後行為從「啟動失敗」變為「混用錯誤」或「開始服務」] → 訊息語意更精確；純頂層 zone 從失敗變為服務是本 change 的目的本身,屬預期行為變更（v0.x 實驗階段可接受）。
- [混用偵測只報第一個違規 zone，多個違規需多輪修正] → 與 BIND named-checkconf 單點報錯的體驗一致，可接受。
