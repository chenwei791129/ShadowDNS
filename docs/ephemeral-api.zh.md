# Ephemeral TXT API

ShadowDNS 內建一個輕量的 HTTP API，讓 ACME client（certbot、acme.sh、lego 等）能動態新增或刪除短時效 TXT record，用於 DNS-01 challenge 驗證。所有 record 只存在記憶體中，不寫入 zone file，也不寫入磁碟；TTL 到期、服務重啟或 SIGHUP reload 都會被清除。

---

## 啟用與設定

API 由 unified config（`--config` 指向的 `shadowdns.yaml`）中的 `ephemeral_api` 區段控制。該區段缺席時 API server 不啟動。

```yaml
# /etc/shadowdns/shadowdns.yaml
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "127.0.0.1"
    - "10.0.0.0/8"
  # token: "optional-bearer-token"
```

欄位規則：

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `listen` | string | 必填 | `host:port`，API server 綁定位址 |
| `allow` | list | 必填，非空 | 允許連線的來源 IP 或 CIDR（IPv4/IPv6 皆可）|
| `token` | string | 選填 | Pre-shared bearer token；省略則不驗證 |

IP ACL 先於 token 驗證：來源 IP 不在白名單直接回 `403`，就算 token 正確也不放行。

---

## Endpoints

| Method | Path | 用途 |
|--------|------|------|
| `PUT` | `/v1/txt/{fqdn}` | 新增或更新 ephemeral TXT record |
| `DELETE` | `/v1/txt/{fqdn}` | 刪除 ephemeral TXT record（冪等）|

`{fqdn}` 會被正規化為 lowercase + trailing dot；大小寫與是否帶尾點均不影響結果。

---

## PUT — 新增或刷新 TXT value

同一 FQDN 可同時存在多筆 value。PUT 的語意為「add-or-refresh」：

- 傳入的 `value` 於該 FQDN 下尚不存在 → 追加一筆新 entry
- 傳入的 `value` 已存在 → 就地刷新該 entry 的 TTL，**不**建立重複

因此連續兩次相同 body 的呼叫是冪等的——最終狀態與只呼叫一次相同。對應 ACME DNS-01 同時驗證 apex + wildcard 的情境，兩支 client 可各自 PUT 自己的 token，彼此不覆蓋。

### Request body

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `value` | string | 必填 | TXT record 的值（例如 ACME challenge token）；UTF-8 bytes ≤ 255（RFC 1035 TXT character-string 上限），超過回 `400` |
| `ttl` | integer | 選填（預設 0）| 秒數；會 clamp 至 `[1, 3600]`（`0` → `1`，`7200` → `3600`）|

### 範例（無 token）

```bash
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Content-Type: application/json' \
  -d '{"value":"challenge-token-from-acme-client","ttl":120}'
```

### 範例（有 token）

```bash
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Authorization: Bearer secret123' \
  -H 'Content-Type: application/json' \
  -d '{"value":"challenge-token","ttl":120}'
```

### 成功回應（200）

```json
{
  "status": "ok",
  "fqdn": "_acme-challenge.example.com.",
  "ttl": 120,
  "count": 1
}
```

- `fqdn`：canonical 形式（lowercase + trailing dot）
- `ttl`：實際採用的值（可能已被 clamp）
- `count`：該 FQDN 目前的 ephemeral entry 總數（含本次 PUT 的 entry）。例如若另一支 ACME client 已對同名放了一筆 value，你的 PUT 之後 `count` 會是 `2`。

### 多值範例

```bash
# 第一次 PUT（apex 驗證）
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Content-Type: application/json' \
  -d '{"value":"token-apex","ttl":120}'
# → {"status":"ok","fqdn":"...","ttl":120,"count":1}

# 第二次 PUT（wildcard 驗證，不同 value）
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Content-Type: application/json' \
  -d '{"value":"token-wildcard","ttl":120}'
# → {"status":"ok","fqdn":"...","ttl":120,"count":2}

# DNS 查詢會回傳兩個獨立的 TXT RR
dig @127.0.0.1 _acme-challenge.example.com TXT +short
# "token-apex"
# "token-wildcard"
```

---

## DELETE — 清除 ephemeral records

`DELETE` 支援兩種模式：

- **不帶 `?value=`（wipe-all）**：移除該 FQDN 下**所有** ephemeral entries，無論目前有幾筆、value 為何。
- **帶 `?value=<value>`（per-value delete）**：只移除該 FQDN 下 value 與查詢字串完全相符的那一筆 entry；其他 value 不受影響。這是 ACME DNS-01 平行驗證（apex + wildcard 同名不同 token）下收尾單一 challenge 的安全做法——直接 wipe-all 會連同另一個仍在驗證中的 token 一起被清掉。

