## 1. 回歸測試（TDD，先寫）

- [x] 1.1 新增 `internal/transfer/axfr_soa_test.go`：用 TCP-style 的假 `dns.ResponseWriter` 分別驅動 `HandleAXFR`（傳入 `z.SOA == nil` 的 zone）與 `HandleAliasAXFR`（backing root zone 的 SOA 為 nil），斷言兩者皆回 `RCODE=REFUSED` 且不 panic、不使測試 process 崩潰。覆蓋需求 "AXFR refuses a zone without a usable SOA instead of crashing"。驗證：`go test -race ./internal/transfer/ -run SOA` 在修補前為紅燈（panic/crash）。

## 2. 修補 transfer handler（守衛 nil SOA）

- [x] 2.1 在 `internal/transfer/axfr.go`：`HandleAXFR` 於 collectNonSOA/streamAXFR 之前加入 `z.SOA == nil → replyRefused(w, req); return` 守衛；`HandleAliasAXFR` 於呼叫 `alias.BackupSOA` 之前加入 `rootZone.SOA == nil → replyRefused(w, req); return` 守衛。觀察結果：對 SOA-less zone 的 AXFR/alias-AXFR 回 REFUSED 而非崩潰。此覆蓋需求 "AXFR refuses a zone without a usable SOA instead of crashing"。驗證：步驟 1.1 測試轉綠（`go test -race ./internal/transfer/`）。

## 3. 載入期拒絕無 SOA 的 root zone

- [x] 3.1 在 `internal/server/build.go` 的 `BuildState`：當某 zone 經 `zone.Classify` 判為 `RoleRoot` 但 `z.SOA == nil` 時，視為 load error 回傳（錯誤訊息需含該 zone 的 origin），使無效 zone 不會成為可服務狀態；沿用既有 fail-soft reload 模型（startup 中止、reload 保留舊 state）。此覆蓋需求 "Reject a root zone with no apex SOA at load"。驗證：新增單元測試以一個無 SOA 的 root zone 驅動 `BuildState`，斷言回傳 error 且訊息含 origin；reload 路徑保留舊 state。

## 4. 驗證與回歸

- [x] 4.1 確認既有 AXFR / alias-AXFR 成功路徑、reload 與 state build 行為不變。驗證：`go test -race ./internal/transfer/ ./internal/server/ ./test/integration/ -run 'AXFR|Axfr|Transfer|Reload|Build|SOA'` 全數通過。
- [x] 4.2 整體品質閘通過。驗證：`make lint` 與 `make test` 皆 exit 0。
