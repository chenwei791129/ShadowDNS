# shadowdns.yaml

`shadowdns.yaml` 是 ShadowDNS 自有的統一設定檔（以 `--config` 指定），單一 YAML 文件包含兩個可選的頂層區段：`aliases`（備援網域 → root 對照表）與 `ephemeral_api`（短時效 TXT record 的 HTTP API）。任何其他頂層 key 都會在啟動時被拒絕（strict decoding）。

```yaml
# shadowdns.yaml

aliases:
  example.com:
    members:
      - backup.example.com
      - mirror.example.com
  example.org:
    members:
      - backup.example.org
    rewrite_rdata_labels: true
    collapse_cname_chain: true

ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "127.0.0.1"
    - "10.0.0.0/8"
  # token: "optional-bearer-token"
```

## aliases 欄位

`aliases` 下的每個 key 是一個 root domain，value 是一個物件：

| 欄位 | 必填 | 說明 |
|------|------|------|
| `members` | 是（不可為空） | 此 root 服務的備援網域清單，查詢以改寫方式對應到 root |
| `rewrite_rdata_labels` | 否（預設 `false`） | 設為 `true` 時，RDATA 名稱欄位（CNAME/SRV target、NS、MX、PTR、SOA 名稱）套用 label-anywhere 改寫——值內出現的 root label 序列全數替換為備援 origin，而不只是 in-bailiwick 後綴。適用於以 templated CDN 式 target 將 root origin 嵌在中間 label 的 zone |
| `collapse_cname_chain` | 否（預設 `false`） | 設為 `true` 時，此 root 與其所有成員的回應會收合 zone 內 CNAME 鏈——見 [CNAME 鏈收合](../guides/cname-chain-collapsing.md) |

## aliases 規則

- 同一個備援網域在所有 root 之間（正規化後）最多只能出現一次。
- 備援網域不可等於它的 root（self-alias 會被拒絕）。
- 沒有列在這裡的網域視為獨立的 root zone，完整載入記憶體。
- 備援 zone 可以選擇性提供自己的 zone file，內含 TXT、MX、SRV 覆寫紀錄。備援 zone file 中的 A、AAAA、CNAME、NS、SOA 紀錄會被丟棄並記 WARN —— 這些類型永遠從 root 繼承。

Zone aliasing 的查詢處理細節請見 [Zone Aliasing 原理](../guides/zone-aliasing.md)。

## ephemeral_api 欄位

| 欄位 | 必填 | 說明 |
|------|------|------|
| `listen` | 是 | API server 綁定的 `host:port` |
| `allow` | 是（不可為空） | 允許存取 API 的來源 IP 或 CIDR 清單；空清單會被拒絕 |
| `token` | 否 | Pre-shared bearer token。設定後每個請求都必須帶 `Authorization: Bearer <token>`；省略時跳過 token 驗證（IP ACL 仍然有效） |

`ephemeral_api` 區段不存在時，不會啟動 HTTP API server。端點細節、request/response schema 與 `curl` 範例請見 [Ephemeral TXT API](../ephemeral-api.md)。

## SIGHUP 熱重載

SIGHUP 會重新讀取 `shadowdns.yaml` 並**原子性地**替換記憶體中的 alias map：

- 任一區段驗證失敗時，運行中的伺服器保持先前狀態，ephemeral record 不受影響。
- 重載成功時，ephemeral record store 會被清空。
- 每次重載嘗試都可透過 Prometheus 觀測：
    - `shadowdns_reload_total{result="success"|"failure"}` 計數重載結果
    - `shadowdns_config_last_reload_success_timestamp_seconds` 記錄最近一次成功載入設定的 Unix 時間（啟動時初始化），可用 `time() - <gauge>` 做設定過期告警

!!! warning "v0.x 起的 breaking change"
    舊版的 `--aliases` CLI flag 與 `aliases.yaml` 檔案已移除。遷移方式很機械：把舊 `aliases.yaml`（root → [backups] 格式）的項目搬到新 `shadowdns.yaml` 的 `aliases:` 區段下即可。
