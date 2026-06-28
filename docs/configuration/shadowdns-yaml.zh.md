# shadowdns.yaml

`shadowdns.yaml` 是 ShadowDNS 自有的統一設定檔（以 `--config` 指定），單一 YAML 文件包含三個可選的頂層區段：`aliases`（備援網域 → root 對照表）、`ephemeral_api`（短時效 TXT record 的 HTTP API），以及 `doh`（DNS-over-HTTPS，RFC 8484）。任何其他頂層 key 都會在啟動時被拒絕（strict decoding）。

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

doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme-v02.api.letsencrypt.org/directory"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
    account_key_file: "/var/lib/shadowdns/acme/account.key"
```

## aliases 欄位

`aliases` 下的每個 key 是一個 root domain，value 是一個物件：

| 欄位 | 必填 | 說明 |
|------|------|------|
| `members` | 是（不可為空） | 此 root 服務的備援網域清單，查詢以改寫方式對應到 root |
| `rewrite_rdata_labels` | 否（預設 `false`） | 設為 `true` 時，RDATA 名稱欄位（CNAME/SRV target、NS、MX、PTR、SOA 名稱）套用 label-anywhere 改寫——值內出現的 root label 序列全數替換為備援 origin，而不只是 in-bailiwick 後綴。適用於以 templated CDN 式 target 將 root origin 嵌在中間 label 的 zone |
| `collapse_cname_chain` | 否（預設 `false`） | 設為 `true` 時，此 root 與其所有成員的回應會收合 zone 內 CNAME 鏈——見 [CNAME 鏈收合](../guides/cname-chain-collapsing.md) |

### `rewrite_rdata_labels`：`false` 與 `true` 的實際差異

CDN 託管的 zone 經常回傳**模板化 CNAME**——target 把 zone 自己的 origin 當成中間 label 嵌入，後面再接 CDN 供應商的後綴。假設 `example.com` 是 root、`example.net` 是它的其中一個備援成員，而 root 對 `assets.example.com` 的權威紀錄是：

```text
assets.example.com.  300  IN  CNAME  assets.example.com.c.cdn.example.org.
```

此時對備援名稱發出查詢——`assets.example.net A`。無論哪種設定，ShadowDNS 都會把 **owner name** 改寫回備援 namespace；兩種設定的差別只在 **CNAME target** 怎麼改寫：

| `rewrite_rdata_labels` | `assets.example.net` 回傳的 CNAME target | 正確？ |
|------|------|------|
| `false`（預設） | `assets.example.com.c.cdn.example.org.` | ✗ —— target 以 `.example.org` 結尾，root origin `example.com` 落在*中間* label，並非 in-bailiwick 後綴。保守規則不會動它，備援因此洩漏了 root 的名字。 |
| `true` | `assets.example.net.c.cdn.example.org.` | ✓ —— label-anywhere 規則把嵌入的 `example.com` 替換成備援 origin `example.net`，與原生託管的 `example.net` zone 回傳結果完全一致。 |

當 root 的紀錄指向把 root origin 當成嵌入 label 的模板化 CDN target 時，就要設 `rewrite_rdata_labels: true`。一般 zone 的 RDATA 名稱若不是 in-bailiwick 就是真正的外部名稱，維持預設 `false`，避免把剛好等於 root origin 的 label 誤改寫。

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

## doh 欄位

所有欄位皆為必填；載入時若有缺漏，會指名第一個缺少的欄位並失敗。

| 欄位 | 必填 | 說明 |
|------|------|------|
| `listen` | 是 | DoH HTTPS 服務綁定的 `host:port`，例如 `203.0.113.10:443` |
| `acme.directory_url` | 是 | 簽發 CA 的 ACME directory URL（必須是絕對的 `https://` URL） |
| `acme.ip` | 是 | 憑證簽發對象的 IP 位址（RFC 8738 IP-identifier 憑證） |
| `acme.http01_listen` | 是 | ACME HTTP-01 challenge 回應器綁定的 `host:port`；必須能從公開網際網路以 port 80 連到 |
| `acme.account_key_file` | 是 | 持久化 ACME 帳號私鑰的絕對路徑（PKCS#8 PEM、權限 `0600`）。檔案不存在時於首次使用產生，並跨重啟重用，使重新註冊具冪等性、不會耗盡 ACME new-account 速率限制。請使用 systemd `StateDirectory`（`/var/lib/shadowdns`）之下的路徑。此檔為**機密**——務必保持 `0600` 且擁有者為服務使用者。變更此欄位需重啟才會生效 |

ACME 帳號以無聯絡 email 註冊，因此 `doh.acme` 不接受 `email` 欄位；若填入會以未知欄位導致載入失敗。（RFC 8555 的聯絡 email 為選填，且短效自動續簽的憑證讓到期通知失去意義。）

`doh` 區段不存在時，不會啟動 DoH server、ACME client 或 HTTP-01 listener。

DoH 重用權威查詢路徑且為**非遞迴**：只回答 ShadowDNS 託管的 zone，zone 以外的查詢會回 `REFUSED`。它**不是**通用的遞迴 DoH resolver。

TLS 憑證透過 ACME HTTP-01 以 Let's Encrypt 短時效 profile（約 6 天）為該 IP 取得，並自動續期、不需重啟即熱抽換。

部署流程與運維細節請見 [DNS-over-HTTPS](../guides/doh.md)。

## SIGHUP 熱重載

SIGHUP 會重新讀取 `shadowdns.yaml` 並**原子性地**替換記憶體中的 alias map：

- 任一區段驗證失敗時，運行中的伺服器保持先前狀態，ephemeral record 不受影響。
- 重載成功時，ephemeral record store 會被清空。
- `doh` 區段在重載時會重新驗證（驗證錯誤時運行中的伺服器維持不變）。但對 `doh.listen` 或任何 `doh.acme.*` 欄位的變更**不會**即時套用——必須重啟程序，並會記錄一則 advisory 提示。憑證輪替則是獨立且自動進行。
- 每次重載嘗試都可透過 Prometheus 觀測：
    - `shadowdns_reload_total{result="success"|"failure"}` 計數重載結果
    - `shadowdns_config_last_reload_success_timestamp_seconds` 記錄最近一次成功載入設定的 Unix 時間（啟動時初始化），可用 `time() - <gauge>` 做設定過期告警

!!! warning "v0.x 起的 breaking change"
    舊版的 `--aliases` CLI flag 與 `aliases.yaml` 檔案已移除。遷移方式很機械：把舊 `aliases.yaml`（root → [backups] 格式）的項目搬到新 `shadowdns.yaml` 的 `aliases:` 區段下即可。
