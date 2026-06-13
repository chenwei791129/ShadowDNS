# GeoIP 資料庫

ShadowDNS 的 view 比對使用 MaxMind mmdb 格式的 GeoIP 資料庫，與 BIND 的 `geoip` module 使用相同的資料來源 —— 因此遷移期間並行運行兩套系統時，GeoIP 的 view 判定結果會一致。

## 必要檔案

GeoIP 資料庫是**條件式**需求：只有在任一 view 的 `match-clients` 使用 `geoip country` / `geoip asnum` 規則，或 `named.conf` 有設定 `geoip-directory` 選項時才需要 mmdb 檔案。省略 `geoip-directory` 與設為空字串（`geoip-directory "";`）等價——皆視為未設定。

當 `geoip-directory` 有設定（非空）時，它指定 mmdb 檔案所在目錄（例如 `/usr/local/share/GeoIP/`），且以下兩個檔案**都必須存在**，缺任一個 ShadowDNS 會拒絕啟動：

| 檔案 | 用途 |
|------|------|
| `GeoLite2-Country.mmdb` | `geoip country` 規則 |
| `GeoLite2-ASN.mmdb` | `geoip asnum` 規則 |

下載來源：[MaxMind GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data)。

當 `geoip-directory` 未設定但至少有一個 view 使用 geo 規則時，啟動與 `--dry-run` 會以設定錯誤失敗，並指出第一個違規的 view 及其來源檔案路徑與行號：

```text
loading GeoIP: /etc/shadowdns/named.conf:42: view "asia" uses geoip match-clients rules but geoip-directory is not set in named.conf options
```

（SIGHUP reload 時外層前綴為 `reloading GeoIP:`，並保留運行中的設定。）

當 `geoip-directory` 未設定且沒有任何 view 使用 geo 規則時，伺服器不需任何 mmdb 檔案即可啟動並提供服務——`any`、IP、CIDR 規則運作不變。

## 讀取與更新

- mmdb 檔案直接讀入記憶體。
- 每次 SIGHUP reload 都會**重新開啟** mmdb 檔案 —— 放入 MaxMind 的每月更新後送 SIGHUP 即可生效，不需重啟 process。
- 成功重載後，`shadowdns_geoip_db_info` gauge 會反映新的 `build_time`，可用來確認更新已生效。
- reload 套用相同的條件式邏輯，因此可透過 SIGHUP 啟用或停用 GeoIP：設定 `geoip-directory` 會當場載入 mmdb 檔案（失敗時保留舊設定）；取消設定（且不再有 geo 規則）後，伺服器會在沒有任何資料庫的狀態下繼續運行。

## 未載入 GeoIP 時

當沒有載入任何 GeoIP 資料庫時：

- metrics 端點**不會輸出 `shadowdns_geoip_db_info` series**，且停用 GeoIP 的 reload 會刪除先前已輸出的 series。
- 若 `--ecs-enable` 啟用中，ShadowDNS 會輸出一筆 **Warn** 日誌——啟動時一次，之後任何結束於此狀態的 reload 也會再輸出一次——因為沒有 GeoIP 資料庫時 ECS 無法影響 view 選擇；只剩下 [ECS option echo](../guides/ecs.md#回應-echo) 行為。
- 「shadowdns ready」、「reload complete」與 dry-run 摘要日誌都帶有布林值 `geoip_enabled` 欄位，因此隨時可從日誌稽核資料庫是否已載入。

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
