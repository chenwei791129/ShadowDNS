## Why

ShadowDNS 的 alias 機制只在 in-bailiwick 時改寫名稱，導致 CNAME RDATA 中**中段 label** 為 root zone 名（如 `host.root.com.cdn.example.net.`）時，alias 端（如 `backup.com`）回應沒有把 `root.com` 改寫為 `backup.com`，跟 BIND9 的答案不一致。

這個 bug 由 dnspyre 一致性檢查作業發現，實際影響範圍是 `shadowdns.yaml` 中所有「以 templated CNAME 為 alias 模式」的 alias group。在目前正式環境的 alias 配置下，所有 alias 端 CNAME 答案都帶錯 label。

## What Changes

- `internal/alias/rewrite.go`：新增 `RewriteNameAnywhere(n, root, backup string) string` 函式，做 label-boundary anywhere-match 改寫；既有 `RewriteName` 維持 in-bailiwick suffix-only 規則供 owner name 使用。
- `internal/alias/rewrite.go`：`RewriteRR` 對 RDATA 名稱欄（`*dns.CNAME.Target`、`*dns.NS.Ns`、`*dns.MX.Mx`、`*dns.PTR.Ptr`、`*dns.SRV.Target`、`*dns.SOA.Ns`、`*dns.SOA.Mbox`）依新增的 per-alias-group 旗標決定改用 `RewriteNameAnywhere` 或維持 `RewriteName`；header owner name 維持 `RewriteName`。
- **BREAKING（v0.x.x 階段可接受）** `shadowdns.yaml` 的 `aliases` 欄位 schema 從 `map[string][]string` 擴充為支援 per-group 旗標（具體形狀於 design.md 決定）。新形狀必須能表達「此 alias group 的 RDATA 是否套用 anywhere-match 改寫」。
- `internal/config/aliases.go` + `internal/shadowdnscfg/config.go`：解析新 schema；保留向後相容路徑（舊 list 形式視為 `rewrite-rdata-labels: false`）或在 v0.x.x 階段直接 break。
- 新增 unit test 覆蓋 in-bailiwick / 中段 label / label-boundary 保護（`myroot.com` 不誤匹配）/ 多次出現 / 旗標關閉時行為不變。
- 新增 integration test 用實際 `root.com` + alias 結構模擬 dnspyre 觀察到的 query。

## Non-Goals

- **不**改 `RewriteName` 的 in-bailiwick 語意，避免影響 owner name 改寫的 DNS 標準行為。
- **不**做純 string substring 改寫（必須以 label boundary 為單位）。
- **不**處理 zone load 時預先 rewrite 一份「物化 alias 副本」的優化路徑，留待後續效能 change 處理。
- **不**修改上游 BIND9 zone 產生器 — 該系統無 alias 概念，不在本專案 scope。
- **不**改 dnspyre 一致性檢查工具的 alias 認知邏輯。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `alias-resolver`：新增「RDATA 名稱欄在 opt-in 時依 label-anywhere 規則改寫」需求；釐清 owner name 仍走 in-bailiwick 規則。
- `shadowdns-config`：`aliases` 欄位 schema 擴充以支援 per-group `rewrite-rdata-labels` 旗標；舊純 list 形式語意明確化（等同旗標 false）。

## Impact

- Affected specs：`alias-resolver`、`shadowdns-config`
- Affected code：
  - Modified：
    - internal/alias/rewrite.go
    - internal/alias/override.go
    - internal/config/aliases.go
    - internal/shadowdnscfg/config.go
  - New：
    - internal/alias/rewrite_anywhere_test.go
    - test/integration/alias_rdata_rewrite_test.go
  - Removed：(none)
- 行為差異：所有在 `aliases` 設定中啟用 `rewrite-rdata-labels` 的 group，其下 backup origin 的 CNAME / MX / NS / SRV / PTR / SOA RDATA 名稱欄將額外觸發中段 label 改寫；未啟用的 group 保持現行 in-bailiwick 行為。
- Ops 動作：bench-ns2 上 `/etc/shadowdns/shadowdns.yaml` 對 `root.com` alias group 加上 `rewrite-rdata-labels: true`，於 release notes 提示。
