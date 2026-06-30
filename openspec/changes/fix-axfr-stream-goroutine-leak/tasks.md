## 1. 回歸測試（TDD，先寫）

- [x] 1.1 為 `streamAXFR` 新增回歸測試於 `internal/transfer/axfr_stream_test.go`：用一個假的 `dns.ResponseWriter`（第 2 次 `WriteMsg` 回傳 error，模擬 peer 中途中斷），驅動 `streamAXFR`，斷言呼叫在短逾時（例如 2s）內返回、不 hang，且 producer goroutine 不外洩。此測試覆蓋需求 "AXFR streaming survives a mid-stream peer abort without leaking"。驗證：`go test -race ./internal/transfer/ -run StreamAXFR` 在修補前為紅燈（逾時失敗）。

## 2. 修補 streamAXFR（僅本專案程式碼）

- [x] 2.1 在 `internal/transfer/axfr.go` 將 `streamAXFR` 的 envelope channel 改為緩衝（容量 3 = SOA + records + SOA），並以一個 buffered result channel 取得 `tr.Out` 的回傳 error 後 join；目的是讓 `tr.Out` 在寫入錯誤時提早返回後，producer 的後續 send 仍能完成而不 block。觀察結果：peer 中途中斷時 `streamAXFR` 立即返回且不洩漏。驗證：步驟 1.1 的測試轉綠（`go test -race ./internal/transfer/`）。
- [x] 2.2 在 `streamAXFR` 衍生的 transfer goroutine 內加入 `recover()`，使打包 envelope 時的 panic 被攔截：單一 transfer 失敗但伺服器 process 繼續服務其他請求。此覆蓋 spec scenario "A panic while packing an envelope does not crash the process"。驗證：新增子測試，注入一個在打包時會 panic 的情境，斷言呼叫返回且未使測試 process 崩潰。

## 3. 驗證與回歸

- [x] 3.1 確認既有 AXFR 與 alias-AXFR 成功路徑行為不變（SOA → records → SOA 順序、AXFR over UDP 回 REFUSED、allow-transfer ACL gating）。驗證：`go test -race ./internal/transfer/ ./test/integration/ -run 'AXFR|Axfr|Transfer'` 全數通過。
- [x] 3.2 整體品質閘通過。驗證：`make lint` 與 `make test` 皆 exit 0。
