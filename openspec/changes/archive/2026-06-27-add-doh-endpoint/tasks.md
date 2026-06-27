## 1. 前置驗證（spike）

- [x] 1.1 依 design「採用 go-acme/lego 作為 ACME 函式庫」，以最小程式驗證所選 lego 版本同時支援 (a) RFC 8738 IP identifier、(b) Let's Encrypt shortlived profile 選擇、(c) HTTP-01。驗證方式：對 pebble（本地 ACME 測試伺服器）成功簽出一張 IP identifier 的憑證並印出其效期；若 lego 不支援則記錄結論並回報使用者改評估 certmagic 或 fork x/crypto。spike 結論寫入 design.md 的 Open Questions。

## 2. 設定與 reload

- [x] 2.1 實作 spec「DoH server listens on a configured address」：在 internal/shadowdnscfg 新增 `doh:` 區塊，正規化 YAML 欄位為 `doh.listen`、`doh.acme.email`、`doh.acme.directory_url`、`doh.acme.ip`、`doh.acme.http01_listen`，比照 EphemeralAPIConfig 的 buildXxx 驗證器；缺少任一必要欄位時載入失敗並指名該欄位。驗證：internal/shadowdnscfg 單元測試覆蓋有效設定解析、缺 `acme.ip` 等必要欄位回指名錯誤、以及 `doh` 區塊缺席時回傳 nil 設定。
- [x] 2.2 實作 design「DoH 設定置於 shadowdns.yaml 的 doh 區塊並納入 SIGHUP reload」與 spec「DoH configuration is re-validated on SIGHUP; listener changes require a restart」：SIGHUP 重新解析並驗證 `doh` 區塊、錯誤如其他區塊一樣回報，但 `doh.listen` 與 `doh.acme.*` 變更不在 reload 即時套用，而是記錄「需重啟」並維持啟動時 listener（比照既有 listen-address drift 行為）。驗證：reload 測試斷言無效 `doh` 設定於 SIGHUP 被回報且現有 server 不變、變更 `doh.listen` 後 SIGHUP 記錄需重啟且仍綁原位址。

## 3. DoH 請求處理核心

- [x] 3.1 實作 design「以 synthetic dns.ResponseWriter 橋接 HTTP 並複用 ServeDNS」與 spec「DoH derives the client source IP from the HTTP connection」：在 internal/doh 新增合成 `dns.ResponseWriter`，`RemoteAddr()` 回傳 HTTP 連線對端 TCP 位址（不解析 X-Forwarded-For／Forwarded）、`WriteMsg`/`Write` 擷取封包，呼叫既有 server.ServeDNS。驗證：internal/doh/responsewriter_test.go 斷言 RemoteAddr 驅動正確 view 選擇、帶 `X-Forwarded-For` 不改變 view 選擇、擷取的回應 bytes 與直接 ServeDNS 一致。
- [x] 3.2 實作 design「RFC 8484 請求解碼與回應編碼」與 spec「DoH endpoint implements RFC 8484 GET and POST」：handler 解析 GET `?dns=` base64url（去 padding）與 POST body，請求/回應 Content-Type 為 application/dns-message，非 /dns-query 路徑回 404、不支援方法回 405。驗證：internal/doh/server_test.go 覆蓋 GET、POST、404、405，並以 RFC 8484 範例字串 `AAABAAABAAAAAAAAA3d3dwdleGFtcGxlA2NvbQAAAQAB` 斷言解碼為 `www.example.com. A` 查詢。
- [x] 3.3 實作 spec「DoH reuses the authoritative query path and is non-recursive」：DoH 回應與等價 TCP 查詢逐位元組一致（RCODE 與 answer），且對非本機 zone 不做遞迴、回 REFUSED。驗證：internal/doh/server_test.go 比對 in-zone 查詢 DoH 與 TCP answer bytes 一致、out-of-zone 查詢回 RCODE REFUSED 且 Answer 為空。
- [x] 3.4 實作 spec「DoH response cache headers are bounded by the minimum answer TTL」：依 Answer 區最小 TTL 設定 Cache-Control max-age，空 Answer 不宣告正向快取。驗證：internal/doh/server_test.go 以 spec 的 TTL 表（300,60→≤60；0→0；empty→無正向 max-age）斷言。
- [x] 3.5 實作 spec「Malformed DoH requests are rejected with HTTP 400」：無法解碼為 DNS 訊息的請求（壞 base64url、缺 dns 參數、空 POST body）回 400 且不進查詢路徑。驗證：internal/doh/server_test.go 覆蓋壞 base64url 與空 body 回 400。
- [x] 3.6 實作 spec「DoH server enforces request size and timeout limits」：DoH HTTPS server 與 HTTP-01 listener 設讀寫/idle timeout，並對 POST body 設不小於 65535 bytes 的上限，超過回 413 且不進查詢路徑。驗證：internal/doh/server_test.go 斷言超量 POST body 回 413、server 設有非零 timeout。

