## 1. 以二分搜尋重寫 truncateForUDP

- [x] 1.1 在 `internal/server/handler.go` 的 `truncateForUDP` 中，進入時先記下 `origAnswer := m.Answer` 與 `origTC := m.Truncated`；以完整 `m.Answer` 呼叫 `m.Pack()`；若回傳 error 則不改動 `m`（`m.Answer`、`m.Truncated` 維持進入值）直接返回；若 wire size ≤ budget 則直接返回（不設 TC、不丟任何 RR，`m.Truncated` 保持進入值不被設起）。
- [x] 1.2 當完整 Answer 超出 budget 時，對前綴長度做二分搜尋以求最大可行 `k`：維持 `lo = 0`（已知或視為可行的下界）、`hi = len(m.Answer)`（已知超出 budget 的上界），每步取 `mid`，暫設 `m.Answer = origAnswer[:mid]` 後以真實 `m.Pack()` 量測，size ≤ budget 則 `lo = mid`，否則 `hi = mid`，直到 `lo+1 == hi`；收斂後 `k = lo`。搜尋過程中任一次 `Pack()` 回傳 error，即將 `m.Answer` 還原為 `origAnswer`、`m.Truncated` 還原為 `origTC` 後返回（`m` 完全不變）。
- [x] 1.3 二分收斂後設 `m.Answer = origAnswer[:k]`。由於每個被 `lo` 接受的 `k` 都已在搜尋過程中以 `Pack()` 驗證 ≤ budget，收斂結果不需要再做任何「退回較小 k」的事後修正。當有 RR 被丟棄（`k < len(origAnswer)`）、或 `k == 0` 且 header-only 封裝仍 > budget 時，設定 `m.Truncated = true`。
- [x] 1.4 更新 `truncateForUDP` 自身的 doc comment，描述二分搜尋策略與「丟棄尾端、設 TC、Pack error 使 `m` 完全不變、OPT 永不丟」等保留語意；移除舊 comment 中「drop the trailing Answer RR ... re-pack」的逐一丟棄描述。呼叫端 `replyWithAnswer` 的 doc comment 在可觀察層面仍正確（仍丟尾端 Answer RR、仍設 TC），不需更動。

## 2. 回歸測試（僅測本專案自有的 truncateForUDP）

- [x] 2.1 在 `internal/server/handler_test.go` 新增測試：建構一個大型單一 owner RRset（數百至上千筆 A 或 TXT RR）的 `dns.Msg`，以 512-byte budget 呼叫 `truncateForUDP`，斷言 (a) 最終 `m.Pack()` wire size ≤ 512、(b) `m.Truncated == true`、(c) 存活 Answer 筆數 > 0。
- [x] 2.2 於同測試以獨立的「逐一丟棄」參考實作（測試檔內的 local helper，重現舊演算法）對同一輸入計算存活筆數，斷言二分結果的存活筆數與參考實作逐筆一致。
- [x] 2.3 新增邊界測試：完整 Answer 剛好 ≤ budget 時不丟 RR 且不設 TC；單筆 RR 即超 budget 時 Answer 清空、設 TC；空 Answer 輸入不 panic。
- [x] 2.4 以計數包裝斷言 `Pack()` 呼叫數有明確對數上界：對 N 筆需裁切的 Answer，總 `Pack()` 呼叫數 ≤ `2*ceil(log2(N)) + 4`（涵蓋 1 次完整 Answer 探測 + 二分搜尋的 O(log N) 步 + 小常數餘裕）。以 N=1000 為例斷言 Pack 呼叫數 ≤ 24，確保實作即使退化為線性（如 ~900 次）也會失敗，真正鎖住 O(log N)。

## 3. 驗證與 Perf-Guard

- [x] 3.1 執行 `make test`（race detector）與 `make lint`，確認下列既有截斷測試無回歸且無 lint 問題：`internal/server/handler_test.go`、`test/integration/compression_budget_test.go`（EDNS 4096 下不截斷、TC=0、全數 RR 保留）、`test/integration/stress_shared_bucket_test.go`（截斷斷點、存活筆數、TC flag、預算符合）；並確認行為符合 `dns-server` spec 需求「Listen for DNS queries on UDP and TCP port 53」底下的 UDP 截斷 scenarios（含本變更新增的「truncation cost is bounded logarithmically」scenario）。
- [ ] 3.2 請使用者確認：本變更為 hot-path，實作與 review chain 完成後需依 Perf-Guard 在 ns2 跑 baseline → 部署 → 重測，確認 QPS 未下降 > 5% 且 p99 未上升 > 15%。
