## 1. Package scaffolding

- [x] 1.1 建立 `internal/prunebackup/` package，放置 `doc.go`（package-level comment）與空的 `prunebackup.go` 匯出入口，確認 `go test ./internal/prunebackup/...` 可跑。

## 2. Line lexer（支撐 decision「RR → 行號映射透過自寫 line lexer，而非 miekg/dns 內部行號」）

- [x] 2.1 於 `internal/prunebackup/lexer.go` 實作 raw-byte line 切分器，輸出 `{kind, rawText, startLine, endLine}`；kind 含 `directive` / `blankOrComment` / `rrSingle` / `rrMulti`，並追蹤 active `$TTL` 與 `$ORIGIN`。
- [x] 2.2 為 `rrMulti` 狀態正確吃到對應 `)`（參照 spec Scenario「multi-line RR enclosed in parentheses is treated as a single range」）。
- [x] 2.3 把每個 `rrSingle` / `rrMulti` 的 raw text 套上當前 origin / ttl context，呼叫 `miekg/dns.NewRR` 得到可比對的 RR；parse 失敗即整體 abort 並回傳含 file:line 的 error（支援 spec Requirement「Exit code semantics」中 parse 失敗條件）。
- [x] 2.4 於 `internal/prunebackup/lexer_test.go` 以 `testdata/integration/master/backup.example_view-th.fwd` 等現有檔為 golden input 覆蓋 lexer 行為。

## 3. Include resolution（支撐 spec Requirement「Merge main file and $include files before diffing」與 decision「`$include` 檔遞迴處理，directive 本身保留」）

- [x] 3.1 於 `internal/prunebackup/include.go` 實作 `$include` path 解析：相對路徑以 named.conf `directory` option 為 base（對齊 `internal/zone/parser.go` 的 `rewriteBindIncludes` 現行行為）。
- [x] 3.2 遞迴展開 include，組成 merged RR list，每筆 RR 夾帶 `(sourceFile, startLine, endLine)` 註記；偵測 include 迴圈並 abort。
- [x] 3.3 於 `internal/prunebackup/include_test.go` 覆蓋巢狀 include、相對路徑、含 quoted `$include "..."` 寫法三種組合。

## 4. RRSet diff（支撐 spec Requirement「Determine deletion candidates using RRSet-level rules」、decision「RRSet 相等比對以 "sorted rdata set" 為準」、decision「SOA 與 zone apex 的 NS 永遠豁免」）

- [x] 4.1 於 `internal/prunebackup/diff.go` 建立 `RRSetIndex`：`map[struct{owner string; rtype uint16}][]dns.RR`，owner 以 `dnsutil.Canonicalize` 標準化；為 backup 與 root 各建一份。
- [x] 4.2 實作 `rrsetEqual(a, b []dns.RR) bool`：取 canonical rdata string（`rr.String()` 截掉 header 的 TTL 欄位）→ sort → `slices.Equal`；class 固定 IN。TTL 差異不視為差異。
- [x] 4.3 實作 `classify(backupRRSet, rootRRSet, owner, rtype, origin) Decision`：SOA 保留、`owner == origin && rtype == NS` 保留、其餘非 overridable 刪、overridable 僅在 `rrsetEqual` 為真時刪。
- [x] 4.4 於 `internal/prunebackup/diff_test.go` 以 table-driven 覆蓋 spec 中 RRSet comparison example table 的每一列，加上 apex-NS 豁免、非 apex NS 刪除、SOA 保留三組情境。

## 5. Line-based writer（支撐 spec Requirement「Delete records by line range while preserving formatting」與 decision「採用 line-based 刪除而非 parse-serialize」，同時處理 spec Requirement「$GENERATE directives are opaque」）

- [x] 5.1 於 `internal/prunebackup/rewrite.go` 實作 `pruneFile(originalLines []string, deleteRanges []LineRange) []string`：丟棄落在刪除範圍內的行；其餘原樣保留。
- [x] 5.2 同 pass 丟棄 blank 與 stand-alone 註解行；確保 retained RR 行的行尾 `;` 註解被保留。
- [x] 5.3 對 `$GENERATE` 所在行 emit INFO log「opaque directive retained」，並確保該行不會被 delete（即不會出現在 deleteRanges 中）。
- [x] 5.4 於 `internal/prunebackup/rewrite_test.go` 覆蓋：relative owner 保留、trailing comment 保留、`$include` 主檔保留、多行 SOA 連續範圍保留、`$GENERATE` 保留與 INFO log 發送。

