## 1. Drop 計數器 metric

- [x] 1.1 （對應 design 決策 3：drop 計數器帶 bounded `reason` label）在 `internal/metrics/metrics.go` 新增 counter vec `shadowdns_doh_acme_dropped_total`（namespace `shadowdns`、subsystem `doh`、單一 label `reason`），並提供遞增方法供 `internal/doh` 使用，nil metrics 時為安全 no-op。驗證：`internal/metrics` 套件測試斷言該 metric 已註冊、三個 `reason` 值（`unknown_path`/`unknown_token`/`bad_method`）各自獨立計數，nil 接收者遞增不 panic。

## 2. ACME listener 中止行為（TDD）

- [x] 2.1 [P] 在 `internal/doh/acme_test.go` 先寫失敗測試：以 `httptest` 對 `challengeResponder.Handler()` 發出「有效 token 的 GET」斷言回 HTTP 200 + 正確 key authorization 且不遞增 drop metric；對「未知路徑」「未知/空 token」「非 GET method」「結尾無斜線 `/.well-known/acme-challenge`」分別斷言連線被中止（handler panic `http.ErrAbortHandler`、無 HTTP response、無 301），且對應 `reason` 的 drop metric 各遞增 1。驗證：`make test` 中這些新測試先為 red。
- [x] 2.2 （對應 design 決策 1：以 `panic(http.ErrAbortHandler)` 中止連線，而非回 444 狀態碼或 hijack；決策 2：以手寫 `http.Handler` 取代 `http.ServeMux`）將 `challengeResponder.Handler()` 由 `http.ServeMux` 改為手寫 `http.Handler`：僅當 method 為 GET、path 具 `acmeChallengeBasePath` 前綴、且 token 命中 `c.tokens` 時回 200 + key authorization；其餘情形依 `unknown_path`/`unknown_token`/`bad_method` 遞增 drop metric 後 `panic(http.ErrAbortHandler)`。驗證：2.1 的測試全部轉 green，且 `make test`（race）通過。
- [x] 2.3 將 drop metric 注入 `challengeResponder`（沿用既有 DoH cert metric 的注入方式，nil 安全），更新 `newChallengeResponder` 及 `internal/doh/server.go` 中其建構點。驗證：`internal/doh` 套件測試斷言中止時對應 metric 遞增、`shadowdns_panics_total` 不因中止而增加；`make lint` 通過。

## 3. 整合驗證

- [x] 3.1 確認需求「TLS certificate is obtained for an IP address via ACME HTTP-01」的簽發路徑不受影響：既有 ACME 整合測試（pebble/acme integration，如 `internal/doh/acme_integration_test.go`）仍綠燈，證明有效 challenge 仍回 200、憑證簽發不受中止行為影響。驗證：`make test` 中該整合測試通過。

## 4. 雙語手冊更新

- [x] 4.1 [P] 在 `docs/operations/monitoring.md` 與 `docs/operations/monitoring.zh.md` 記載新 metric `shadowdns_doh_acme_dropped_total` 及其 `reason` label 值域，說明可用於觀測 port 80 探測量。驗證：`make docs-build`（strict）通過、雙語內容對齊。
- [x] 4.2 [P] 在 `docs/guides/doh.md` 與 `docs/guides/doh.zh.md` 補述 ACME HTTP-01 listener 對非有效 challenge 請求一律中止連線（nginx `return 444` 語意）以降低公開攻擊面的行為。驗證：`make docs-build`（strict）通過、雙語內容對齊。
