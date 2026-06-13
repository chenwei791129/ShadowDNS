# EDNS Client Subnet (ECS)

ShadowDNS 支援 EDNS Client Subnet（ECS, RFC 7871），作為 GeoIP view 選擇的 opt-in 加強層。當查詢經由公共 resolver（Google Public DNS 等）轉送時，伺服器看到的來源 IP 是 resolver 的位址而非終端使用者的——GeoIP view 選擇反映的是 resolver 所在地，不是使用者所在地。ECS 讓 resolver 把使用者的子網附在查詢中；啟用 ECS 後，ShadowDNS 改用該子網做 geo 比對。

ECS **預設關閉**，與 BIND 一致（BIND 已在 9.13.0 移除實驗性 ECS 支援）。旗標關閉時，查詢行為與沒有此功能的版本完全相同（bit-identical）。

---

## 啟用方式

ECS 處理由單一 CLI 旗標控制：

```bash
shadowdns --config /etc/shadowdns/shadowdns.yaml --ecs-enable
```

- `--ecs-enable` 預設為 `false`，只在啟動時讀取——SIGHUP 不會改變其值。
- 啟動時伺服器會輸出一筆 info 等級日誌，記錄 ECS 處理是啟用或關閉（`--dry-run` 也會輸出），因此隨時可從日誌稽核目前狀態。
- 關閉時，查詢中的 ECS option 在所有層面一律忽略——view 選擇、驗證、回應組裝——且回應永不帶 ECS option（RFC 7871 對未啟用 ECS 的伺服器的要求）。

---

## ECS 如何影響 view 選擇

啟用 ECS 後，若查詢帶有合法的 ECS option 且 source prefix length 大於 0，ECS 位址會成為 **geo 查詢位址**——但僅限會查 GeoIP 的規則類型：

| `match-clients` 規則類型 | 評估的位址 |
|--------------------------|------------|
| `country` | ECS 位址 |
| `asn` | ECS 位址 |
| IP | 真實來源 IP |
| CIDR | 真實來源 IP |
| `any` | 不論位址皆符合 |

IP 與 CIDR 規則永遠以真實的傳輸層來源 IP 評估。這是刻意設計的安全邊界：ECS 是 client 自行提供的資料，因此**偽造的 ECS option 永遠無法選中受 ACL 保護的 view**。geo 導流用 country/ASN 規則、存取控制用 IP/CIDR 規則；ECS 只影響前者。

其他規則：

- 不帶 ECS option 的查詢，處理方式與 ECS 關閉時完全相同。
- 若 OPT record 中有多個 ECS option，只處理第一個。
- 若 GeoIP 資料庫中查不到 ECS 位址，country/ASN 規則視為不符合，繼續比對下一條規則。**不會**退回以來源 IP 重新評估 geo 規則。
- AXFR/IXFR 的 view 選擇永不使用 ECS（zone transfer 查詢帶有格式錯誤的 ECS option 時，仍與一般查詢一樣回 FORMERR）。

---

## 沒有 GeoIP 資料庫時

GeoIP 是條件式載入（見 [GeoIP 資料庫](../configuration/geoip.md#未載入-geoip-時)）：沒有 `geoip-directory` 也沒有 geo 規則的設定，會在不載入任何 mmdb 檔案的狀態下運行。此時 ECS 對 view 選擇**完全沒有影響**——沒有 country/ASN 規則可評估，而 IP/CIDR/`any` 規則永遠使用真實來源 IP——唯一保留的 ECS 行為是下方的[回應 echo](#回應-echo)。

為了讓此狀態可稽核，當 `--ecs-enable` 啟用中但未載入任何 GeoIP 資料庫時，ShadowDNS 會輸出一筆 **Warn** 日誌：啟動時一次，之後任何結束於此狀態的 reload 也會再輸出一次。

---

## 回應 echo

對合法的 ECS 查詢，回應的 OPT record 會帶有恰好一個 ECS option，echo 查詢的 FAMILY、SOURCE PREFIX-LENGTH 與 ADDRESS，且 **SCOPE PREFIX-LENGTH 等於查詢的 SOURCE PREFIX-LENGTH**。echo 適用於標準回答路徑組出的每一種回應——NOERROR、NXDOMAIN，以及不符合任何 view 的 client 收到的 REFUSED。

在 ECS 處理點之前就產生的回應不在 echo 範圍內：不支援的 opcode 回 NOTIMP、question 數量錯誤回 FORMERR、不支援的 EDNS 版本回 BADVERS、COOKIE option 格式錯誤回 FORMERR，以及 zone transfer 回應串流與 panic 復原的 SERVFAIL。不帶 ECS option 的查詢，回應也永遠不會帶 ECS option。

---

## Client opt-out

格式正確但 SOURCE PREFIX-LENGTH 為 0 的 ECS option 是 RFC 7871 的 client opt-out（「不要使用我的子網」）。ShadowDNS 對 FAMILY 0、1、2 一律 honor——包含 `dig +subnet=0` 送出的 FAMILY 0 形式：

- view 選擇只使用真實來源 IP。
- 回應 echo 該 ECS option，保留查詢的 FAMILY，SCOPE PREFIX-LENGTH 為 0。

---

## 格式錯誤的 ECS 處理

啟用 ECS 後，ECS option 違反 RFC 7871 的查詢會被拒絕並回 FORMERR（回應帶 OPT record 但不帶 ECS option）。handler 在以下情況視為格式錯誤：

- 查詢的 SCOPE PREFIX-LENGTH 非零（RFC 7871 規定查詢中必須為 0），或
- SOURCE PREFIX-LENGTH 之外的位址 bit 非零（prefix length 為 0 時，所有位址 bit 都在 prefix 之外——因此此檢查優先於 opt-out 判定）。

與旗標無關地，DNS 訊息函式庫會在解包時就拒絕嚴重格式錯誤的 option（未知 FAMILY、prefix length 超過 family 上限 32/128）；這類查詢在進入 handler 之前就會收到 FORMERR。ECS 關閉時，上述 handler 可偵測的格式錯誤形式則會被靜默忽略，不觸發 FORMERR。

---

## 用 dig 測試

```bash
# 帶上 client subnet：geo 規則改評估 203.0.113.0/24 而非你的來源 IP
dig @192.0.2.53 www.example.com A +subnet=203.0.113.0/24

# Client opt-out：以來源 IP 回答，echo 的 scope 為 0
dig @192.0.2.53 www.example.com A +subnet=0
```

第一個回應的 `CLIENT-SUBNET` 偽區段會顯示 echo 回來的 option，例如 `203.0.113.0/24/24`（位址 / source prefix / scope）。

---

## 維運筆記

- 實務上約 90% 的 ECS 流量來自 Google Public DNS；Cloudflare 的 1.1.1.1 基於隱私完全不送 ECS。因此啟用 ECS 主要改善的是經由 Google DNS 而來的查詢的 geo 精準度——它是 source-IP GeoIP 之上的加強層，而非取代。
- `--ecs-enable` 旗標摘要見 [CLI 參考](../reference/cli.md)，與 BIND 的差異見[功能比較表](../index.md#與-bind-的功能比較)。