**DELETE 只影響 ephemeral store；zone file 中的同名 record 完全不受影響**，因此不會發生透過 API 刪除正式資料的情境。

### Wipe-all

```bash
curl -X DELETE http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Authorization: Bearer secret123'
```

### Per-value delete

```bash
# URL-encode value 中任何非 URL-safe 字元（token 通常是 base64url 不需要編碼）
curl -X DELETE "http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com?value=token-apex" \
  -H 'Authorization: Bearer secret123'
```

比對規則：

- byte-exact、case-sensitive、完全不做 normalization（與 PUT 比對邏輯一致）。
- `?value=` 值的 UTF-8 bytes ≤ 255（RFC 1035 TXT character-string 上限），超過回 `400`。
- `?value=`（空字串）會回 `400`，以避免和 wipe-all（不帶 query key）混淆。
- `?value=xxx` 但 store 中無 matching entry 時回 `200`（idempotent）。

### 成功回應（200）

```json
{
  "status": "ok",
  "fqdn": "_acme-challenge.example.com."
}
```

DELETE 是冪等的——對不存在的 FQDN、或 `?value=` 無匹配，都回 `200`。多次 DELETE 同一 FQDN 亦安全。

---

## 查詢 TXT record

Ephemeral TXT record 直接透過標準 DNS 查詢取得，不需要另一個 API。當同一 FQDN 有多筆 ephemeral value 時，DNS 回應會把每筆合成**獨立的 TXT RR**（而非把多個字串塞進同一個 RR），每筆 RR 各有自己動態計算的剩餘 TTL（下限 `1`）。

```bash
dig @127.0.0.1 _acme-challenge.example.com TXT +short
# "token-apex"
# "token-wildcard"
```

### 與 zone file 的優先順序

若 zone file 已有同名的 TXT record，**zone file 優先**——ephemeral store 不會被查到，避免透過 API 意外覆蓋正式資料。

DNS 查詢分派順序（TXT qtype）：

1. Zone 精確 `(qname, TXT)` 匹配 → 命中則回傳 zone TXT
2. **Ephemeral store 覆蓋層** → 命中則回傳 ephemeral TXT（見下節）
3. RFC 1034 §3.6.2 CNAME fallback
4. RFC 4592 wildcard synthesis

此順序對 root zone 與 backup（alias）zone 一致。

### Ephemeral TXT 覆蓋 exact CNAME（僅 TXT qtype）

典型 ACME DNS-01 delegation 將 `_acme-challenge.<domain>` CNAME 到外部 acme-dns provider：

```
_acme-challenge.foo.example.com. IN CNAME acme-dns.external.net.
```

在此情況下透過 API PUT 一筆 `_acme-challenge.foo.example.com.` 的 TXT，**DNS TXT 查詢會回傳 ephemeral TXT 值，而不是 zone 上的 CNAME**。這是相對 RFC 1034 §3.6.2「CNAME 獨佔 owner name」的刻意偏離，限縮在：

- **僅 `TXT` qtype**：`dig CNAME`、`dig A`、`dig AAAA` 等其他 qtype 仍依標準 CNAME fallback 行為，不受 ephemeral store 影響
- **僅當 ephemeral store 有未過期的 entry 時**：entry 過期或未寫入時自動回退成標準 CNAME 鏈（RFC 1034 §3.6.2）
- **無狀態切換成本**：當所有 ephemeral entry 消失後，`dig TXT` 行為立刻恢復為 CNAME 跟隨結果，不需人工操作

```bash
# 寫入 ephemeral TXT
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.foo.example.com \
  -d '{"value":"token-xyz","ttl":120}'

# TXT 查詢回傳 ephemeral 值（不回 CNAME）
dig @127.0.0.1 _acme-challenge.foo.example.com TXT +short
# "token-xyz"

# CNAME 查詢仍回傳 zone 配置的 CNAME
dig @127.0.0.1 _acme-challenge.foo.example.com CNAME +short
# acme-dns.external.net.
```

此覆蓋行為同樣適用於 backup（alias）zone：若透過 API PUT `_acme-challenge.foo.backup.com.`，即使 root zone 在 `_acme-challenge.foo.example.com.` 有 CNAME，對 backup 名稱的 TXT 查詢也會回傳 ephemeral TXT。

---

## 錯誤對照

