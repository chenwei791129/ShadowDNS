## 1. Store layer — DeleteValue

- [x] 1.1 在 `internal/ephemeral/store_test.go` 新增 TDD failing test，對應 spec `DeleteValue removes a single ephemeral entry by value` 的 scenarios：只刪 matching entry、無匹配回 false、刪除最後一筆時移除 FQDN key、unknown FQDN 回 false、canonicalize FQDN。
- [x] 1.2 在 `internal/ephemeral/store.go` 實作 `DeleteValue(fqdn, value string) bool`：取寫鎖 → 尋找符合 value 的 entry → `slices.Delete` 移除 → 若 slice 空則 `delete(s.records, canonical)` → 回傳是否有刪除。跑 `go test -race` 直到 1.1 的測試全綠。
- [x] 1.3 補 race/並發 sanity：確認 store 在 concurrent Put + DeleteValue 混用時不會 panic（已有的 race detector 覆蓋即可；檢查是否需要額外 test）。

## 2. API layer — DELETE `?value=` selector + value length validation

- [x] 2.1 [P] 在 `internal/api/server_test.go` 新增 failing test 覆蓋 spec `DELETE endpoint removes all ephemeral TXT entries for an FQDN` 的新 scenarios：`?value=X` 只刪 matching、`?value=token-X` 無匹配回 200 並保留原 entry、`?value=`（空字串）回 400、`?value=<>255 bytes>` 回 400、不帶 value 維持 wipe-all 行為。
- [x] 2.2 [P] 在 `internal/api/server_test.go` 新增 failing test 覆蓋 spec `PUT endpoint adds or refreshes an ephemeral TXT value` 的新 scenario：PUT body 的 `value` > 255 bytes 時回 400，store 不變。
- [x] 2.3 在 `internal/api/server.go` 新增共用輔助 `validateValue(v string) error`（長度 ≤ 255 bytes；空字串規則由 caller 決定）。
- [x] 2.4 在 `internal/api/server.go` 修改 `handlePut`：decode body 後、呼叫 store 前套用 `validateValue`；違反時 400。跑 2.2 測試至綠。
- [x] 2.5 在 `internal/api/server.go` 修改 `handleDelete`：讀 `r.URL.Query().Get("value")`；未帶 query key → 呼叫 `store.Delete`（wipe all）；帶 query key 但空字串 → 400；帶非空 value → `validateValue` → 呼叫 `store.DeleteValue`；回應 status=ok（不論 DeleteValue 回 true/false，皆 200 idempotent）。跑 2.1 測試至綠。
- [x] 2.6 確認 `handleDelete` 區分「query key absent」與「query key present but empty」——使用 `r.URL.Query()["value"]` 判斷 key 是否存在，而非只看 `Get("value") == ""`。

## 3. End-to-end coverage

- [x] 3.1 [P] 在 `cmd/shadowdns/main_ephemeral_test.go` 新增 e2e 測試：PUT 兩筆 value 到同一個 FQDN → `DELETE /v1/txt/<fqdn>?value=token-A` → DNS 查詢只回傳 `token-B`；再 `DELETE /v1/txt/<fqdn>` → DNS 回 NXDOMAIN。
- [x] 3.2 [P] 在 `cmd/shadowdns/main_ephemeral_test.go` 新增 e2e 測試：PUT 一筆合法 value、DELETE 帶合法 `?value=` 把最後一筆刪光 → DNS 回 NXDOMAIN（確認 FQDN key 真的從 store 清除）。

## 4. Documentation

- [x] 4.1 更新 `docs/ephemeral-api.md`：在 DELETE 章節新增「per-value delete」子節，給 `curl -X DELETE .../v1/txt/<fqdn>?value=<value>` 範例，註明 URL-encoding 原則與「無匹配回 200」語意。
- [x] 4.2 在 `docs/ephemeral-api.md` 新增「Value 長度限制」說明：PUT 與 DELETE 的 value 最長 255 bytes（RFC 1035 TXT character-string），超過回 400；同時在 PUT 範例旁邊備註。

## 5. Verification

- [x] 5.1 `make test` 全綠（含 race detector）。
- [x] 5.2 `make lint` 無新警告。
- [x] 5.3 `make smoke` dry-run 通過（確認 CLI/boot 未受影響）。修正 pre-existing 的 `scripts/smoke.sh`：改用 unified `--config` 取代已淘汰的 `--aliases` flag，並在 SMOKE_DIR 產生最小 `shadowdns.yaml`。
- [ ] 5.4 手動跑 curl 驗證：在本機 127.0.0.1 綁 API → PUT 兩筆 → `?value=` 精準刪一筆 → `dig` 驗 DNS 回應只剩另一筆。（使用者手動步驟；機械等價由 `TestEphemeralTxtApi_PerValueDeleteEndToEnd` e2e 測試覆蓋，已綠。）
