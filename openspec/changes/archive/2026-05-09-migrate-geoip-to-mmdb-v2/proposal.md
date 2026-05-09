## Why

`view.(*CountryDB).Lookup` 在 2026-05-09 baseline-recheck pprof 量測下佔 cum 12.85%，其中 42.93% (6.59s/15.35s) 是 `runtime.newobject` — 來自 `maxminddb-golang v1.13.1` reflection-based 的 `c.db.Lookup(netIP, &rec)` 對 record struct 的 per-call alloc。整個 process 的 `newobject` 有 62.88% 來自 CountryDB.Lookup 一處。`maxminddb-golang/v2 v2.1.1`（已在 go.sum 為 indirect）提供 non-reflection 的 `Result.DecodePath` API，能消除這些 alloc 並接受 `netip.Addr` 直接查詢，無需 `ip.AsSlice()` 轉換。預期 +5~8% QPS，每筆 query 都受惠（非 client repeat 依賴）。

## What Changes

- 把 `internal/view/geoip_country.go` 的 `(*CountryDB).Lookup` 內部實作從 `c.db.Lookup(netIP, &rec)`（v1 reflection decode）改為 v2 的 `c.db.Lookup(ip).DecodePath(&iso, "country", "iso_code")`。
- 把 `internal/view/geoip_asn.go` 的 `(*ASNDB).Lookup` 內部實作從 v1 reflection decode 改為 v2 的 `r.db.Lookup(ip).DecodePath(&asn, "autonomous_system_number")`。
- 兩個檔案的 `OpenCountryDB` / `OpenASNDB` 與 `Metadata` 內部從 `maxminddb` 改用 `maxminddb/v2`；`Metadata()` 回傳型別從 `v1.Metadata` 變為 `v2.Metadata`（`DatabaseType`、`BuildEpoch` 欄位名稱與型別在 v2 不變，現有 callers 不需改動）。
- `go.mod` 移除 `github.com/oschwald/maxminddb-golang v1.13.1`（line 10），把 `github.com/oschwald/maxminddb-golang/v2` 從 indirect 升為 direct；執行 `go mod tidy`。
- 在 `internal/view/geoip_country_test.go` 與 `geoip_asn_test.go` 新增 `BenchmarkCountryDB_Lookup` / `BenchmarkASNDB_Lookup`，用 `b.ReportAllocs()` 量化 alloc/op 變化作為遷移收益記錄。

## Non-Goals (optional)

- 不動 `(*CountryDB).Lookup` 與 `(*ASNDB).Lookup` 的對外簽章（`(netip.Addr) (string, bool)` 與 `(netip.Addr) (uint32, bool)`）— 對 caller 端 `internal/view/matcher.go` 透明。
- 不動 `internal/view/matcher.go`、`internal/view/loader.go`、`cmd/shadowdns/main.go`、`internal/server/build.go`。
- 不加 per-IP cache（dnspyre 單一 source IP 會給 100% hit rate 假象，production 客戶端分散下 gain 不會 transfer）。
- 不順便處理 plan §1.6 排序的 #2（`alias.Detect` 資料結構）或 #3（`alias.RewriteName` concat）。
- 不升級 v2 之外的依賴；不改 `OpenCountryDB`/`OpenASNDB` 接受 path 的構造方式。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

(none — 此 change 為純內部實作 refactor，現有 view-matcher capability 的 country/ASN lookup 行為要求不變：相同 IP 輸入產生相同 ISO code / ASN 輸出，相同錯誤路徑視為 no-match。)

## Impact

- Affected specs: 無（觀察行為不變）
- Affected code:
  - Modified: internal/view/geoip_country.go
  - Modified: internal/view/geoip_asn.go
  - Modified: internal/view/geoip_country_test.go
  - Modified: internal/view/geoip_asn_test.go
  - Modified: go.mod
  - Modified: go.sum
- Affected dependencies:
  - Removed direct: `github.com/oschwald/maxminddb-golang v1.13.1`
  - Promoted indirect → direct: `github.com/oschwald/maxminddb-golang/v2 v2.1.1`