## 4. TLS 憑證與 ACME

- [x] 4.1 依 spike 結論加入 ACME 函式庫相依（首選 go-acme/lego）至 go.mod/go.sum。驗證：`go mod tidy` 後 `make build` 成功編譯，相依出現在 go.mod。
- [x] 4.2 實作 design「TLS 憑證經 Let's Encrypt HTTP-01 與 shortlived profile 對 IP 自動簽發」、design「Port 80 challenge responder 與 port 443 DoH 服務分離」與 spec「TLS certificate is obtained for an IP address via ACME HTTP-01」：以 HTTP-01 + shortlived profile 對設定 IP 取得憑證；port 80 獨立 listener 僅回應 /.well-known/acme-challenge/、其餘 404；443 不需對 ACME 伺服器可達。驗證：internal/doh/acme_test.go 以可注入的 ACME 目錄（pebble 或 mock）斷言取證成功、port 80 challenge 路徑回 key authorization、非 challenge 路徑回 404。
- [x] 4.3 實作 design「憑證熱輪替以 GetCertificate callback 搭配 atomic 指標」與 spec「TLS certificate is renewed and hot-swapped without restart」：TLS 設定以 GetCertificate callback 從 atomic 指標取憑證，背景於到期前續簽並原子替換、不重啟 listener；續簽失敗記錄 log 與 metric 且保留現有有效憑證。驗證：internal/doh/acme_test.go 斷言續簽後新 handshake 取得新憑證且 listener 未重啟、續簽失敗時仍續用舊憑證並記錄失敗。

## 5. 啟動整合與關閉

- [x] 5.1 實作 spec「DoH server shuts down gracefully」：在 cmd/shadowdns/main.go 的 run() 於 ephemeral-API／metrics 區塊旁啟動 DoH HTTPS server 與 port-80 listener goroutine，傳入 *server.Server；主 context 取消時兩者 graceful shutdown（在途請求最多等 5 秒）。驗證：啟動整合測試斷言設定 `doh` 後 DoH 埠可連、SIGTERM 後停止接受新連線。

## 6. 觀測

- [x] 6.1 實作 design「DoH 查詢在 Prometheus metrics 以獨立 proto 標籤呈現」與 spec「DoH queries are labeled distinctly in metrics」：合成 DoH writer 暴露其傳輸（如 `Protocol() string`），internal/server/handler.go 的 proto 判定先以介面斷言取得 `"doh"`、否則退回 `dnsutil.IsUDP` 的 udp/tcp（internal/metrics 不需改 proto 機制）。驗證：handler 或 internal/doh 測試斷言 DoH 查詢在 `proto` 標籤遞增有別於 udp/tcp 的值。
- [x] 6.2 實作 spec「TLS certificate is renewed and hot-swapped without restart」的可觀察性部分：在 internal/metrics 註冊憑證續簽結果與到期時間 metric，續簽失敗時遞增失敗計數並記 log。驗證：internal/metrics 測試斷言續簽失敗會反映於該 metric。

## 7. 文件與打包

