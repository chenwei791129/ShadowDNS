## 1. DNS name compression

- [x] 1.1 [P] 在 `internal/server/handler_test.go` 新增失敗測試 `TestReplyWithAnswer_UsesNameCompression`：驗證「Successful answer responses SHALL use DNS name compression」requirement。用兩筆同 owner name 的 TXT answer 呼叫 `replyWithAnswer`，捕獲 `w.WriteMsg` 的 packed bytes，斷言第二筆 owner name 出現的位置為 0xC0 開頭的 2-byte pointer（而非完整 label 序列）
- [x] 1.2 在 `internal/server/handler.go` 的 `replyWithAnswer` 裡、`m.SetReply(req)` 之後加入 `m.Compress = true`，讓 1.1 測試通過（Decision: Enable DNS name compression unconditionally in `replyWithAnswer`）
- [x] 1.3 [P] 新增 benchmark `BenchmarkReplyWithAnswer_N48_Compressed` 於 `internal/server/handler_test.go`，記錄 n=48 shared owner name TXT 的 ns/op 與 packed bytes，作為未來回歸保護基準

## 2. Strict UDP truncation by Pack() wire size

- [x] 2.1 [P] 在 `internal/server/handler_test.go` 新增失敗測試 `TestReplyWithAnswer_UDPRespectsEDNS0Budget`：設 48 筆 TXT、client OPT.UDPSize=4096，斷言 `Pack()` 後 wire size ≤ 4096（覆蓋「UDP response size SHALL NOT exceed the advertised EDNS0 buffer」Scenario）
- [x] 2.2 [P] 新增 `TestReplyWithAnswer_UDPNoEDNSFallsBackTo512`：client 無 OPT record、放入足夠多 TXT 使 wire size > 512，斷言 packed ≤ 512 bytes 且 TC=1（覆蓋「UDP response without EDNS0 falls back to 512-byte budget」Scenario）
- [x] 2.3 新增 helper 函式 `truncateForUDP(m *dns.Msg, budget int)`（於 `internal/server/handler.go`）：先 `Pack()`，若 `len(packed) > budget` 則迴圈丟 `m.Answer` 末筆 + 設 TC=1 + 重 Pack 驗證，直到 packed ≤ budget 或 `Answer` 為空（Decision: Strict UDP truncation based on `Pack()` wire size）
- [x] 2.4 `replyWithAnswer` 改呼叫新 `truncateForUDP` 取代舊 `m.Truncate(maxSize)`；確保此呼叫發生在 `m.Compress = true` 之後，讓 budget check 檢的是壓縮後的 wire size（Decision: Compression 開啟順序在 Truncate 之前）

## 3. Spec 與回歸

- [x] 3.1 依 Decision: 修改 dns-server spec 而非建立新 capability — 本 change 已以 delta 形式修訂「Listen for DNS queries on UDP and TCP port 53」requirement 並新增 name compression requirement，此 task 確認 archive 後 `openspec/specs/dns-server/spec.md` 內容正確合併且無 placeholder 殘留
- [x] 3.2 [P] 在 `test/integration/` 新增或補強整合測試，對實際 bind 的 UDP port 發送 EDNS0 4096 query，斷言 48 筆同 owner name TXT 的回應完整且 packed ≤ 4096（對應外部 cert-tool `stress_shared_bucket_test.go` 找到的回歸點）
- [x] 3.3 執行 `go test -race -count=1 ./...` 與 `golangci-lint run`，確認全綠；執行 benchmark 確認 n=48 壓縮 ON 的 Pack ns/op 優於或持平原 ns/op
