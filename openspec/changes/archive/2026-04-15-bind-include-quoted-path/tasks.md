## 1. 建立測試 fixture — Decision 4: Fixture domain naming for testdata

- [x] 1.1 依照 Decision 4: Fixture domain naming for testdata 建立 include 片段 `testdata/integration/master/cnames/example.com_cname`，內容為幾條 CNAME 記錄（owner 屬於 `example.com.`，目標使用 `example.com.` 或 `example.net.` 等 RFC 6761 保留域名）
- [x] 1.2 建立主 zone 檔 `testdata/integration/master/example.com_include.fwd`，包含基本 SOA、NS 記錄，並在檔案中段同時放入 `$include "..."`（雙引號）與 `$include ...`（無引號）兩種寫法，指向 1.1 的片段以測試兩種語法共存
- [x] 1.3 在 `testdata/integration/master.zones` 或等效的 zone 宣告檔中註冊新 fixture，使整合測試能載入 `example.com_include.fwd`

## 2. 實作 BIND 相容前處理 — Decision 1: Pre-processing wrapper instead of fork

- [x] 2.1 依 Decision 1: Pre-processing wrapper instead of fork，在 `internal/zone/parser.go` 新增一個具名的前處理函式（例如 `rewriteBindIncludes(io.Reader) io.Reader`），採用 Decision 2: Line-anchored token-level matching，逐行讀取並僅在去除前導空白後以 `$INCLUDE` 或 `$include` 開頭且後接 whitespace 的行才進入處理邏輯
- [x] 2.2 依 Decision 3: Preserve line numbers，在該函式中將匹配到的 `$include`-line 中「包裹檔案路徑」的兩個配對雙引號替換為空白字元（而非刪除），其他位置原樣保留；未找到配對結束 `"` 時整行保持原樣
- [x] 2.3 將 `ParseFile` 中的 `f` reader 包裝為前處理過的 reader 再傳給 `dns.NewZoneParser`；確認 `SetIncludeAllowed(true)` 與 origin 邏輯維持不變

## 3. 單元測試 — Accept BIND-compatible `$INCLUDE` directive with quoted file path

- [x] 3.1 在 `internal/zone/parser_test.go` 新增測試（Accept BIND-compatible `$INCLUDE` directive with quoted file path，小寫情境）：`$include "path"` 能成功載入被 include 的記錄
- [x] 3.2 [P] 新增測試（Accept BIND-compatible `$INCLUDE` directive with quoted file path，大寫情境）：`$INCLUDE "path"` 產出與小寫版本完全相同的 records 集合
- [x] 3.3 [P] 新增測試：`$include path`（無引號，原支援語法）仍然成功，確保無退化
- [x] 3.4 [P] 新增測試：zone 檔中的 TXT 記錄 `@ IN TXT "v=spf1 -all"` 不受前處理影響，解析後 txt value 仍為 `v=spf1 -all`
- [x] 3.5 [P] 新增測試：`$include "path" ; trailing comment` 同一行尾註解不影響載入
- [x] 3.6 [P] 新增測試：未配對引號（只有開頭 `"` 沒有結尾 `"`）保持原樣，讓 miekg 依原行號回報錯誤
- [x] 3.7 [P] 新增測試驗證 Decision 3: Preserve line numbers 的正確性：在多個 `$include "..."` 之後插入一條明顯語法錯誤的記錄，驗證 parser 回傳的 error 行號與原檔行號一致

## 4. 整合測試

- [x] 4.1 在 `test/integration/` 加入端到端測試，使用 1.1–1.3 的 fixture 啟動 ShadowDNS 並透過 DNS 查詢確認被 include 進來的 CNAME 記錄可查得
- [x] 4.2 執行 `make test`、`make lint`、`make smoke` 確認既有測試全數通過、無 lint 退化
- [x] 4.3 在 backup-classified zone 中也驗證 quoted `$include`：新增 fragment `testdata/integration/master/backup.example_overrides`（含 TXT 與 SRV override，覆蓋兩種 overridable 類型），讓 `backup.example_view-other.fwd` 透過 `$include "..."` 載入；新增整合測試 `TestBackup_QuotedInclude_TXT`（驗證 inline + include TXT 同時生效、root SPF 仍被抑制）與 `TestBackup_QuotedInclude_SRV`（驗證 backup 命名空間能查得 include 進來的 SRV）

## 5. 文件

- [x] 5.1 [P] 更新 `README.md` 的 BIND 相容性章節，新增一條：支援 `$INCLUDE` / `$include` 的雙引號檔名語法（BIND 9 慣用寫法）