- [x] 7.1 [P] 更新 `shadowdns.yaml` 設定文件，新增 `doh:` 欄位表與範例（雙語 docs/configuration/shadowdns-yaml.md 與 .zh.md），IP 用 RFC 5737、域名用 RFC 2606。驗證：內容審閱欄位齊全且雙語一致。
- [x] 7.2 [P] 新增 DoH 功能指南（雙語 docs/guides/doh.md 與 docs/guides/doh.zh.md），明確標示「只回本機 zone、非遞迴」與 port 80/443 防火牆部署，並在 mkdocs.yml 的 nav 與 nav_translations 各加一筆。驗證：`make docs-build`（strict）通過，無斷鏈與 nav 不匹配。
- [x] 7.3 [P] 更新 docs/index.md 與 docs/index.zh.md 的功能比較表，以及 README.md 的 features/planned 清單，將 DoH 由 planned 改為 supported。驗證：內容審閱與 `make docs-build` 通過。
- [x] 7.4 [P] 在 packaging/shadowdns.yaml.example 加入註解掉的 `doh:` 範例區塊（沙盒化 IP/域名）。驗證：內容審閱，example 與設定文件欄位一致。

## 8. 完成驗證

- [x] 8.1 執行 `make test`（race）、`make lint` 與 `make docs-build` 全數通過。驗證：三個命令皆 exit 0。
- [x] 8.2 對暫存 diff 執行 CLAUDE.local.md 的 sanitize grep gate，確認無非 RFC 2606 域名與非保留範例 IP 外洩。驗證：grep gate 無命中。

## 9. 移除 ACME email 欄位

- [x] [P] 9.1 實作 spec「TLS certificate is obtained for an IP address via ACME HTTP-01」更新後的「ACME account SHALL be registered without a contact email / email 欄位不被接受」與 design「不設定 ACME 帳號 email（無 contact）」：移除設定層的 email 欄位與其依賴程式碼。設定層（internal/shadowdnscfg/config.go）移除 `DoHACMEConfig.Email`、`rawDoHACME.Email` 與 `buildDoHACME` 的 email 必填檢查；ACME 層（internal/doh/acme.go）移除 `acmeUser.email` 欄位與 `newLegoObtainer` 對 `cfg.Email` 的引用，`acmeUser.GetEmail()` 改回傳空字串以滿足 lego `registration.User` 介面（lego 即以空 Contact 註冊帳號）。同一任務一併完成以保持工作樹可編譯。TDD：先改 internal/shadowdnscfg 單元測試（`doh_test.go`）——把「missing acme.email」案例由「預期失敗」改為「設定不含 email 仍載入成功」、新增「`doh.acme.email` 出現時以未知欄位被拒」案例、並從 `validDoHYAML` 移除 email 行——使測試先紅，再移除程式碼使其綠；同步更新 `internal/doh/acme_integration_test.go` 移除 `Email` 設定。驗證：`go test ./internal/shadowdnscfg/ ./internal/doh/`（race）通過，且 grep `Email`/`email` 於 config.go 與 acme.go 無殘留。
- [x] [P] 9.2 更新設定文件雙語版（docs/configuration/shadowdns-yaml.md 與 docs/configuration/shadowdns-yaml.zh.md）：自 `doh` 欄位表移除 `acme.email` 列、自 YAML 範例移除 `email:` 行，並調整內文不再提及 email 必填。沙盒化規則照舊（RFC 2606 域名 / RFC 5737 IP）。驗證：`make docs-build`（strict）通過，雙語欄位一致且皆無 email。
- [x] [P] 9.3 更新 packaging/shadowdns.yaml.example：自註解掉的 `doh:` 範例區塊移除 `email:` 行與其說明註解，使 example 與設定文件欄位一致。驗證：內容審閱，example 不含 email 且其餘 `doh.acme.*` 欄位齊全。
- [x] 9.4 完成驗證：執行 `make test`（race）、`make lint`、`make docs-build`（strict）全數通過，並對暫存 diff 重跑 CLAUDE.local.md 的 sanitize grep gate。驗證：四項皆通過/無命中。
