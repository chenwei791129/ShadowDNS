## Why

ShadowDNS 目前無條件要求 `geoip-directory` 與兩個 mmdb 檔（Country + ASN）才能啟動，即使整份設定沒有任何 `geoip country` / `geoip asnum` 規則也一樣。這使得不做 GeoIP 分流的 BIND 設定（純 `any`／IP／CIDR 的 match-clients，或與 parked change `implicit-default-view` 互補的無 view 平鋪設定）無法直接遷移——使用者被迫去準備他們的設定完全用不到的 MaxMind 資料庫。Matcher 層其實已天然支援 nil GeoIP DB（country/ASN 規則查無資料時回 no-match），強制載入只是啟動層的閘門，沒有架構上的必要。

## What Changes

- GeoIP 載入需求改為條件式：整份設定（named.conf + 所有 include）的所有 view 的 match-clients 都沒有 country/ASN 規則、且 `geoip-directory` 未設定（缺席或空字串，兩者等同）時，跳過 mmdb 載入，Matcher 以 nil GeoIP DB 運作（country/ASN 規則本來就不存在，行為無差異）。
- 設定中存在任何 country/ASN 規則但 `geoip-directory` 未設定 → 維持致命錯誤，且錯誤訊息點名第一個使用 geo 規則的 view 的 source 檔案與行號（取代現行不分青紅皂白的 `geoip-directory not set`）。
- `geoip-directory` 有設定時行為完全不變：照舊強制載入並驗證兩個 mmdb，失敗即啟動失敗——避免目錄打錯字時靜默退化成「geo 規則永遠不匹配」。
- SIGHUP reload 套用與啟動相同的條件式判斷，自然涵蓋兩個轉換態：reload 後新增 geo 規則（需當場載入 mmdb，失敗則 keep-old）、geo 規則與 geoip-directory 一併移除（成功 reload 後 GeoIP handle 走既有的 deferred-close 世代輪替釋放）。
- 未載入 GeoIP 時，`shadowdns_geoip_db_info` metric 不輸出任何 series；reload 從有 GeoIP 轉為無 GeoIP 時刪除既有 series（`SetGeoIPInfo` 需把傳入的 map 視為完整期望集合，補上刪除缺席 database 的行為）。
- `--ecs-enable` 且 GeoIP 未載入時輸出 Warn（ECS 位址只影響 country/ASN 查找，無 DB 時 ECS 對 view 選擇無作用、僅剩 echo 行為）——啟動與「reload 後進入該狀態」皆觸發。
- 「shadowdns ready」、「reload complete」與 dry-run 摘要三條日誌各補一個 `geoip_enabled` 布林欄位（dry-run 在 ready 日誌前提早返回，需單獨補）。
- 手冊更新：geoip 設定頁與 named-conf 頁的 `geoip-directory` 欄位說明改為「僅在使用 geo 規則時必填」，ECS 指南補「無 GeoIP 時 ECS 無作用＋Warn」一節，CLI 參考的 SIGHUP 說明改為條件式，並檢視 getting-started、migration、index 比較表（雙語）與 README 是否有「GeoIP 必要」的描述需要同步。

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `view-matcher`: 「Fail startup when GeoIP databases are missing or unreadable」改為條件式（僅在設定含 geo 規則或 geoip-directory 有設時適用）；新增「無 geo 規則時以 nil GeoIP DB 運作」的 requirement。
- `sighup-reload`: 「GeoIP databases are reloaded on SIGHUP」改為條件式，明定 geo 規則新增／移除兩個轉換態的行為與失敗語意。
- `prometheus-metrics`: 「Expose GeoIP database metadata」補上未載入 GeoIP 時不輸出 series、轉換態時刪除 stale series 的行為。

## Impact

- Affected specs: `view-matcher`、`sighup-reload`、`prometheus-metrics`
- Affected code:
  - Modified: cmd/shadowdns/main.go（啟動與 reload 的條件式 GeoIP 驗證／載入、SetGeoIPInfo 呼叫點的 nil 處理、ECS Warn、摘要日誌欄位）
  - Modified: internal/config/match.go（新增掃描 views 是否含 country/ASN 規則的 predicate，回報第一個使用 geo 規則的 view 位置）
  - Modified: internal/config/match_test.go（predicate 單元測試）
  - Modified: internal/metrics/metrics.go（SetGeoIPInfo 刪除缺席 database 的 series）
  - Modified: internal/metrics/metrics_test.go（series 刪除行為測試）
  - Modified: internal/view/matcher.go（僅註解：nil DB 在無 geo 規則設定下是合法生產狀態）
  - Modified: cmd/shadowdns/main_test.go（無 geo 規則免 mmdb 啟動、有 geo 規則缺 directory 的錯誤訊息測試）
  - Modified: cmd/shadowdns/main_reload_test.go（兩個轉換態的 reload 測試）
  - Modified: test/integration/helpers_test.go（既有 fixture helper 無條件產生 mmdb 並呼叫 view.LoadGeoIP，需提供跳過 GeoIP 的變體供無 GeoIP 測試使用）
  - Modified: docs/configuration/geoip.md、docs/configuration/geoip.zh.md（條件式需求說明）
  - Modified: docs/configuration/named-conf.md、docs/configuration/named-conf.zh.md（geoip-directory 欄位說明）
  - Modified: docs/guides/ecs.md、docs/guides/ecs.zh.md（補「無 GeoIP 時 ECS 對 view 選擇無作用、啟動／reload Warn」一節——現行內容假設 GeoIP 恆載入）
  - Modified: docs/reference/cli.md、docs/reference/cli.zh.md（SIGHUP 信號說明「re-reads the GeoIP mmdb files」與 `--named-conf` 的 geoip-directory 描述改為條件式）
  - Modified（視檢視結果，可能無需變更）: docs/getting-started.md、docs/getting-started.zh.md、docs/migration.md、docs/migration.zh.md、docs/index.md、docs/index.zh.md、README.md（「GeoIP 必要」描述同步）
  - New: test/integration/geoip_optional_test.go（無 geo 規則、無 mmdb 的端到端啟動與查詢測試）
  - Removed: （無）
- 與 parked change `implicit-default-view` 正交互補：該 change 的合成 `_default` view 只含 `any` 規則，自動落入本 change 的「無 geo 規則」分支，兩者皆完成後「無 GeoIP 的 BIND viewless 設定直接遷移」才完整成立。
