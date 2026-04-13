## Why

現行 BIND 權威 DNS server 載入 3,600 個 unique domain × 7 個 view ≈ 25,200 份完整 zone 資料，其中約 80% 是 backup domain 的冗餘副本（內容與對應 root domain 相同，僅 zone name 不同），造成記憶體佔用龐大且維運成本高。需要一個相容現有 `named.conf` / zone 檔格式、能讓 3 台既有 BIND slave 無縫繼續運作，同時透過 zone 別名機制避免 backup domain 冗餘載入的新權威 DNS server。

## What Changes

- 新增一個用 Go 實作的權威 DNS server（ShadowDNS），取代目前 master 節點的 BIND
- 相容既有 `named.conf` + `master.zones` 設定（view、match-clients、zone file 對應、allow-transfer）
- 相容既有 zone file 內容格式（RFC 1035 文字格式；檔案副檔名非契約）
- 支援 BIND split-horizon view：以 GeoIP country、GeoIP ASN、裸 IP、CIDR、`any` 作為 match-clients 規則，first-match 語意
- 新增 `aliases.yaml` 設定：定義 root domain ↔ backup domain 對應；backup domain 不載入完整 zone 資料
- 新增「in-bailiwick rewrite」查詢邏輯：查詢 backup domain 時 owner name 一律改寫、record value 中指向 root zone 的名字跟著改寫，指向第三方的名字保留原樣
- backup domain 仍可透過獨立的 zone 檔提供 TXT / MX / SRV 覆寫記錄；A / AAAA / CNAME / NS / SOA 一律從 root 繼承
- 支援 AXFR 與 NOTIFY，讓既有的 BIND slave 群繼續拉取；alias zone 的 AXFR 以 rewrite 後的記錄回應
- SOA serial 策略：backup zone 的 SOA serial 繼承自 root zone，root 更新時自動連動
- 啟動期（v1）不支援熱更新，重新載入設定以重啟程序達成

## Capabilities

### New Capabilities

- `config-loader`: 解析 `named.conf` 與 `master.zones`，抽出 options、view 定義、zone 檔路徑對應；解析 `aliases.yaml` 取得 root ↔ backup 對應。
- `zone-parser`: 解析 RFC 1035 格式的 zone 檔，建立 in-memory zone tree；依 `aliases.yaml` 將檔案分類為 root zone（完整載入）或 backup override（僅取 TXT/MX/SRV）。
- `view-matcher`: 依 client IP 套用 first-match 語意判斷 view；支援 GeoIP country、GeoIP ASN、IP/CIDR、`any` 四種規則；使用 MaxMind mmdb 做 country/ASN 查詢。
- `alias-resolver`: 查詢期執行 in-bailiwick rewrite：偵測目標 zone 是否為 backup、改寫 owner name、依「target 是否指向 root zone」決定是否改寫 record value。
- `dns-server`: UDP/TCP :53 listener、DNS message 解析與回應、NXDOMAIN/NODATA 回應（含 authority section SOA）、minimal-responses、recursion=no、version/hostname 隱藏。
- `zone-transfer`: AXFR query handler、NOTIFY 送出、`allow-transfer` ACL 判斷；alias zone 的 AXFR 以 rewrite 後記錄 stream 回應。

### Modified Capabilities

（無——專案首個 change，無既有 capability）

## Impact

- **新增程式碼**（全新 Go 專案，所有檔案皆新建）：
  - `cmd/shadowdns/main.go`：進入點、flag 解析、signal 處理
  - `internal/config/`：named.conf / master.zones / aliases.yaml 解析器
  - `internal/zone/`：zone 檔解析、in-memory 儲存、查詢介面
  - `internal/view/`：match-clients 規則引擎、GeoIP 查詢
  - `internal/alias/`：in-bailiwick rewrite 邏輯
  - `internal/server/`：DNS UDP/TCP server、AXFR/NOTIFY 處理
  - `go.mod` / `go.sum`：依賴宣告
- **新增設定檔範例**：`examples/aliases.yaml`、`examples/named.conf`
- **外部依賴**：
  - `github.com/miekg/dns`：DNS 協定處理（事實標準）
  - `github.com/oschwald/maxminddb-golang`：MaxMind GeoIP 查詢
  - `gopkg.in/yaml.v3`：aliases.yaml 解析
- **外部系統**：
  - 部署期需放置 MaxMind GeoLite2-Country 與 GeoLite2-ASN `.mmdb` 檔
  - 部署期既有 BIND slave 指向 ShadowDNS 拉 AXFR；slave IP 清單由部署者於 `allow-transfer` 設定
- **不影響**：
  - zone file 內容格式（沿用既有）
  - slave 端 BIND（不需更動）
  - 管理系統產生 zone file 的流程（可選：加產 `aliases.yaml`）
