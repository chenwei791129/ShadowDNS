## Context

GeoIP 強制載入只存在於 `cmd/shadowdns` 啟動層的兩道閘門：startup 與 SIGHUP reload 都以「`geoip-directory` 為空即致命錯誤」攔截（共用 `errGeoIPDirectoryUnset`），通過後無條件呼叫 `view.LoadGeoIP` 載入 Country + ASN 兩個 mmdb。底層沒有架構性依賴：`view.Matcher` 對 nil DB 已優雅降級（country/ASN 規則在 `Country`/`ASN` 為 nil 時回 no-match，不 panic），`server.BuildState` 只是把兩個 handle 塞進 Matcher，`geoipRuntime` 的 `closePrev`/`closeAll` 已有 nil 檢查，`Metrics.SetGeoIPInfo` 對 nil receiver 安全。唯二的 nil 不安全點：`SetGeoIPInfo` 的兩個呼叫端直接解參考 `Metadata().BuildEpoch`，以及 `SetGeoIPInfo` 收到空 map 時不會刪除前一代 series（geo 轉 no-geo 時 stale series 會殘留）。match-clients 規則型別（`CountryRule`/`ASNRule`/`IPRule`/`CIDRRule`/`AnyRule`）定義於 `internal/config`，規則值不帶 source 位置，但 `config.View` 帶 `Line`/`Source`。

## Goals / Non-Goals

**Goals:**

- 整份設定沒有任何 country/ASN match-clients 規則且 `geoip-directory` 未設時，啟動與 reload 都不需要 mmdb，Matcher 以 nil GeoIP DB 運作。
- 有 geo 規則但 `geoip-directory` 未設 → 致命錯誤點名第一個使用 geo 規則的 view 的 source 檔案與行號。
- `geoip-directory` 有設 → 行為與現行完全相同（強制載入、驗證、失敗即 fatal/keep-old）。
- reload 的 geo↔no-geo 轉換態行為明確：新增 geo 規則時當場載 mmdb（失敗 keep-old）、移除時 handle 走既有 deferred-close 世代輪替、metrics series 同步刪除。

**Non-Goals:**

- 不改變 Matcher 的規則評估語意（nil DB 時 country/ASN 規則 no-match 是既有行為，本 change 只是讓它成為合法生產狀態）。
- 不放寬「directory 有設但 mmdb 缺失／損壞」的 fatal 行為——這是防 typo 靜默降級的安全線。
- 不引入 lazy loading 或部分載入（只載 Country 不載 ASN）：兩個 mmdb 仍是全有或全無。
- 不調整 `view.LoadGeoIP` 本身的候選檔名鏈與驗證邏輯。
- 不處理 parked change implicit-default-view 的合成 view（正交；該 change 的 `_default` 只含 any 規則，自動落入本 change 的無 geo 規則分支）。

## Decisions

### GeoIP 需求判定：directory 有設就載入，未設才掃描 geo 規則

判定規則（啟動與 reload 共用）：`geoip-directory` 有設（非空字串）→ 一律呼叫 `LoadGeoIP`，失敗即錯（與現行相同）；未設 → 掃描所有 view 的 MatchClients，存在任何 `CountryRule`/`ASNRule` 即回致命錯誤，否則以 nil DB 繼續。「未設」定義為選項缺席**或值為空字串**，兩者行為相同——解析器把 `geoip-directory` 存成普通字串（`OptionsBlock.GeoIPDirectory`），無法區分 `geoip-directory "";` 與完全沒寫，且現行程式碼本來就把空字串當未設攔下（`errGeoIPDirectoryUnset`）；空字串 + geo 規則仍走「明確設定錯誤」分支，保留 sighup-reload spec 原有「空 directory 必須是明確設定錯誤、而非相對路徑開檔錯誤」的失敗形狀保證。理由：「有設就載入」保留操作者意圖的明確語意，且避免「目錄打錯欄位名導致整組 geo 規則靜默永不匹配」——錯字會落到「有規則但未設 directory」分支被攔下。替代方案「只在有 geo 規則時才載入（directory 設了也忽略）」被否決：mmdb 更新與 SIGHUP 重載的既有運維流程會因規則暫時移除而中斷，且 dry-run 驗證 mmdb 可用性的能力消失。

### geo 規則掃描 predicate 放在 internal/config 與規則型別同檔

