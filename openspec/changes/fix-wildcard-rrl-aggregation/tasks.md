## 1. RRL writer 支援 per-query 帳戶名 override

- [x] 1.1 在 `internal/ratelimit/writer.go` 的 `ResponseWriter` 增加 per-query 欄位 `responsesNameOverride string` 與方法 `SetResponsesAccountName(name string)`（設定該欄位）。
- [x] 1.2 在 `WriteMsg` 計算 `name := ImputedName(m, category)` 之後，若 `category == CategoryResponses && w.responsesNameOverride != ""`，以 `w.responsesNameOverride` 取代 `name` 再傳入 `w.limiter.Decide`。其他 category 與 override 為空時行為不變；`ImputedName`（`internal/ratelimit/key.go`）維持不變。

## 2. alias wildcard 解析回傳真正的 wildcard 節點名

- [x] 2.1 在 `internal/alias/override.go` 的 `synthesizeWildcardRRs`，於改寫迴圈**之前**擷取傳入 `wRRs[0].Header().Name`（即 `*.<rootZone.Origin>` wildcard 節點，改寫前），作為 wildcard owner 回傳；**不得**使用該函式內以 `RewriteName(qname, ...)` 算出的 per-label 改寫名作為 owner。
- [x] 2.2 擴充 `ResolveWildcard` 與 `ResolveWildcardCollapse` 的回傳值以帶出 2.1 的 wildcard owner（例如多回一個 `wildcardOwner string`）；更新 `internal/alias` 內外**所有既有呼叫點**與相關測試以配合新簽章。CNAME-fallback／exact／ephemeral 等非 wildcard 解析函式不變。

## 3. handler 於「所有」wildcard 合成點 plumb owner

- [x] 3.1 在 `internal/server` 的 `queryOpt` 增加欄位 `rrl *ratelimit.ResponseWriter` 與 `rrlWildcardOwner string`。
- [x] 3.2 在 `ServeDNS` 安裝 RRL writer 處（`w = ratelimit.NewResponseWriter(w, limiter)`）把該 typed writer 捕獲進區域變數（無 limiter 時 nil）；於稍後 `qo := parseQueryOpt(req)` **建立 qo 之後**才 `qo.rrl = rlWriter`（不可在 writer 安裝行寫 `qo.rrl`，該處 qo 尚不存在）。
- [x] 3.3 root zone 直接 wildcard：在 `handleRootQuery` 的 `rootZone.LookupWildcard(qname, qtype)` 命中分支入口，設 `qo.rrlWildcardOwner = dnsutil.LookupKey(wRRs[0].Header().Name)`（改寫前的 `*.parent`）。此涵蓋隨後的直接 `replyWithAnswer` 與經 `collapseRootCNAME`（收 qo by value，複本帶 owner）的回應。
- [x] 3.4 root zone wildcard-CNAME chain：在 `rootZone.LookupWildcard(qname, dns.TypeCNAME)` 命中分支，設 `qo.rrlWildcardOwner = dnsutil.LookupKey(wCNAMEs[0].Header().Name)`；涵蓋其 `collapseRootCNAME` 與 `FollowCNAME` 直接回應兩出口。
- [x] 3.5 alias 非 collapse wildcard：在呼叫 `alias.ResolveWildcard(...)` 取得 records 與 wildcard owner 後、於 `replyWithAnswer` 前，設 `qo.rrlWildcardOwner = dnsutil.LookupKey(owner)`。
- [x] 3.6 alias collapse wildcard：在 collapse 分支呼叫 `alias.ResolveWildcardCollapse(...)` 且其產生 records 時，於呼叫捕獲外層 `qo` 的 `answeredCollapsed` 閉包**之前**設 `qo.rrlWildcardOwner = dnsutil.LookupKey(owner)`；`ResolveCNAMEFallbackCollapse`（真實 CNAME 鏈）產生的答案**不**設 owner。
- [x] 3.7 在 `replyWithAnswer` 的 `w.WriteMsg(m)` 之前加入：`if qo.rrl != nil && qo.rrlWildcardOwner != "" { qo.rrl.SetResponsesAccountName(qo.rrlWildcardOwner) }`。因所有 positive-answer 出口收斂於此，僅需各 wildcard 分支正確設好 owner。

## 4. 回歸測試（僅測本專案自有的 RRL 帳戶邏輯）

- [x] 4.1 在 `internal/ratelimit/writer_test.go` 新增測試：對分類為 `CategoryResponses` 的回應，設 `SetResponsesAccountName("*.example.com.")` 後，以攔截 `Decide` 參數的 fake limiter 斷言帳戶名為該 override；未設 override 或 category 非 `CategoryResponses` 時仍為 `ImputedName` 的原結果。
- [x] 4.2 在 `internal/server/handler_ratelimit_test.go` 新增測試：對服務 `*.example.com` 的 zone，從同一 client block 送多個不同隨機 label 的查詢（wildcard 合成 NOERROR），以低 `responses-per-second` 斷言超額後被限流（聚合到單一 `{block, responses, *.example.com.}` 帳戶）。分別覆蓋：(a) root-zone 直接 wildcard、(b) root wildcard-CNAME chain、(c) alias `ResolveWildcard`、(d) alias `ResolveWildcardCollapse`（collapse 開啟），確認四條路徑皆聚合。
- [x] 4.3 在 `internal/alias` 既有測試補充：`ResolveWildcard`／`ResolveWildcardCollapse` 回傳的 wildcard owner 為 `*.<origin>` 節點（非 per-label 改寫名）。
- [x] 4.4 不回歸測試：兩個不同的非 wildcard（精確匹配真實紀錄）positive answer 仍分屬不同帳戶（各以精確 query name 為 key）；CNAME-fallback 產生的答案以 query name 為 key（不被誤設 wildcard owner）；NXDOMAIN/NODATA 對 zone origin、errors 對空名稱的既有聚合不變。

## 5. 手冊與驗證

- [x] 5.1 檢視 MkDocs 手冊中 RRL 相關頁（`docs/` 下 response-rate-limiting / monitoring 相關），評估此 wildcard 聚合行為是否需補一句「wildcard 合成的 positive answer 依 wildcard owner 聚合，與 BIND 一致」；若需更新則同步 `.md` 與 `.zh.md` 兩語系並跑 `make docs-build`（strict）。若判定不需更新（純為使 RRL 符合既有 BIND 相容承諾、無新設定/CLI），明確記錄此結論。**結論：手冊無需更新**——grep 確認 docs 無描述 RRL 帳戶 keying 的 per-name/wildcard 細節、亦無 responses-per-second 與 wildcard 共提；本變更無新設定/CLI、非 wildcard 行為不變，僅使 RRL 對 wildcard zone 更貼合既有 BIND 相容承諾。
- [x] 5.2 執行 `make test`（race detector）與 `make lint`，確認 `internal/ratelimit/*_test.go`、`internal/server/handler_ratelimit_test.go`、`internal/alias` 既有與新增測試全數通過、無 lint 問題，且行為符合 `response-rate-limiting` spec 需求「Account key construction with name imputation」底下新增的「Wildcard-synthesized positive-answer flood aggregates per wildcard owner」scenario。
- [ ] 5.3 請使用者確認：本變更為 hot-path（`internal/ratelimit` + `internal/server` 於每筆 UDP 回應路徑）；實作與 review chain 完成後需依 Perf-Guard 在 ns2 跑 baseline → 部署 → 重測，確認 QPS 未下降 > 5% 且 p99 未上升 > 15%。
