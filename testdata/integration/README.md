# Integration Test Fixtures

這個目錄包含整合測試使用的固定資料。

## 結構

- `named.conf` — 主要設定檔。`directory` 和 `geoip-directory` 含有 `TESTDATA_DIR_PLACEHOLDER`，整合測試載入時會替換為 `t.TempDir()` 的絕對路徑。
- `master.zones` — 兩個 view 的宣告：
  - `view-th`：`geoip country TH` 或 `geoip asnum "AS64500 Test ASN"` 命中
  - `view-other`：`any`（fallback）
- `aliases.yaml` — 宣告 `backup.example` 為 `example.com` 的 backup
- `master/*.fwd` — zone files

## 測試 IP 選擇

- view-th：來源 IP 必須在 mock GeoIP DB 中對應到 country=TH 或 ASN=64500
- view-other：任何不命中 view-th 的 IP

## Mock GeoIP

整合測試在 `geoip/` 目錄下動態產生 mmdb：
- `192.0.2.0/24` → country TH
- `198.51.100.0/24` → country JP（不命中 view-th 的 country rule）
- ASN 64500 對應 `203.0.113.0/24`（命中 view-th 的 asnum rule）