新增 `config.FirstGeoRuleView(views []View) (viewName, source string, line int, found bool)`（或等價形狀）於 internal/config/match.go，以型別 switch 掃描所有 view 的 MatchClients，回報第一個含 `CountryRule` 或 `ASNRule` 的 view 的名稱與 `View.Line`/`View.Source`。理由：規則型別定義在這裡，新增規則型別時 predicate 在同一檔案內，遺漏更新的風險最低；規則值本身不帶位置資訊，view 層級的 source:line 已足夠定位（一個 view 的 match-clients 區塊不會大到難以人工掃視）。錯誤訊息格式沿用解析錯誤慣例：`<source>:<line>: view "<name>" uses geoip match-clients rules but geoip-directory is not set in named.conf options`。條件式三分支邏輯與這條錯誤訊息的組裝收斂在 cmd/shadowdns 的**單一共用 helper**（形如 `loadGeoIPIfRequired(cfg, logger) (*view.CountryDB, *view.ASNDB, error)`），啟動與 reload 兩個呼叫點都只呼叫它——避免三分支寫兩份後各自漂移（startup 接受、reload 拒絕同一份設定的分歧正是共用語意決策要防的）。替代方案「把 geo 規則需 directory 的檢查下沉到 `config.LoadNamedConf`（緊鄰 `warnShadowedViews` 的後處理）」被否決：`prune-backup` 子命令同樣經由 `LoadNamedConf` 載入設定，但它完全不需要 GeoIP，下沉會讓離線工具無端繼承伺服器的 GeoIP 驗證語意；解析層維持純解析。

### 啟動與 reload 共用同一條件式載入語意

reload 路徑把現行「空 directory 即失敗 + 無條件 LoadGeoIP」替換為與啟動相同的條件式判定。轉換態因此自然成立：(a) no-geo → geo（reload 後設定新增 geo 規則與 directory）：條件式判定走「有設→載入」分支，載入失敗即 reload 失敗 keep-old，成功則新 handle 進 BuildState、舊的（nil）無需輪替；(b) geo → no-geo（規則與 directory 一併移除）：判定通過後以 nil DB 建新 state，被取代的舊 handle 照既有機制旋入 `geoipRuntime` 的 deferred-close 槽位，於下次 reload 或 shutdown 釋放（`closePrev`/`closeAll` 的 nil 檢查已涵蓋 nil 新值）。理由：單一語意、零特例，轉換態不需要新的狀態機；替代方案「reload 禁止 geo 啟用狀態改變」被否決，因為它把一個可以自然支援的操作變成人為限制。

### SetGeoIPInfo 把傳入 map 視為完整期望集合

`Metrics.SetGeoIPInfo` 修改為：對 `prevGeoIPLabels` 中存在、但本次 `buildEpochs` 缺席的 database，刪除其 series 並移出追蹤 map。呼叫端在無 GeoIP 時傳空 map（不是不呼叫），nil 解參考問題隨之消失——`Metadata().BuildEpoch` 只在 handle 非 nil 的分支讀取。理由：宣告式「期望集合」語意讓 geo→no-geo 轉換的 series 清理不需要呼叫端記得另外刪除；與既有同 database 換 build_time 時刪 stale series 的差分更新模式一致。替代方案（新增獨立的 `ClearGeoIPInfo` 方法）被否決：兩個方法要維持呼叫順序契約，比一個冪等方法容易誤用。

### ECS 無 GeoIP 時啟動 Warn 不 fatal

`--ecs-enable` 且 GeoIP 未載入時輸出一條 Warn：ECS 位址僅供 country/ASN 查找使用，無 DB 時 ECS 對 view 選擇無作用、僅保留 option echo 行為。觸發時機是「每次設定載入完成後的結果狀態」——啟動如此，**SIGHUP reload 完成後若新狀態為 ECS 啟用且無 GeoIP 也同樣輸出一次**（`--ecs-enable` 是 process 級 flag，geo→no-geo 的 reload 會讓 ECS 在執行中途靜默失效，正是這條 Warn 要揭露的情境）。不 fatal 的理由：這是無害的組合（行為正確、只是無效果），fatal 會把「先開 ECS、之後再補 geo 規則」的漸進設定流程變成錯誤；對齊既有「view 內 rate-limit 警告但不擋啟動」的遷移友善慣例。

### 啟動與 reload 摘要日誌補 geoip_enabled 欄位

「shadowdns ready」、「reload complete」與 `--dry-run` 的「dry-run: configuration loaded successfully」三條 Info 日誌各補一個 `geoip_enabled` 布林欄位。理由：無 GeoIP 是新的合法狀態，操作者需要一眼能從日誌確認當前模式。dry-run 必須單獨列出，因為它在 ready 日誌之前就以自己的摘要日誌提早返回，不會經過 ready 日誌——「走同一條啟動路徑」只保證 GeoIP 閘門的成敗一致，不保證日誌欄位自動出現。

## Implementation Contract

**行為**：

