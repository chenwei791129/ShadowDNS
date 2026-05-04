## Why

Backup zone file 在 runtime（`internal/zone/classify.go` 的 `filterBackupRecords`）只會保留 TXT/MX/SRV；其他類型被 drop 並 warn。可是 operator 實際維護的 backup zone file 常常是從 root zone 複製過來再改，留下大量 runtime 根本不會 serve 的 A/AAAA/CNAME/NS 等記錄，造成閱讀噪訊與維護負擔。同理，TXT/MX/SRV 當 backup 的 RRSet 跟 root 完全一樣時，背後 runtime 行為等同於「讓 root 的 answer rewrite 過來」，留著也是多餘。

目前沒有工具協助 operator 清理這類冗餘；手動比對 root 與 backup 的 zone file 容易漏看或誤刪。需要一個離線 CLI 工具幫 operator 列出並刪除冗餘記錄，同時保留 backup 的 overlay 意圖。

## What Changes

- 新增 sub-command `shadowdns prune-backup`（cobra subcommand，與現有 `reload` 同層）。
- 讀 `--named-conf` 與 `--config`，以每個 view 下的 `(backup zone, root zone)` 對為處理單位。
- 對每個 backup zone（含遞迴展開的 `$include` 檔）計算冗餘 RR 集合，規則：
  - **非 overridable 類型**（即非 TXT/MX/SRV）：整組 RRSet 刪除。**例外豁免**：SOA 與 zone apex 的 NS 永遠保留，以維持 zone file 的 RFC 1035 合法性。
  - **Overridable 類型**（TXT/MX/SRV）：當 backup 的 RRSet 作為集合完全等於 root 對應 `(owner, type)` 的 RRSet 時，整組刪除；任何差異（多一筆、少一筆、rdata 不同）全組保留。
- 對每個實體檔採 **line-based 刪除**：保留 `$TTL` / `$ORIGIN` / `$INCLUDE` / `$GENERATE` 等 directive、保留 relative owner name 寫法、保留行尾註解；清除空行與獨立註解行；被 `$include` 的檔一律遞迴處理。
- 預設 **dry-run**：列出每個要刪的 RR + 檔案 + 行號，不改檔；加 `--apply` 才真的寫回；寫入前原檔另存為 `<path>.bak`。
- **不觸發 reload**、不發 SIGHUP、不連線任何 network。Operator 自行決定何時 `shadowdns reload`。

## Non-Goals

- 不修改 runtime 行為：`OverridableTypes`、`filterBackupRecords`、`alias.Resolve` 等語意完全不動。
- 不擴充 backup 的 overridable 類型（CNAME、A、AAAA 不會變成可 override）。
- 不保留原檔的空行與獨立註解行（operator 已確認可接受）。
- 不支援 in-place 原子寫入（single fsync+rename 以外的更強保證，例如 journal、rollback）；依賴 `.bak` 備份即可。
- 不處理非 backup zone（root zone 完全不動）。
- 不處理 `$GENERATE` 的展開結果——directive 本身保留，不嘗試把它展開後比對，因為 `miekg/dns` parser 不展開 `$GENERATE`，且 runtime 也不展開。
- 不在本 change 引入伺服器端 API（例如 HTTP endpoint 觸發 prune）；只做 CLI。

## Capabilities

### New Capabilities

- `prune-backup-cli`: 新增 `shadowdns prune-backup` sub-command，離線掃描 backup zone 檔案並移除相對 root zone 冗餘的 records，採 line-based 刪除、預設 dry-run、寫入前自動備份。

### Modified Capabilities

(none)

## Impact

- Affected specs:
  - New: `openspec/specs/prune-backup-cli/spec.md`
- Affected code:
  - New:
    - `cmd/shadowdns/prune_backup.go` — cobra subcommand 入口、flag 定義、dry-run vs apply 分派。
    - `cmd/shadowdns/prune_backup_test.go` — subcommand level 測試（flag 解析、required flag、exit code）。
    - `internal/prunebackup/` — 新 package，包含：
      - `prunebackup.go` — 主流程：收集 (backup, root) 對、呼叫比對、回報結果。
      - `diff.go` — RRSet 級別比對邏輯（overridable 集合相等、非 overridable 豁免 SOA/apex-NS）。
      - `rewrite.go` — line-based 檔案改寫：定位 RR 行範圍、剝除空行與獨立註解行、遞迴處理 `$include`。
      - 各檔對應的 `*_test.go`。
  - Modified:
    - `cmd/shadowdns/main.go` — 在 `newRootCmd()` 內註冊 `newPruneBackupCmd()`（與現有 `newReloadCmd()` 同層）。
    - `Makefile` — `make completions` 自動涵蓋新 subcommand，無需額外目標。
    - `CHANGELOG.md` — release-please 自動；本 change 不手動編輯。
  - Removed: (none)