## 6. Apply writer（支撐 spec Requirement「Atomic per-file write with .bak backup on --apply」與 decision「預設 dry-run，`--apply` 才寫檔；寫前自動 `.bak`」）

- [x] 6.1 於 `internal/prunebackup/apply.go` 實作 `applyFile(path string, newContent []byte) error`：rename `path → path.bak`（覆蓋既有 `.bak`）、`WriteFile` tmp → `f.Sync()` → `os.Rename(tmp, path)`。
- [x] 6.2 偵測既有 `.bak` 覆蓋情境並發 INFO log（含 file path）。
- [x] 6.3 對「本次無刪除的檔」完全不觸發 apply 路徑：不 rename、不產生 `.bak`。
- [x] 6.4 Fail-stop：任一檔 apply 失敗即回傳 error 終止後續檔處理；保留先前已寫成功的檔與對應 `.bak`。
- [x] 6.5 於 `internal/prunebackup/apply_test.go` 用 `t.TempDir` 覆蓋成功、`.bak` 覆蓋、zero-deletion skip、mid-batch fail 四組情境。

## 7. Cobra sub-command（支撐 spec Requirement「Provide a prune-backup sub-command」、「Load config and named.conf identically to the server」、「Iterate backup zones per view」、「Default to dry-run; require --apply to modify files」、「Exit code semantics」、decision「多 view 處理策略：各 view 獨立」、decision「Exit code 語意」）

- [x] 7.1 新增 `cmd/shadowdns/prune_backup.go`，定義 `newPruneBackupCmd()`：註冊 `--named-conf`（required）、`--config`（required）、`--apply`（bool, 預設 false）、`--no-color`。
- [x] 7.2 RunE 內以 `config.LoadNamedConf` + `shadowdnscfg.Load` 載入配置；任一失敗即回傳 error（cobra SilenceUsage 保持一致）。
- [x] 7.3 建 `(view, zoneOrigin)` 配對清單：以 alias map 過濾出 backup zones，同 view 內查找對應 root zone file；缺 root 時 emit WARN 並跳過該 pair（不 abort 其他 pair）。
- [x] 7.4 對每個 pair 串接 lexer → include resolver → diff → line-based writer，得到 per-file deletion plan 與 pruned content。
- [x] 7.5 Dry-run 輸出 formatter：逐筆印 `file:startLine-endLine owner TYPE rdata`；若無候選則印一行「no redundant records found」。
- [x] 7.6 `--apply` 分派：對每個有刪除的檔呼叫 `applyFile`；實作 spec Requirement「Exit code semantics」中的 exit-code 對應（dry-run 永遠 0、parse/load/apply 失敗非 0）。
- [x] 7.7 於 `cmd/shadowdns/main.go` 的 `newRootCmd()` 內 `cmd.AddCommand(newPruneBackupCmd())`，與 `newReloadCmd()` 同層註冊。
- [x] 7.8 於 `cmd/shadowdns/prune_backup_test.go` 測 flag parsing、required flag enforcement、help 輸出包含三個 flag、sub-command 不會 bind 任何 listener。

## 8. Integration、docs、make targets

- [x] 8.1 於 `test/integration/` 新增 `prune_backup_test.go`：使用 `testdata/integration/master/` 的 backup/root 配對，測 dry-run stdout 與 `--apply` 後磁碟狀態（原檔、`.bak`、include target 檔的三方一致性）。
- [x] 8.2 跑 `make completions` 更新 `bin/shadowdns.{bash,zsh,fish}`，驗證 `prune-backup` 出現在補全輸出中。
- [x] 8.3 更新 `README.md`：在 Usage 區塊追加 `shadowdns prune-backup` 小節，附 dry-run 範例指令。
- [x] 8.4 跑 `make lint`、`make test`、`make smoke` 皆綠；確認現有測試無回歸。
