# DNS-over-HTTPS (DoH)

ShadowDNS 在 `/dns-query` 提供一個 RFC 8484 的 DNS-over-HTTPS 端點，重用與 UDP/TCP listener 相同的權威查詢路徑。其用途偏向維運：讓操作者能透過標準 HTTPS（TCP/443）驗證 zone 紀錄——例如穿過只放行 TCP/443 的防火牆或 middlebox——而不必開放 port 53。DoH 查詢會先被解碼，交給 UDP/TCP 路徑使用的同一個 handler，再把 wire-format 答案透過 HTTPS 回傳。

!!! warning
    **ShadowDNS DoH 是權威（AUTHORITATIVE）且非遞迴（NON-RECURSIVE）的。** 它只回答 ShadowDNS 所託管的 zone；任何 out-of-zone 查詢都回 REFUSED。它**不是**通用的遞迴 DoH resolver——請**不要**把瀏覽器或用戶端裝置指向它並期待它做公共名稱解析。它的存在只是為了透過 HTTPS 驗證 ShadowDNS 自身的權威紀錄，僅此而已。

---

## 啟用方式

DoH 完全透過 [`shadowdns.yaml`](../configuration/shadowdns-yaml.md) 中的 `doh:` 區段設定。當此區段不存在時，**不會啟動任何 DoH 伺服器**，且二進位檔的行為與沒有此功能的版本完全相同。

必填欄位如下：

| 欄位 | 用途 |
|------|------|
| `listen` | DoH HTTPS 服務綁定的位址（TCP/443） |
| `acme.email` | 註冊於 ACME 帳號的聯絡信箱 |
| `acme.directory_url` | ACME directory 端點（例如 `https://acme-v02.api.letsencrypt.org/directory`） |
| `acme.ip` | 簽發憑證所對應的公開 IP |
| `acme.http01_listen` | ACME HTTP-01 challenge 回應器綁定的位址（TCP/80） |

完整欄位表與範例區塊見 [`shadowdns.yaml`](../configuration/shadowdns-yaml.md)。

---

## RFC 8484 協定

端點在 `/dns-query` 路徑上同時接受 GET 與 POST：

- **GET** `/dns-query?dns=<base64url-no-padding>` — DNS 查詢訊息以 base64url 編碼（不帶 padding）放在 `dns` query 參數中。
- **POST** `/dns-query` — 原始 DNS 查詢訊息為 request body，`Content-Type` 為 `application/dns-message`。

回應一律以 `Content-Type: application/dns-message` 回傳。

錯誤處理：

| 情況 | 狀態碼 |
|------|--------|
| 非 `/dns-query` 的路徑 | `404 Not Found` |
| 非 GET 或 POST 的方法 | `405 Method Not Allowed` |
| 無法解碼成 DNS 訊息的請求 | `400 Bad Request` |
| POST body 大於 65535 bytes | `413 Payload Too Large` |

---

## curl 範例

```bash
# GET：base64url 編碼（不帶 padding）的 DNS 查詢放在 `dns` 參數
curl -sS 'https://203.0.113.10/dns-query?dns=AAABAAABAAAAAAAAA3d3dwdleGFtcGxlA2NvbQAAAQAB' \
  | xxd

# POST：原始 DNS 訊息作為 request body
curl -sS -H 'content-type: application/dns-message' \
  --data-binary @query.bin \
  https://203.0.113.10/dns-query | xxd
```

要產生 `query.bin`，可擷取一段 wire-format 查詢——例如用 `dig +noedns +qr www.example.com A` 取出 request bytes，或任何能輸出原始 DNS 訊息的工具。

---

## TLS 與憑證

DoH listener 以一張**為 IP 位址**（`acme.ip`）簽發的憑證提供 TLS，該憑證透過 ACME HTTP-01 驗證自動取得，採用 Let's Encrypt 的短期憑證 profile（約 6 天效期）。ShadowDNS 會在到期前充分提早自動續期，並把新憑證**不重啟**地熱替換進運行中的 listener——進行中與後續的連線都會透明地接上新憑證。

由於憑證綁定的是 IP 而非主機名，用戶端直接連到該 IP（如上方 curl 範例）。

---

## 防火牆與 port 部署

DoH 使用兩個 TCP port，曝險需求差異很大：

- **Port 80**（`acme.http01_listen`）**必須能從公開 Internet 連到**，ACME 伺服器才能完成 HTTP-01 驗證。此回應器**只**服務 `/.well-known/acme-challenge/`——其他所有路徑都回 `404`。它不承載任何 DNS 資料。
- **Port 443**（`listen`，DoH 服務）**應以防火牆限制為受信任的來源 IP**。它**不需要**讓 ACME 伺服器連到，只需讓用來驗證紀錄的操作者連到。

典型部署會把 port 80 對全世界開放（僅限 challenge），並把 port 443 限制在一小份操作者位址的 allowlist 內。

---

## 來源 IP 與 view

DoH 的 view 選擇使用 **TCP 連線的來源 IP**——也就是 ShadowDNS 在傳輸層觀察到的位址。`X-Forwarded-For` 與 `Forwarded` HTTP header 會被**忽略**。這是刻意設計的安全邊界：用戶端無法藉由設定 header 來偽造 view。

---

## Cache header

每個 DoH 回應都帶有 `Cache-Control: max-age=N` header，其中 `N` 受回應中最小的 Answer TTL 上限約束。對於沒有正效期答案的回應（空 answer 區段），`N` 為 `0`。

---

## 可觀測性

DoH 查詢與 UDP、TCP 一同呈現在標準 metrics 中：

- `shadowdns_dns_requests_total` 帶有 `proto="doh"` label，與 `proto="udp"`、`proto="tcp"` 區隔，因此可單獨統計與追蹤 DoH 流量速率。
- `shadowdns_doh_cert_renewals_total{result="success"|"failure"}` 依結果計數憑證續期嘗試。
- `shadowdns_doh_cert_not_after_timestamp_seconds` 以 Unix timestamp 記錄目前憑證的到期時間，可用於即將到期的告警。

這些 metrics 如何被 scrape 與做成 dashboard，見[監控](../operations/monitoring.md)。

---

## Reload 行為（SIGHUP）

`doh:` 區段會在 SIGHUP 時重新驗證，但對 `doh.listen` 或任何 `doh.acme.*` 欄位的變更**需要重啟程序**才會生效——listener 與 ACME 帳號是在啟動時建立的。當 reload 偵測到這類變更時，ShadowDNS 會記錄一筆 advisory 日誌，說明需要重啟；在此之前運行中的 listener 會沿用先前的設定。

---

## 延伸閱讀

- [`shadowdns.yaml`](../configuration/shadowdns-yaml.md) — `doh:` 區段的欄位參考與範例。
- [CLI 參考](../reference/cli.md) 中的相關旗標。
- [監控](../operations/monitoring.md) 中上述的 DoH metrics。
