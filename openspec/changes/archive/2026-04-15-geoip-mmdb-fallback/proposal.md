## Why

MaxMind 付費版 GeoIP2 資料庫與免費版 GeoLite2 使用不同的檔名（`GeoIP2-Country.mmdb` vs `GeoLite2-Country.mmdb`），但 ShadowDNS 目前只支援硬編碼的 GeoLite2 檔名，付費版使用者無法載入。BIND9 本身就支援多個已知檔名的 fallback chain（`bin/named/geoip.c`），ShadowDNS 作為 BIND9 替代品應行為一致。

## What Changes

- `internal/view/loader.go` 將 `countryMMDBFilename` 與 `asnMMDBFilename` 從單一字串常量改為檔名候選列表（先 `GeoIP2-*`、後 `GeoLite2-*`）。
- `LoadGeoIP` 依序嘗試每個候選檔名，取第一個成功開啟的 mmdb；Country 與 ASN 獨立決策，允許混合使用付費與免費版。
- 若某一類資料庫（Country 或 ASN）全部候選檔名都開不起來，錯誤訊息列出所有嘗試過的路徑以利 debug。
- Log 維持現狀（僅印 `path`），使用者可從路徑判讀載入的是哪個 edition。
- 使用者無需任何配置：把對應檔案放入 `geoip-directory`，ShadowDNS 自動載入。

## Non-Goals (optional)

- 不新增 CLI flag（例如 `--geoip-country-db`）。MaxMind 只有兩組命名慣例，fallback chain 已完整覆蓋，加 flag 只是多餘的配置表面積。
- 不修改 `named.conf` 語法。BIND9 本身也不支援在 `options { }` 中指定 mmdb 檔名，保持相容。
- 不變更 log 格式或新增 `edition` 欄位。`path` 已包含完整檔名，使用者需要時可自行過濾。
- 不支援 City 資料庫（`GeoIP2-City.mmdb` / `GeoLite2-City.mmdb`）。現行 view-matcher 只使用 Country 與 ASN。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `view-matcher`: `Fail startup when GeoIP databases are missing or unreadable` 這條 requirement 的行為擴展 — 載入時嘗試 GeoIP2 與 GeoLite2 兩組候選檔名，取第一個成功者；錯誤訊息需列出所有嘗試過的路徑。

## Impact

- Affected specs: `view-matcher`（修改「Fail startup when GeoIP databases are missing or unreadable」requirement 的載入行為與錯誤條件）。
- Affected code:
  - `internal/view/loader.go` — 檔名常量改為 slice，`LoadGeoIP` 實作嘗試邏輯。
  - `internal/view/loader_test.go` — 新增 fallback 相關測試（只有 GeoIP2、只有 GeoLite2、兩者都有取 GeoIP2、混合 Country/ASN、全部缺失）。
- Unaffected:
  - `cmd/shadowdns/main.go`、`internal/config/options.go`、`packaging/named.conf.example` — 不需改動。
  - `docs/migration.md`、`README.md` — 行為為向後相容的擴展，現有 GeoLite2 使用者無感。
