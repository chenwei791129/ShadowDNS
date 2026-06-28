## Summary

將 port 80 的 ACME HTTP-01 listener 改為 nginx `return 444` 語意：只有針對目前有效 challenge token 的 GET 請求回 200，其餘所有請求一律中止連線、不回任何 HTTP response。

## Motivation

`doh.acme.http01_listen` 依設計必須對全世界開放在 port 80（ACME HTTP-01 驗證一律連 port 80），是 ShadowDNS 真正完全公開的攻擊面。目前此 listener 對任何非 challenge 路徑回 HTTP 404（帶 `Server` header 與 body），等於對掃描器昭告「這裡有一台 HTTP server」，提供可指紋化的回應並產生噪音。此 listener 的合法流量只有一種——ACME validator 對「我們剛 Present 的 token」發出的 GET——因此其餘一切都能安全地直接斷線，最大化降低公開攻擊面與指紋暴露。

## Proposed Solution

- 以手寫 `http.Handler` 取代 `challengeResponder.Handler()` 內的 `http.ServeMux`，消除 ServeMux 對 subtree pattern 結尾無斜線時自帶的 301 redirect 指紋。
- 僅當「method 為 GET」且「路徑位於 `/.well-known/acme-challenge/` 之下」且「token 命中目前已 Present 的授權」時回 HTTP 200 + key authorization。
- 其餘所有情形——未知路徑、`/.well-known/acme-challenge/` 下未知或空 token、非 GET method——以 `panic(http.ErrAbortHandler)` 中止連線：HTTP/1.1 下關閉連線、不送任何 response byte，且不輸出 stack trace。此路徑不經 `internal/server` 的 DNS panic recover（該 recover 僅在 port 443 的 `ServeDNS` 路徑），故 `shadowdns_panics_total` 不受影響。
- 新增計數器 `shadowdns_doh_acme_dropped_total`（subsystem `doh`），帶 `reason` label（bounded 值：`unknown_path`、`unknown_token`、`bad_method`），每次中止連線前依情形遞增,保留「被探測量」與探測型態的可觀測性。

## Non-Goals

- 不變更 port 443 DoH（`/dns-query`）對未知路徑回 404、對錯誤 method 回 405 的現有行為；該強化列為日後 follow-up。
- 不新增任何 config 開關控制此行為（v0.x 實驗階段，行為恆開）。
- 不改變 ACME 憑證簽發、續期或 SIGHUP reload 流程；有效 challenge 仍正常回 200，憑證可正常簽發。
- 不嘗試在 TLS 層之前隱藏 listener 的存在（無法做到，且非本次目標）。

## Alternatives Considered

- **回 HTTP 444 狀態碼（`w.WriteHeader(444)`）**：仍會送出一整包 `HTTP/1.1 444` response，留下指紋，與 nginx 行為相反、達不到降低攻擊面的目的。
- **以 `http.Hijacker` 劫持連線後關閉**：port 80 為純 HTTP/1.1 雖可用，但 `panic(http.ErrAbortHandler)` 更簡潔、同時涵蓋 h1/h2 語意且不需手動管理連線。
- **保留 ServeMux 僅替換 404 handler**：無法消除 ServeMux 對結尾無斜線 subtree 的 301 redirect 指紋，故改用手寫 handler。

## Impact

- Affected specs: doh-endpoint（修改「TLS certificate is obtained for an IP address via ACME HTTP-01」需求中 HTTP-01 listener 對非 challenge 請求的行為，並新增 drop 計數器的行為）
- Affected code:
  - Modified:
    - internal/doh/acme.go
    - internal/doh/server.go
    - internal/metrics/metrics.go
    - docs/operations/monitoring.md
    - docs/operations/monitoring.zh.md
    - docs/guides/doh.md
    - docs/guides/doh.zh.md
  - New: (none)
  - Removed: (none)
