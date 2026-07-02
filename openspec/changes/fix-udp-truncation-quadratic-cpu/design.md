## Context

`truncateForUDP`（`internal/server/handler.go`）位於每個被截斷 UDP 回應的 per-query hot path。現行實作以「Pack → 若超出預算則丟棄尾端一筆 Answer RR → 重 Pack」的迴圈收斂到預算內。對含 N 筆 RR 的大型單一 owner RRset、且必須裁到 512-byte 後備預算時，需約 N 次 Pack，每次 O(訊息大小)，整體 O(N²)。這是可被廉價、來源可偽造的 UDP 查詢觸發的 CPU 放大（安全 LOW，GitHub issue #15）。

約束：miekg/dns 的 `dns.Msg.Pack()` 是唯一可信的 wire size 量測（含名稱壓縮）；不得自行估算 wire size。既有截斷語意（丟尾端、設 TC、OPT 永不丟、Pack error 保持 `m` 不變）已由 `dns-server` spec 與現有測試鎖定，必須逐條保留。

## Goals / Non-Goals

**Goals**
- 將裁切的 Pack 呼叫數由 O(N) 降為 O(log N)，總成本 O(N log N)，消除 CPU 放大。
- 逐條保留現行可觀察截斷語意（見 Implementation Contract）。

**Non-Goals**
- 不改變預算計算、觸發時機、丟棄順序（仍為尾端優先）。
- 不改動 `replyWithAnswer`、`udpMaxSize`、OPT 附加或 `m.Compress` 所有權。
- 不涉及 TCP/AXFR 路徑。

## Decisions

**Decision 1：以二分搜尋 Answer 前綴長度取代逐一丟棄。**
封裝 wire size 對 `m.Answer` 前綴長度 `k`（保留 `m.Answer[:k]`）是單調非遞減的：k 越大，序列化內容為 k 較小時的超集，名稱壓縮只會讓後加入的 RR 產生 ≥ 0 的增量，不會使總 size 減少。因此「size(k) ≤ budget」對 k 具單調性，可二分搜尋出最大可行 k。

- 為何不用逐一丟棄：即現行 O(N²) 缺陷本身。
- 為何不自行估算每筆 RR 的 wire size 來一次算出要丟幾筆：名稱壓縮使單筆 RR 的邊際 wire size 依前綴內容而變，估算不可靠且容易低估而破壞預算上限；二分搜尋每步都用真實 `Pack()` 量測，正確性與現行一致。

**Decision 2：二分搜尋只接受「已經 Pack 驗證 ≤ budget」的 k，因此無需事後退回。**
二分搜尋維持不變量 `lo` = 目前已知最大且封裝 size ≤ budget 的前綴長度（初值 0），`hi` = 目前已知封裝 size > budget 的最小前綴長度（初值 `len(m.Answer)`，因已知完整 Answer 超出 budget 才進入搜尋）。每一步取 `mid`，暫設 `m.Answer = m.Answer[:mid]` 後以真實 `m.Pack()` 量測：size ≤ budget 時 `lo = mid`，否則 `hi = mid`，直到 `lo+1 == hi`。收斂後 `lo` 即為答案，且該 `lo` 於搜尋過程中已被 Pack 且驗證 ≤ budget——不存在「選到過大的 k」的情形，因此不需要任何事後「退回較小 k」的步驟。

**Decision 3：以 header-only（`m.Answer` 清空，k=0）作為搜尋下界並處理過大 header。**
若連 0 筆 Answer（僅 header + question + authority + OPT）都超出預算（即收斂到 `lo == 0` 且 `k=0` 的封裝仍 > budget），則設 `TC=1` 並保留 header-only 結果（對應現行「Answer 清空仍過大」分支）。若完整 Answer 已可 Pack 進預算，則完全不進入二分搜尋、不截斷、不設 TC（維持現行行為）。