| 情境 | HTTP code | 回應 body |
|------|-----------|-----------|
| 來源 IP 不在 `allow` 清單 | `403` | `{"status":"error","error":"source IP not in allow list"}` |
| 設了 token 但 header 缺失 / 格式錯誤 | `401` | `{"status":"error","error":"missing or malformed Authorization header"}` |
| 設了 token 但值不符 | `401` | `{"status":"error","error":"invalid token"}` |
| Body 空、非 JSON、或有未知欄位 | `400` | `{"status":"error","error":"invalid JSON body: ..."}` |
| Body 缺少 `value` 欄位 | `400` | `{"status":"error","error":"missing required field: value"}` |
| PUT body 的 `value` 長度 > 255 bytes | `400` | `{"status":"error","error":"value exceeds 255-byte limit (got N)"}` |
| DELETE `?value=`（空字串）| `400` | `{"status":"error","error":"empty value query parameter"}` |
| DELETE `?value=` 長度 > 255 bytes | `400` | `{"status":"error","error":"value exceeds 255-byte limit (got N)"}` |
| PUT FQDN 不在任何已載入 zone 底下 | `422` | `{"status":"error","error":"FQDN \"...\" does not belong to any zone served by this server"}` |

### Zone 歸屬檢查（僅 PUT）

PUT 寫入 ephemeral store 之前會檢查 canonical FQDN 是否落在任一已載入的 zone origin 之下（跨所有 view、root 與 backup 兩種 role 都納入比對）。無匹配時直接回 `422 Unprocessable Entity`，store 不被修改。

此設計可捕捉 caller 端 typo（如 `_acme-challenge.exmaple.com` 誤打）造成的 silent failure：舊版本會回 `200` 但後續 DNS 查詢拿到空結果，新版本會顯式報錯。

檢查順序在 IP ACL、token、FQDN canonicalize、JSON 解析、value 長度、TTL clamp 之後；因此格式錯誤與鑑權失敗仍會回各自的 `400` / `401` / `403`，而非被覆寫為 `422`。

`DELETE` 不受此檢查影響，維持原本的冪等語意——對不在 zone 內的 FQDN 發 DELETE 仍回 `200`。

SIGHUP reload 後，新增或移除的 zone origin 會在下一次 PUT 立即反映，不需要重啟 API server。

Token 比較使用 `crypto/subtle.ConstantTimeCompare`，可抵抗 timing attack。

---

## TTL 行為與清理

| 觸發 | 效果 |
|------|------|
| TTL 到期 | Lazy eviction（query 時判斷）+ 每 30 秒一次 periodic GC 主動掃除 |
| SIGHUP 重新載入 unified config | Reload 成功後呼叫 `Store.Clear()` 清空所有 ephemeral record；reload 失敗則保留 |
| Process 重啟 | 所有 record 消失（in-memory，不持久化） |

`ttl` 上限 `3600` 秒是刻意設計的防護，避免遺忘的 record 長時間佔用記憶體。

---

## ACME client 整合提示

大多數 ACME client 可透過自訂 hook 來推送 challenge：

```bash
# certbot --manual --preferred-challenges dns --manual-auth-hook ./put-txt.sh
# put-txt.sh:
curl -X PUT "http://127.0.0.1:8053/v1/txt/_acme-challenge.${CERTBOT_DOMAIN}" \
  -H "Authorization: Bearer ${SHADOWDNS_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"${CERTBOT_VALIDATION}\",\"ttl\":120}"
```

對應的 cleanup hook——單一 client 場景（`certbot` 只驗證 apex 或 wildcard 其中一個）：

```bash
curl -X DELETE "http://127.0.0.1:8053/v1/txt/_acme-challenge.${CERTBOT_DOMAIN}" \
  -H "Authorization: Bearer ${SHADOWDNS_TOKEN}"
```

**平行驗證情境**（同時跑 apex + wildcard，或兩支 client 共用 `_acme-challenge.<domain>`）cleanup 時應改用 `?value=`，只收自己那筆 token，避免 wipe-all 誤刪另一支仍在驗證中的 challenge：

```bash
curl -X DELETE "http://127.0.0.1:8053/v1/txt/_acme-challenge.${CERTBOT_DOMAIN}?value=${CERTBOT_VALIDATION}" \
  -H "Authorization: Bearer ${SHADOWDNS_TOKEN}"
```

`lego` 可實作 `Provider` interface 包裝這兩個呼叫；`acme.sh` 則可透過 `dns_shadowdns.sh` 自訂 plugin。
