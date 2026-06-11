# 快速開始

本頁帶你以最短路徑把 ShadowDNS 跑起來：從原始碼編譯、準備 GeoIP 資料庫、到啟動服務。

## 前置條件

- Go 1.26+
- MaxMind GeoLite2 資料庫（Country + ASN，見步驟 3）
- 既有的 BIND 設定（`named.conf` 與 zone file）

## 1. 取得原始碼

```bash
git clone https://github.com/chenwei791129/ShadowDNS.git
cd ShadowDNS
```

## 2. 編譯

```bash
make build
```

產出的 binary 位於 `bin/shadowdns-<GOOS>-<GOARCH>`（例如 Linux/amd64 上是 `bin/shadowdns-linux-amd64`，Apple Silicon 上是 `bin/shadowdns-darwin-arm64`）。

## 3. 準備 GeoIP 資料庫

ShadowDNS 從 `named.conf` 的 `geoip-directory` 選項讀取 mmdb 目錄（預設 `/usr/local/share/GeoIP/`）。以下兩個檔案**必須存在**，缺一就拒絕啟動：

```text
/usr/local/share/GeoIP/GeoLite2-Country.mmdb
/usr/local/share/GeoIP/GeoLite2-ASN.mmdb
```

請從 [MaxMind](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data) 下載。詳見 [GeoIP 資料庫](configuration/geoip.md)。

## 4. 啟動 ShadowDNS

使用步驟 2 產出的 binary（以下範例假設 linux/amd64）：

```bash
./bin/shadowdns-linux-amd64 \
    --named-conf /etc/namedb/named.conf \
    --config     /etc/namedb/shadowdns.yaml
```

ShadowDNS 預設監聽 `:53`（UDP 與 TCP），可用 `--listen` 覆寫。

`shadowdns.yaml` 的內容與格式請見 [shadowdns.yaml 設定](configuration/shadowdns-yaml.md)。

## 5. 用 `--dry-run` 驗證設定

正式啟動前，建議先以 `--dry-run` 驗證所有 zone file 都能正確解析、GeoIP 資料庫可讀取：

```bash
./bin/shadowdns-linux-amd64 \
    --named-conf /etc/namedb/named.conf \
    --config     /etc/namedb/shadowdns.yaml \
    --dry-run
```

`--dry-run` 會載入設定與 zone、輸出摘要後直接結束，不開啟任何 listener。Exit code 為 0 即代表設定有效。

## 下一步

- [安裝](installation.md) — 以 `.deb` 套件部署到 Debian/Ubuntu 主機並交由 systemd 管理
- [CLI 參考](reference/cli.md) — 完整的 flag 與 subcommand 說明
- [從 BIND 遷移](migration.md) — 正式環境切換的完整操作指引
