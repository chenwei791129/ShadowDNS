## 1. Zone 層 wildcard lookup

- [x] [P] 1.1 在 `internal/zone/zone.go` 新增 `LookupWildcard(qname string, qtype uint16) ([]dns.RR, bool)` 方法（Wildcard lookup 演算法放在 zone.LookupWildcard 方法中），實作「Match wildcard records per RFC 4592 when exact lookup fails」的 closest encloser 演算法：從 qname 逐級剝離 leftmost label，每級嘗試 `*.<parent>` 查 `z.Records`，若命中則按 qtype 過濾後回傳（`bool` = true），若 parent 本身存在 `z.Records`（Empty non-terminal 以 Records map 存在性判斷，即 ENT blocker）則停止回傳空（`bool` = false），直到 parent 等於 zone origin
- [x] [P] 1.2 在 `internal/zone/zone_test.go` 為 `LookupWildcard` 新增單元測試：(a) 單層 wildcard 匹配 `foo.example.com.` → 命中 `*.example.com.`，(b) 多層 subdomain `foo.bar.example.com.` → 命中 `*.example.com.`（bar 不存在時），(c) ENT blocking：`sub.example.com.` 存在時查 `other.sub.example.com.` → 不命中，(d) 更具體 wildcard 優先：`*.sub.example.com.` 優先於 `*.example.com.`，(e) 無 wildcard → 回傳空，(f) wildcard match 但 qtype 不匹配 → 回傳空（NODATA case）

## 2. Zone parser wildcard 驗證（Parse and store wildcard owner names）

- [x] [P] 2.1 驗證「Parse and store wildcard owner names」：在 `internal/zone/parser_test.go` 新增測試確認 parser 正確處理 wildcard owner name：(a) `* A 1.2.3.4` 解析後存為 `*.example.com.`，(b) `*.sub CNAME target.` 解析後存為 `*.sub.example.com.`，(c) 同一 wildcard owner 下多筆紀錄正確 append。若現有 parser 已通過則僅作為 regression guard

## 3. Handler 層 wildcard 合成

- [x] 3.1 修改 `internal/server/handler.go` 的 `handleRootQuery`：當 `rootZone.Lookup(qname, qtype)` 回空（且 CNAME synthesis 也未命中）時，呼叫 `rootZone.LookupWildcard(qname, qtype)`，若命中則實作 Wildcard 合成的 owner name 由 handler 層改寫為原始 qname，以 `replyWithAnswer` 送出
- [x] 3.2 修改 `internal/server/handler.go` 的 `handleBackupQuery` 及 `internal/alias/override.go` 的 `Resolve`：在 root zone exact lookup 回空後增加 `rootZone.LookupWildcard(rootQName, qtype)` 的 fallback，命中後透過 `RewriteRR` 轉換 owner name 至 backup namespace。Backup zone wildcard 透過 alias.Resolve 擴充
- [x] [P] 3.3 在 `internal/server/server_test.go` 新增 handler 層 wildcard 合成測試：(a) A query 命中 wildcard → NOERROR + answer 的 owner 為 qname 非 `*`，(b) ENT blocking → NXDOMAIN，(c) 精確紀錄優先於 wildcard，(d) backup zone wildcard → owner 為 backup namespace

## 4. 整合測試與驗證

- [x] 4.1 在 testdata zone file 中加入 wildcard 紀錄（`*.example.com. A`、`*.sub.example.com. CNAME`），在 `test/integration/` 新增 wildcard 整合測試：驗證 (a) wildcard A 查詢正確回傳，(b) wildcard CNAME + CNAME synthesis 正確運作，(c) ENT blocking 正確，(d) backup zone wildcard 正確
- [x] 4.2 執行 `make test` 與 `make lint` 確認所有測試通過且無 lint 錯誤
