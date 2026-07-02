## Problem

`truncateForUDP`（`internal/server/handler.go`）在 UDP 回應超出大小預算時，會反覆呼叫 `dns.Msg.Pack()`，每次僅丟棄 **一筆** 尾端 Answer RR 後重新 Pack。對於一個含 N 筆 RR、且必須裁切以符合小預算（查詢未帶 EDNS OPT 時為 512 bytes）的答案，這會執行約 N 次 `Pack()`，每次成本 O(訊息大小) → 每個查詢 O(N²)。

由於 `truncateForUDP` 位於每個被截斷 UDP 回應都會經過的 per-query hot path，攻擊者只需對一個持有大型單一 owner RRset 的名稱送出一筆廉價、可偽造來源的 UDP 查詢，即可迫使伺服器付出不成比例的 CPU（放大攻擊）。此迴圈會終止（Answer 清空或單筆過大 RR 因 Pack error 退出），因此是 CPU 放大，而非 hang。

## Root Cause

裁切演算法是線性逐一丟棄：每丟一筆 RR 就重新序列化整個訊息。當需要丟棄的 RR 數量與 RRset 大小同階時，總成本退化為 O(N²)。

## Proposed Solution

以 **二分搜尋** 取代逐一丟棄迴圈：對 `m.Answer` 的前綴長度做二分搜尋，找出仍能 Pack 進預算的最大前綴。封裝後的 wire size 對 Answer 前綴長度是單調遞增的（名稱壓縮不破壞單調性：較長前綴的封裝結果永遠 ≥ 較短前綴），因此二分搜尋成立，將 `Pack()` 呼叫數從 O(N) 降為 O(log N)、總成本降為 O(N log N)。

必須完整保留既有可觀察語意：

- 任何 Answer RR 被丟棄、或清空 Answer 後的 header-only 剩餘仍超出預算時，都必須設定 `TC=1`。
- 最終封裝的 wire size 必須 ≤ 預算。
- 存活的 Answer RR 數量必須與原本逐一丟棄迴圈得出的結果相同（同一前綴）。
- Pack error 時維持 `m` 不變並返回（後續 `WriteMsg` 會透過正常路徑回報同一錯誤）。
- OPT record 在截斷前就已附加、計入預算且永不被丟棄。
- `m.Compress` 的所有權仍屬呼叫端；本函式沿用 `m` 帶入的設定。

## Non-Goals

- 不改動 `replyWithAnswer`、`udpMaxSize` 或 OPT 附加邏輯。
- 不改變截斷的觸發時機或預算計算（EDNS advertised size vs 512-byte 後備）。
- 不新增 AXFR/TCP 相關行為；本變更僅涉及 UDP 回應裁切。
- 不改變被丟棄 RR 的選取策略（仍為尾端優先）。

## Success Criteria

- 對一個大型單一 owner RRset、以 512-byte 預算裁切時，最終封裝 wire size ≤ 512 bytes 且 `TC=1`。
- 裁切後存活的 Answer RR 數量，與既有逐一丟棄演算法對同一輸入的結果逐筆一致。
- `Pack()` 呼叫次數相對於被丟棄的 RR 數呈對數級（可用計數包裝或有界迭代斷言），不再是線性。
- 既有 `internal/server/handler_test.go` 與 `test/integration/compression_budget_test.go` 全數通過，無回歸。
- Perf-Guard（hot-path 變更必跑）：ns2 baseline → 部署 → 重測，QPS 未下降 > 5% 且 p99 未上升 > 15%。

## Impact

- Affected specs: dns-server（MODIFY 既有 UDP 回應大小裁切需求：以可觀察結果描述取代「逐一丟棄」的實作性描述，並加入裁切成本有界的保證）
- Affected code:
  - Modified: internal/server/handler.go
  - New: (none)
  - Removed: (none)
- Affected tests:
  - Modified: internal/server/handler_test.go（新增大型 RRset 二分裁切的回歸測試）
  - Regression gates (must stay green, not modified): test/integration/compression_budget_test.go、test/integration/stress_shared_bucket_test.go（既有 UDP 截斷斷點/存活筆數/TC/預算斷言，須確認二分搜尋未改變其結果）