1. 設定無任何 country/ASN 規則且 `geoip-directory` 未設（缺席或空字串）→ 啟動成功（不需任何 mmdb 檔），查詢按 any/IP/CIDR 規則正常分流，ready 日誌含 `geoip_enabled=false`；`--dry-run` 同樣成功，其摘要日誌含 `geoip_enabled=false`。
2. 設定含任何 country/ASN 規則但 `geoip-directory` 未設（含 `geoip-directory "";`）→ 啟動與 `--dry-run` 失敗，錯誤訊息含第一個使用 geo 規則的 view 名稱、source 路徑、行號（明確設定錯誤，絕不是相對路徑開檔錯誤）；SIGHUP reload 遇同樣設定則失敗 keep-old。
3. `geoip-directory` 有設（非空）→ 與現行版本行為完全一致（含 GeoIP2→GeoLite2 候選鏈、mmdb 缺失／損壞的 fatal、reload 失敗 keep-old、`shadowdns_geoip_db_info` series 輸出）。
4. reload no-geo → geo：成功後查詢立即使用新 mmdb 分流，`geoip_db_info` series 出現，reload complete 日誌含 `geoip_enabled=true`；mmdb 載入失敗則 reload 失敗、舊（無 GeoIP）狀態續行。
5. reload geo → no-geo：成功後 `geoip_db_info` 全部 series 消失，reload complete 日誌含 `geoip_enabled=false`，被取代的 mmdb handle 不立即關閉（沿用 deferred-close 世代輪替），下次 reload 或 shutdown 時釋放。
6. 設定載入完成（啟動或 reload）後的結果狀態為 `--ecs-enable` 且 GeoIP 未載入 → 該次載入輸出 Warn 一次，服務照常。

**介面／資料形狀**：internal/config 新增掃描 predicate（輸入 `[]View`，輸出第一個 geo 規則 view 的名稱/source/line 與是否存在）；`Metrics.SetGeoIPInfo` 簽名不變，語意改為完整期望集合（空 map = 清空所有 geoip_db_info series）；`view.Matcher`、`server.BuildState`、`view.LoadGeoIP` 介面與行為皆不變。

**失敗模式**：geo 規則缺 directory 是 `run()`/reload 的回傳錯誤（fatal；reload 沿用 keep-old）；directory 有設時的载入錯誤沿用既有 `LoadGeoIP` 錯誤。無新增靜默降級——唯一的「規則不匹配」靜默情境（nil DB + country/ASN 規則）已被啟動閘門排除。

**驗收**：internal/config 測試覆蓋 predicate（無 geo 規則、country 規則、ASN 規則、多 view 取第一個、空 view 清單）；internal/metrics 測試覆蓋空 map 刪除全部 series、部分缺席只刪缺席者、同 database 換 build_time 仍只留一條 series；cmd/shadowdns 測試覆蓋行為 1、2（含錯誤訊息斷言與 `geoip-directory "";` 空字串案例、dry-run 成功與失敗兩個方向）與 reload 兩個轉換態（行為 4、5，含 metrics series 與 reload 日誌欄位斷言、ECS Warn 斷言）；test/integration 新增無 geo 規則、無 mmdb 的端到端測試（啟動 + IP/CIDR 分流查詢取得權威回應；既有 helper 無條件產生 mmdb 並載入，需要提供跳過 GeoIP fixture 的變體）。`make test`、`make lint`、`make smoke` 通過；`make docs-build` 通過。

**範圍邊界**：in scope = cmd/shadowdns/main.go 的啟動與 reload 條件式載入（含共用 helper）、internal/config 的 predicate、internal/metrics 的 SetGeoIPInfo 語意、internal/view/matcher.go 的註解修正、test/integration/helpers_test.go 的無 GeoIP fixture 變體、上列測試與手冊頁。out of scope = `view.LoadGeoIP` 內部邏輯、Matcher 規則評估、implicit-default-view 的任何內容、packaging 範例設定（維持示範 GeoIP 用法）。

## Risks / Trade-offs

- [操作者誤以為「拿掉 geoip-directory 就能停用分流」，但設定裡還有 geo 規則 → 啟動失敗] → 這是刻意的 fail-fast：錯誤訊息直接點名規則所在 view 的 source:line，修正路徑明確。
- [監控面板假設 `shadowdns_geoip_db_info` series 恆存在，無 GeoIP 部署會查無資料] → 手冊 geoip 頁記載「未載入時無此 series」；這正是該 metric 語意上正確的表達方式。
- [未來新增的 geo 類 match 規則型別若漏更新 predicate，會出現「有規則但免 mmdb 啟動、規則永不匹配」的靜默降級] → predicate 與規則型別同檔（internal/config/match.go），並在 predicate 的型別 switch 加註解要求新增規則型別時同步檢視；predicate 單元測試逐型別列舉。
- [reload 在 geo↔no-geo 之間反覆切換時，deferred-close 槽位邏輯面對 nil 混合世代] → `closePrev`/`closeAll` 既有 nil 檢查已涵蓋；reload 轉換態測試明確覆蓋連續切換。

## Migration Plan

向後完全相容：所有現行可啟動的設定（`geoip-directory` 必然有設）行為位元等價，無 CLI flag 或設定欄位變更，部署即生效、rollback 即還原。v0.x 實驗階段，無需漸進開關。

## Open Questions

（無）
