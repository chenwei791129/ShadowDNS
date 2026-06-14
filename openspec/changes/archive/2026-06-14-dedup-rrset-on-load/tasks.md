## 1. AddRR 插入時去重（internal/zone/zone.go）

實作需求 "Index parsed records by owner and qtype"（去重儲存不變式）；落實 design 之 Decision 1（在 `AddRR` insertion-time 去重，非 post-parse pass）與 Decision 2（用 `miekg/dns.IsDuplicate` 作 RR identity）；對應 contract 之 interface / data shape（`AddRR` 加 bool 回傳）與 observable behavior。

- [x] 1.1 在 `internal/zone/zone_test.go` 先寫失敗測試：同一 owner+qtype 插入兩筆 byte-identical RR 後該 RRset 長度為 1；插入 RDATA 不同的多筆 RR 全數保留且維持插入順序；同名同型但 TTL 不同（300 後接 60）的兩筆塌縮為 1 筆且保留第一次的 TTL 300；驗證 `AddRR` 對重複插入回傳 false、對新記錄回傳 true（涵蓋 single 與 promoted sub-map 兩種儲存形態）
- [x] 1.2 實作需求 "Index parsed records by owner and qtype" 的去重不變式（Decision 1：deduplicate inside `AddRR` insertion-time，非 post-parse pass；Decision 2：use `miekg/dns.IsDuplicate` for RR identity）：修改 `AddRR` 簽名為回傳 `bool`，在 append 至既有 `s.rrs`（single 同型）與 `s.sub[qtype]`（promoted）兩個分支前，用 `github.com/miekg/dns.IsDuplicate` 比對該 RRset 既有記錄；命中重複則不插入並回傳 false，否則插入後回傳 true；新 owner 與型別升級（promote）分支一律回傳 true
- [x] 1.3 執行 `go test ./internal/zone/ -run AddRR -race -count=1` 確認 1.1 測試轉綠

## 2. ParseFile 彙總去重日誌（internal/zone/parser.go）

實作需求 "Log resource-record deduplication at load"；落實 design 之 Decision 3（log shape 對齊既有 backup-override drop summary：per-record DEBUG + 每 zone 一筆 WARN 彙總）與 Decision 4（在 `ParseFile` 彙總）；對應 contract 之 logging。

- [x] 2.1 在 `internal/zone/parser_test.go` 先寫失敗測試（用 `zaptest`/observer 攔截 log 與既有 parser 測試風格一致）：載入一個 inline 宣告某筆 CNAME、又經 `$INCLUDE` 片段宣告同一筆的 zone 檔後，該 owner 的 CNAME RRset 長度為 1；DEBUG 等級開啟時每筆重複各有一筆帶 zone/owner/type 的 DEBUG；DEBUG 關閉時無 per-record DEBUG 但彙總仍出現；有重複的 zone 恰好一筆 WARN 彙總且帶 zone origin、總數、依型別 histogram；零重複的 zone 不產生任何彙總
- [x] 2.2 實作需求 "Log resource-record deduplication at load"（Decision 3：log shape mirrors the existing backup-override drop summary；Decision 4：aggregate the summary in `ParseFile`）：在 `ParseFile` 的 `zp.Next()` 迴圈中，將 `z.AddRR(rr)` 改為依回傳值累計：重複時對一個 by-type histogram 計數，並在 `logger.Core().Enabled(zapcore.DebugLevel)` 守衛下印出 per-record DEBUG（欄位 zone/owner/type）；迴圈結束後，若 histogram 非空則印一筆 WARN 彙總（欄位 zone origin、總 count、histogram）
- [x] 2.3 重用 `internal/zone/classify.go` 既有的 `dropHistogram`（其 `MarshalLogObject` 提供字母序穩定輸出）作為彙總 histogram 的型別；若需共用則保持其為 package 層級型別，避免複製 marshaler 邏輯
- [x] 2.4 執行 `go test ./internal/zone/ -race -count=1` 確認 2.1 測試轉綠且既有 parser 測試不回歸

## 3. 驗證與文件

驗證 contract 之 acceptance criteria，並遵守 design 之 Goals / Non-Goals 與 in scope / out of scope 邊界（不動 resolution / wildcard / CNAME-following / alias-rewrite / AXFR，不改查詢 hot path，不改上游 master-data 產生流程）。

- [x] 3.1 [P] 執行 `make test` 與 `make lint`，全綠
- [x] 3.2 [P] 檢視 MkDocs manual：在 zone 載入 / operations 相關頁（英文版與 `.zh.md` 同步）補一句「載入時會塌縮 RRset 內的 byte-identical 重複記錄」，並以 `make docs-build`（strict）驗證；若判定為純內部行為對齊無使用者可觀察新增面則明確記錄此結論並略過
- [x] 3.3 請使用者在測試 nameserver 上對重現名稱（一個 backup alias 名稱，其 CNAME 同時 inline 與經 `$INCLUDE` 宣告；具體生產名稱見 `.local/`）以 dig 交叉比對 ShadowDNS 與 BIND，確認第一個 CNAME 不再重複、回應記錄集與 BIND 一致