**Decision 4：任何一次 `Pack()` 回傳 error，整個 `m` 還原為進入時狀態並返回。**
搜尋前先記下進入時的 `m.Answer` slice 與 `m.Truncated` 值。過程中任一次 `Pack()` 回傳 error，即把 `m.Answer` 與 `m.Truncated` 都還原為進入時的值再返回，交由後續 `WriteMsg` 於正常路徑回報同一錯誤。由於 `m.Truncated` 只在搜尋收斂、選定最終 `k` 之後才設定，mid-search 的 Pack error 本就發生在任何 TC 變更之前；還原 `m.Truncated` 是為了涵蓋「收斂後最終驗證 Pack 才 error」的殘餘情形，確保與現行「Pack error 使 `m` 完全不變」語意逐字一致。

## Implementation Contract

- **函式**：`truncateForUDP(m *dns.Msg, budget int)`（`internal/server/handler.go`），簽章不變。
- **輸入**：已附加 OPT、已設定 `m.Compress` 的回應訊息；`budget` 為目標 wire size 上限（bytes）。
- **可觀察行為（必須全部成立）**：
  1. 函式返回後 `m.Pack()` 的 wire size ≤ `budget`（除非 Pack 本身 error）。
  2. 只要有任一 Answer RR 被丟棄、或 header-only 剩餘仍超出 `budget`，`m.Truncated`（TC）必須為 `true`。
  3. 完整 Answer 已符合 `budget` 時，不丟任何 RR 且不設 TC。
  4. 存活的 Answer RR 為原 `m.Answer` 的最長前綴 `m.Answer[:k]`，其 `k` 與逐一丟棄演算法對同一輸入收斂到的數量相同。
  5. 任一 `Pack()` 回傳 error 時，`m.Answer` 與 `m.Truncated` 都還原為進入時的值並返回；不得半途留下被裁切的 `m.Answer` 或被設起的 TC。
  6. OPT record（若存在）由呼叫端在進入前附加，本函式不得移除。
- **成本**：`Pack()` 呼叫數對「需丟棄的 RR 數」呈 O(log N)。
- **In scope**：`truncateForUDP` 函式本體，以及該函式自身的 doc comment（改述二分搜尋策略）。呼叫端 `replyWithAnswer` 的 doc comment 目前描述「drops trailing Answer RRs and sets TC=1」，在可觀察層面仍正確（仍丟尾端、仍設 TC），無需更動。
- **Out of scope**：`replyWithAnswer`/`udpMaxSize` 的程式碼與行為、預算計算、OPT 附加、壓縮設定、TCP/AXFR。

## Risks / Trade-offs

- [單調性假設若不成立（後加 RR 反而使封裝 size 變小），二分可能漏掉某個更大的可行 k，導致存活筆數比逐一丟棄少] → 這只會產生「較保守（多丟一點）」而非「破壞預算上限」的結果，因為二分接受的每個 k 都經真實 `Pack()` 驗證 ≤ budget。單調性對 DNS 名稱壓縮成立（後加 RR 對總 size 的邊際貢獻 ≥ 0），故實務上二分與逐一丟棄收斂到相同的 k；等值回歸測試（task 2.2）即用來鎖住這一點。
- [邊界：Answer 為空、單筆即超預算、完整 Answer 剛好等於預算] → 分別對應「不進搜尋直接檢查」「搜尋下界 k=0 並設 TC」「size == budget 視為符合、不設 TC」；回歸測試需涵蓋這三種邊界。
- [與既有 spec Example 衝突：原文寫「drop trailing Answer RRs one at a time and re-pack」] → 於 delta spec MODIFY 該需求，改以可觀察結果（最終 ≤ budget、正確存活筆數、TC=1）描述，並新增裁切成本有界的 scenario。

## Migration Plan

無資料遷移。純函式內部演算法替換，對外行為不變。部署後依 Perf-Guard 於 ns2 量測 baseline → 部署 → 重測確認無 QPS/p99 回歸。

## Open Questions

（無）
