# GeoIP 資料庫

ShadowDNS 的 view 比對使用 MaxMind mmdb 格式的 GeoIP 資料庫，與 BIND 的 `geoip` module 使用相同的資料來源 —— 因此遷移期間並行運行兩套系統時，GeoIP 的 view 判定結果會一致。

## 必要檔案

`named.conf` 的 `geoip-directory` 選項指定 mmdb 檔案所在目錄（預設 `/usr/local/share/GeoIP/`）。以下兩個檔案**都必須存在**，缺任一個 ShadowDNS 會拒絕啟動：

| 檔案 | 用途 |
|------|------|
| `GeoLite2-Country.mmdb` | `geoip country` 規則 |
| `GeoLite2-ASN.mmdb` | `geoip asnum` 規則 |

下載來源：[MaxMind GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data)。

## 讀取與更新

- mmdb 檔案直接讀入記憶體。
- 每次 SIGHUP reload 都會**重新開啟** mmdb 檔案 —— 放入 MaxMind 的每月更新後送 SIGHUP 即可生效，不需重啟 process。
- 成功重載後，`shadowdns_geoip_db_info` gauge 會反映新的 `build_time`，可用來確認更新已生效。

## 每月更新 SOP

```bash
# 1. Place the updated mmdb files into the GeoIP directory
cp GeoLite2-Country.mmdb GeoLite2-ASN.mmdb /usr/local/share/GeoIP/

# 2. Trigger a hot reload
shadowdns reload --named-conf /etc/shadowdns/named.conf
# (or: sudo systemctl reload shadowdns)

# 3. Verify build_time via the metrics endpoint
curl -s localhost:9153/metrics | grep shadowdns_geoip_db_info
```
