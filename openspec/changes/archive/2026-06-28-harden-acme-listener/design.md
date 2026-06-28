## Context

`internal/doh/acme.go` 的 `challengeResponder.Handler()` 目前用 `http.ServeMux` 服務 port 80 的 ACME HTTP-01 listener：對 `/.well-known/acme-challenge/<token>` 命中已 Present 的 token 時回 200 + key authorization，token 未命中時回 404，並以 catch-all `/` 對其餘所有路徑回 404。

此 listener 由 `internal/doh/server.go` 的 `runWith` 透過 `httpserver.NewServer(s.cfg.ACME.HTTP01Listen, responder.Handler())` 啟動，是純 HTTP/1.1（未啟用 h2c），與 port 443 的 DoH HTTPS server 是兩個獨立 listener。依規格 `doh.acme.http01_listen` 必須對全世界開放在 port 80，是 ShadowDNS 唯一完全公開的 HTTP 攻擊面。目前對任意路徑回 404 會洩漏「此處有 HTTP server」的指紋並產生掃描噪音。

`internal/server/handler.go` 內唯一的 `recover()` 只包住 port 443 的 `ServeDNS` query path，不在此 port 80 handler 的呼叫路徑上。既有 DoH metric 慣例為 subsystem `doh`（例如 `shadowdns_doh_cert_renewals_total`）。

## Goals / Non-Goals

**Goals:**

- 將 port 80 ACME HTTP-01 listener 改為 nginx `return 444` 語意：只有「有效 challenge token 的 GET」回 200，其餘一律中止連線、不回任何 HTTP response。
- 消除 ServeMux 對結尾無斜線 subtree 的 301 redirect 指紋。
- 以一個帶 `reason` label 的計數器保留被探測量的可觀測性。

**Non-Goals:**

- 不變更 port 443 DoH 對未知路徑/錯誤 method 的 404/405 行為。
- 不新增 config 開關（v0.x 行為恆開）。
- 不改動 ACME 簽發/續期/SIGHUP reload 流程。

## Decisions

### 決策 1：以 `panic(http.ErrAbortHandler)` 中止連線，而非回 444 狀態碼或 hijack

`http.ErrAbortHandler` 是 Go net/http 用來「中止 response、不回任何內容」的哨兵 panic 值；HTTP/1.1 下 server 會關閉連線、不送任何 byte，且 net/http 會抑制該請求的 stack trace log。

- **替代方案 A：`w.WriteHeader(444)`** — 仍會送出一整包 `HTTP/1.1 444` response，留下指紋，與目標相反。
- **替代方案 B：`http.Hijacker` 劫持後 `Close()`** — port 80 為純 HTTP/1.1 雖可用，但需手動管理連線；`ErrAbortHandler` 更簡潔且同時涵蓋 h1/h2 語意。

由於此 handler 不經 `internal/server` 的 DNS panic recover，`ErrAbortHandler` 會正常傳遞至 net/http 的 per-request recover 完成中止，且不會被計入 `shadowdns_panics_total`。

### 決策 2：以手寫 `http.Handler` 取代 `http.ServeMux`

`http.ServeMux` 對註冊為 subtree 的 pattern，在收到結尾無斜線的請求（`/.well-known/acme-challenge`）時會自行回 301 redirect 到斜線版本——這個 301 會繞過 handler 內的中止邏輯，仍是可觀測的指紋。改用手寫 handler 直接以字串前綴與 method 判斷，杜絕任何 mux 自帶的 redirect。

- **替代方案：保留 ServeMux、僅替換 404 handler** — 無法消除上述 301，故捨棄。

### 決策 3：drop 計數器帶 bounded `reason` label

新增 `shadowdns_doh_acme_dropped_total`（subsystem `doh`），`reason` label 取 `unknown_path` / `unknown_token` / `bad_method` 三個固定值，cardinality 受控，且能分辨探測型態。沿用既有 DoH metric 的 subsystem 命名慣例。

## Implementation Contract

**Behavior（operator/掃描器可觀測）：**

- 對 port 80 ACME listener 發出「method=GET 且 path 在 `/.well-known/acme-challenge/` 下且 token 命中目前已 Present 授權」的請求 → HTTP 200 + key authorization（與現狀一致，ACME 簽發不受影響）。
- 其餘所有請求 → 連線被中止，client 收到 connection reset／EOF，完全沒有 HTTP status line／header／body，server 端不輸出 stack trace。

**Interface / data shape：**

- `challengeResponder.Handler()` 回傳的 `http.Handler` 改為手寫實作（取代內部 `http.NewServeMux()`），其判斷邏輯：GET + `strings.HasPrefix(path, acmeChallengeBasePath)` + token 命中 `tokens` → 200；否則依情形遞增 metric 後 `panic(http.ErrAbortHandler)`。
- 新增 metric：`shadowdns_doh_acme_dropped_total`，type counter vec，namespace `shadowdns`、subsystem `doh`、單一 label `reason`，值域 `{unknown_path, unknown_token, bad_method}`。需在 `internal/metrics/metrics.go` 註冊並提供遞增方法，依既有 DoH metric（cert renewals）的注入方式提供給 `internal/doh`。

**Failure modes：**

- token 命中判斷沿用既有 `c.tokens.Load(token)`；未命中即視為 `unknown_token`。
- 空 token（請求正好是 `/.well-known/acme-challenge/`）歸類 `unknown_token`。
- metric 物件為 nil 時（無 metrics）遞增需為安全 no-op，與既有 DoH cert metric 的 nil 處理一致。

**Acceptance criteria：**

- 單元測試（`internal/doh/acme_test.go` 或同套件 handler 測試）以 `httptest` 對手寫 handler 發出五種請求並斷言：有效 token GET → 200 + 正確 body；未知路徑、未知/空 token、非 GET → handler panic `http.ErrAbortHandler`（或經 net/http server 後連線被中止、無 response），且對應 `reason` 的 metric 遞增 1。
- 針對結尾無斜線 `/.well-known/acme-challenge` 的測試斷言不產生 301、被歸類 `unknown_token` 並中止。
- metric 測試斷言三個 `reason` 值各自獨立計數，且 `shadowdns_panics_total` 不因中止而增加。
- `make test`（race）與 `make lint` 通過。

**Scope boundaries：**

- In scope：`internal/doh/acme.go` 的 `Handler()` 與 `newChallengeResponder`、`internal/doh/server.go` 中 responder 的建構點（注入 drop metric）、`internal/metrics/metrics.go` 的新 metric、相關測試、雙語手冊（monitoring 與 doh guide）。
- Out of scope：port 443 DoH handler、config schema、ACME 簽發/續期/reload 邏輯。

## Risks / Trade-offs

- **[中止連線使 LE 驗證失敗]** → 僅在「非有效 token」時中止；LE validator 只會請求我們剛 Present 的 token，該路徑仍回 200，故簽發不受影響。以整合測試（既有 pebble/acme integration）確認簽發流程綠燈。
- **[失去 404 對除錯/健康檢查的可見性]** → 接受；以 `shadowdns_doh_acme_dropped_total` 提供探測量可觀測性取代。v0.x 實驗階段不提供關閉開關。
- **[未來若於 port 80 啟用 h2c]** → `ErrAbortHandler` 在 h2 下亦為中止語意（RST_STREAM），行為仍正確；目前未啟用 h2c，無立即影響。

## Migration Plan

- 純行為強化，無資料遷移、無 config 變更。部署後對既有有效 ACME 流程無影響。
- Rollback：還原 `internal/doh/acme.go` 與 `internal/metrics/metrics.go` 即可恢復 404 行為。

## Open Questions

（無）
