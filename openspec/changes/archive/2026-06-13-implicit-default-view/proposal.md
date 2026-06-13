## Why

ShadowDNS 目前要求所有 zone 都宣告在 `view` 區塊內：named.conf 出現頂層 `zone` 指令會被解析器以 unsupported directive 拒絕，而完全沒有 view 的設定雖能啟動，卻會對所有查詢回 REFUSED。這與 BIND 的行為不相容——BIND 在沒有任何 view 時會把頂層 zone 放進隱含的 `_default` view 正常服務（不使用 view 是 BIND 最常見的配置形態）。為了讓不使用 GeoIP 分流的 BIND 設定可以直接遷移到 ShadowDNS，解析器需要補上這個 BIND 相容行為。本 change 與已立案的 `geoip-optional`（排定先行 apply）互補：geoip-optional 使「無 country/ASN 規則時免 `geoip-directory` 與 mmdb」成立，而本 change 合成的 `_default` view 只含 `any` 規則、天然落入該分支——兩者皆完成後，無 GeoIP 的 BIND viewless 設定才能真正零額外準備地直接遷移。

## What Changes

- named.conf（含 include 檔）允許頂層 `zone "<domain>" { type master; file "<path>"; };` 宣告。
- 當整份設定（named.conf + 所有 include）沒有任何 `view` 區塊且存在至少一個頂層 zone 時，config-loader 合成一個隱含 view：名稱為 `_default`、match-clients 等同 `{ any; }`、包含所有頂層 zone（保持宣告順序）。合成後的 view 走既有的 view 流程（BuildState、matcher、query log、AXFR），不需下游程式碼改動；`_default` 名稱會出現在 query log 的 view 欄位與 Prometheus metrics 的 view label（於手冊記載為已知表面差異）。
- 當設定中同時存在任何 `view` 區塊與頂層 zone 宣告時（無論宣告順序、無論分散在哪些檔案），config-loader 回傳致命錯誤，指出頂層 zone 的檔案與行號——對齊 BIND「一旦使用 view，所有 zone 都必須在 view 內」的規則。
- 合成 `_default` view 時，對重複的頂層 zone 名稱每名輸出一條 Warn log（列出該名稱所有宣告位置、說明服務時以最後一筆為準），維持既有 last-wins 容忍、不 fatal——BIND named-checkconf 對同 view 重複 zone 會報錯，viewless 平鋪 zone 清單正是最容易出現重名的形態，靜默 last-wins 對遷移者是陷阱。
- 沒有 view 也沒有任何 zone 的設定維持現狀：啟動成功、所有查詢 REFUSED（不合成空的 `_default`）。
- 補上對應的單元測試（零 view + 頂層 zone、混用錯誤、零 view 零 zone）與一個無 view 情境的端到端整合測試（驗證合成 view 走通 matcher 與查詢路徑），並更新手冊中 named.conf 結構說明。

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `config-loader`: 修改「Parse view and zone declarations from master.zones」以接受頂層 zone 宣告（zone-body 語法規則與 view 內相同）；新增「Synthesize implicit _default view for viewless configurations」與「Reject mixing of top-level zones and view blocks」兩條 requirement。既有「Reject unsupported named.conf directives at startup」（type slave/forward 等拒絕）不變。

## Impact

- Affected specs: `config-loader`
- Affected code:
  - Modified: internal/config/zones.go（頂層 zone 解析、混用偵測、`_default` 合成）
  - Modified: internal/config/zones_test.go（新增零 view、混用、空設定測試）
  - Modified: docs/configuration/named-conf.md、docs/configuration/named-conf.zh.md（結構說明補上無 view 形態與混用限制）
  - Modified（視檢視結果，可能無需變更）: docs/index.md、README.md（功能可用性描述若提及 view 必須性則同步）
  - New: test/integration/viewless_test.go（無 view 端到端查詢測試）
  - Removed: （無）
- 不影響 internal/server、internal/view 的程式碼：合成的 `_default` 對下游而言是一個普通 view。
- Apply 順序：排定於 `geoip-optional` 之後——其 task 3.1 會建立「跳過 GeoIP 的整合測試 helper 變體」供本 change 的無 view 整合測試沿用，且 dry-run 驗證 fixture 因此不需產生 mmdb。
