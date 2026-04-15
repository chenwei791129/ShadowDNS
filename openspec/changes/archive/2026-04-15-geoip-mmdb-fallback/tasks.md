## 1. Test Fixtures

- [x] 1.1 在 `internal/view/loader_test.go` 建立 fixture helper，能於 `t.TempDir()` 中放置任意 mmdb 候選檔名組合（重用 `internal/view/testhelper_test.go` 已有的有效 mmdb bytes，再依參數以 `GeoIP2-Country.mmdb`、`GeoLite2-Country.mmdb`、`GeoIP2-ASN.mmdb`、`GeoLite2-ASN.mmdb` 等檔名寫入）

## 2. Failing Tests (TDD red phase)

- [x] 2.1 新增測試：目錄中僅存在 `GeoIP2-Country.mmdb` 與 `GeoIP2-ASN.mmdb` 時，`LoadGeoIP` 成功回傳兩個 DB handle
- [x] 2.2 新增測試：目錄中僅存在 `GeoLite2-Country.mmdb` 與 `GeoLite2-ASN.mmdb` 時（既有部署情境）`LoadGeoIP` 仍成功，回歸保護
- [x] 2.3 新增測試：混合情境 — 只有 `GeoIP2-Country.mmdb` 與 `GeoLite2-ASN.mmdb` 時 Country 用 GeoIP2、ASN 用 GeoLite2
- [x] 2.4 新增測試：`GeoIP2-Country.mmdb` 存在但寫入無效 mmdb 內容、`GeoLite2-Country.mmdb` 為有效 mmdb 時，應 fallback 到 GeoLite2 並成功
- [x] 2.5 新增測試：Country 候選全部缺失時回傳錯誤，錯誤訊息須同時包含 `GeoIP2-Country.mmdb` 與 `GeoLite2-Country.mmdb` 兩個完整路徑
- [x] 2.6 新增測試：ASN 候選全部皆為無效 mmdb 內容時回傳錯誤，錯誤訊息須包含兩個完整路徑與各自的驗證錯誤
- [x] 2.7 新增測試：成功載入時 logger 收到 info 事件且 `path` 欄位為實際開啟的完整檔案路徑（驗證 Country 開 GeoIP2 時 path 包含 `GeoIP2-Country.mmdb`）

## 3. Implementation

- [x] 3.1 在 `internal/view/loader.go` 將 `countryMMDBFilename` 與 `asnMMDBFilename` 從單一字串常量改為字串 slice — `countryMMDBCandidates = []string{"GeoIP2-Country.mmdb", "GeoLite2-Country.mmdb"}`、`asnMMDBCandidates = []string{"GeoIP2-ASN.mmdb", "GeoLite2-ASN.mmdb"}`
- [x] 3.2 重構 `LoadGeoIP` 以實作「Fail startup when GeoIP databases are missing or unreadable」requirement 的新 fallback 行為：對 Country 與 ASN 分別依序嘗試每個候選，取第一個 `OpenCountryDB`/`OpenASNDB` 成功者；保留既有 `logger.Info("loaded GeoIP ... database", "path", ...)` 呼叫不變（path 欄位即包含檔名）
- [x] 3.3 在聚合錯誤路徑實作：當某一類（Country 或 ASN）全部候選皆失敗時，回傳 `fmt.Errorf` 包裝後的錯誤，訊息須列出每個嘗試過的路徑與對應的 `OpenXxxDB` 錯誤；若 Country 成功但 ASN 全失敗，須先 `Close` 已開啟的 Country handle 避免 fd 洩漏

## 4. Verification

- [x] 4.1 [P] 執行 `go test ./internal/view/...` 確認第 2 組所有新增測試通過，且既有測試未被破壞
- [x] 4.2 [P] 執行 `make lint` 無新警告
- [x] 4.3 [P] 執行 `make smoke` 驗證既有 `testdata/integration/geoip/` 下 GeoLite2 部署情境仍能通過 `--dry-run`
