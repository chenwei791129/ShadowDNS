## 1. Root zone CNAME following（handleRootQuery）

- [x] 1.1 [P] 在 `internal/server/handler.go` 的 `handleRootQuery` 中實作 in-zone CNAME following：在 handler/alias 層實作 following loop，不修改 zone 層。當 CNAME fallback 找到 CNAME 且 target 在同一 zone 內（使用 zone origin suffix 判斷 in-bailiwick），以迴圈方式查詢 `(target, 原始 qtype)`，收集完整 CNAME chain + 最終紀錄，CNAME chain 上限設為 8。涵蓋 spec requirement「Synthesize CNAME response when qtype does not match but CNAME exists at the queried name」的 in-zone following 行為
- [x] 1.2 [P] 在 `handleRootQuery` 的 wildcard CNAME synthesis 路徑中同樣加入 in-zone following：wildcard 匹配產生的 CNAME 在 owner rewrite 後，若 target 在 zone 內則繼續 follow。涵蓋 spec scenario「Wildcard CNAME with in-zone target is followed」
- [x] 1.3 單元測試：在 `internal/server/server_test.go` 新增測試案例覆蓋 root zone 路徑的 in-zone CNAME following，包含：(a) 單層 in-zone CNAME + A record、(b) CNAME chain 2-3 層、(c) out-of-bailiwick target 只回傳 CNAME、(d) chain 深度超過 8 截斷、(e) target 無 requested qtype 只回傳 CNAME chain、(f) explicit CNAME query 不 follow、(g) wildcard CNAME with in-zone target

## 2. Backup zone CNAME following（alias.Resolve）

- [x] 2.1 在 `internal/alias/override.go` 的 `Resolve` 函式中實作 in-zone CNAME following：當 root zone 查詢回傳 CNAME 且 target 在 root zone 內，以迴圈方式繼續查詢（含 exact lookup + wildcard fallback），收集所有 RR 後統一呼叫 `RewriteRR` 轉換回 backup namespace。涵蓋 spec requirement「Follow in-zone CNAME targets during backup zone resolution」及 design decision「Backup zone path 在 root zone 空間內 follow，最後統一 rewrite」
- [x] 2.2 單元測試：在 `internal/alias/override_test.go` 新增測試案例覆蓋 backup zone CNAME following，包含：(a) backup zone in-zone CNAME + A record（owner 和 RDATA 正確 rewrite）、(b) backup zone CNAME chain、(c) backup zone out-of-bailiwick target、(d) backup zone wildcard CNAME with in-zone target

## 3. 整合測試

- [x] 3.1 在 `test/integration/` 新增整合測試檔案 `cname_following_test.go`，使用 zone test data 驗證完整查詢流程：root zone in-zone CNAME following、backup zone in-zone CNAME following、CNAME chain、out-of-bailiwick 行為、wildcard CNAME following
- [x] 3.2 在 `testdata/integration/master/` 的 zone 檔案中新增 in-zone CNAME following 所需的測試紀錄（CNAME + 對應 target A record、CNAME chain、out-of-bailiwick CNAME）

## 4. 驗證

- [x] 4.1 執行 `make test` 確認所有測試通過
- [x] 4.2 執行 `make lint` 確認無 lint 錯誤
