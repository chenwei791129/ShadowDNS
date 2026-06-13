# Integration Test Fixtures

這個目錄包含整合測試使用的固定資料。

此佈局模擬 Debian/Ubuntu BIND 慣例（`named.conf` 透過 `include` 拆成
`named.conf.options` + `named.conf.local`，權威 zone 檔放在設定同層），**非 BIND
安裝後的空白預設**。同時，此目錄是 **parser 測試 fixture**：刻意保留 `cnames/`
子目錄與 quoted／bare `$INCLUDE` 變體等測試用構件，用來覆蓋 parser 的路徑解析與
include 相容層，**非最小推薦佈局**——實際部署不需要這些測試構件。

## 結構

- `named.conf` — 主設定檔，僅含兩行 `include`：`include "named.conf.options";` 與
  `include "named.conf.local";`（Debian/Ubuntu include split）。
- `named.conf.options` — `options { ... }` 區塊。`directory` 和 `geoip-directory`
  含有 `TESTDATA_DIR_PLACEHOLDER`，整合測試載入時會替換為 `t.TempDir()` 的絕對路徑。
- `named.conf.local` — 兩個 view 的宣告：
  - `view-th`：`geoip country TH` 或 `geoip asnum "AS64500 Test ASN"` 命中
  - `view-other`：`any`（fallback）
- `shadowdns.yaml` — 統一設定檔，宣告 `example.com` 的備援域名為 `backup.example`
  （採 `root: [backups]` 格式）。
- `db.<zone>-<view>` — split-horizon 同名 zone 的 zone files，以連字號分隔 view 標籤
  與點分隔的 zone 名稱，使 zone 邊界一目了然：`db.example.com-th`、
  `db.example.com-other`、`db.backup.example-th`、`db.backup.example-other`。
- `db.<zone>` — 單一 view 的 zone file：`db.include-test.example`。
- `db.backup.example.overrides` — 由 `db.backup.example-other` 以相對 `$include` 拉入
  的 override 片段。
- `cnames/db.example.com.cname` — 巢狀子目錄中的 `$INCLUDE` 片段，由
  `db.include-test.example` 以 quoted 與 bare 兩種 `$include` 形式拉入，覆蓋巢狀＋
  引號變體。

## 測試 IP 選擇

- view-th：來源 IP 必須在 mock GeoIP DB 中對應到 country=TH 或 ASN=64500
- view-other：任何不命中 view-th 的 IP

## Mock GeoIP

整合測試在 `geoip/` 目錄下動態產生 mmdb：
- `192.0.2.0/24` → country TH
- `198.51.100.0/24` → country JP（不命中 view-th 的 country rule）
- ASN 64500 對應 `203.0.113.0/24`（命中 view-th 的 asnum rule）
