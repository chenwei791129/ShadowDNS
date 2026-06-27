## Why

維運團隊在受限內網（無 internet access、僅經 squid proxy 出網）新增或修改 DNS record 並 reload 後，目前只能用 dig 走 UDP/TCP port 53 驗證。提供一個 DoH (DNS-over-HTTPS, RFC 8484) endpoint，讓他們透過 `https://<IP>/dns-query` 以通用 DoH client（如 curl）驗證 ShadowDNS 權威 zone 的查詢結果，且查詢流量可走標準 HTTPS（443），便於穿越只放行 TCP/443 的企業防火牆。

## What Changes

- 新增 DoH endpoint：在獨立的 HTTPS listener（預設 :443）上以路徑 `/dns-query` 提供 RFC 8484 服務，支援 GET（`?dns=` base64url、去 padding）與 POST（body 為 `application/dns-message`）。
- DoH 查詢複用既有權威查詢路徑（dns-server 的 ServeDNS），不改既有 UDP/TCP 行為；DoH 僅是新增的傳輸層。
- 非遞迴語意：DoH 與既有 UDP/TCP 一致，只回 ShadowDNS 所 host 的 zone；對非本機 zone 的查詢回 REFUSED/SERVFAIL（與既有行為相同）。此限制需在文件明確標示，避免使用者誤把它當成一般遞迴 DoH resolver。
- 新增 `shadowdns.yaml` 的 `doh:` 設定區塊（Listen、TLS/ACME 相關欄位），並納入既有 SIGHUP reload 流程。
- TLS 憑證透過內嵌的 ACME client 以 Let's Encrypt 對 IP 位址自動簽發與續簽（HTTP-01 驗證、shortlived profile），含 port-80 challenge responder 與不重啟 listener 的憑證熱輪替。此為本專案第一個 TLS 使用者。
- ACME 帳號以無 contact email 註冊：`doh.acme` 不提供 email 欄位（RFC 8555 contact 為選填，shortlived 自動續簽情境下到期通知無意義），設定不接受 email，相依的程式碼一併移除。
- 新增第三方相依（首選 go-acme/lego）以支援 RFC 8738 IP identifier 與 Let's Encrypt profile 選擇，因 Go 內建 golang.org/x/crypto/acme 尚不支援 profile 選擇。

## Capabilities

### New Capabilities

- `doh-endpoint`: RFC 8484 DoH 服務，含 HTTPS listener、請求解碼、複用權威查詢路徑、TLS 憑證生命週期（ACME HTTP-01 取證／續簽／熱輪替）、SIGHUP reload 與 metrics 標籤。

### Modified Capabilities

(none)

## Impact

- Affected specs: 新增 doh-endpoint
- Affected code:
  - New:
    - internal/doh/server.go
    - internal/doh/responsewriter.go
    - internal/doh/acme.go
    - internal/doh/server_test.go
    - internal/doh/responsewriter_test.go
    - internal/doh/acme_test.go
    - docs/guides/doh.md
    - docs/guides/doh.zh.md
  - Modified:
    - internal/shadowdnscfg/config.go
    - cmd/shadowdns/main.go
    - internal/server/handler.go
    - internal/metrics/metrics.go
    - go.mod
    - go.sum
    - packaging/shadowdns.yaml.example
    - docs/configuration/shadowdns-yaml.md
    - docs/configuration/shadowdns-yaml.zh.md
    - docs/index.md
    - docs/index.zh.md
    - mkdocs.yml
    - README.md
  - Removed: (none)
- Dependencies: 新增 go-acme/lego（或等效、支援 IP identifier + LE profile 的 ACME 函式庫）。
- Operational: port 80 須對公網開放（僅服務 ACME HTTP-01 challenge），port 443 由防火牆限制來源 IP；憑證為 ~6 天短效，續簽失敗將在效期內導致 DoH 服務憑證過期。
