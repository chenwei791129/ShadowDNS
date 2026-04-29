## Context

ShadowDNS 目前的 alias 機制（`internal/alias/rewrite.go`）使用 `RewriteName` 對 RR 的 owner name 與 RDATA 名稱欄做 in-bailiwick suffix-only 改寫：

- 若 `n == root` 或 `n` 結尾為 `.<root>.` → 改寫
- 否則 → 原封不動

這對 owner name 是正確的（owner 永遠在 root zone bailiwick 內），但對 CNAME RDATA target 不夠用。生產資料中存在「以 templated CNAME 為 alias 模式」的 alias group：以 `root.com` 為 canonical，多個 alias 域名（`backup.com` / `mirror.com` / `another-alias.com` 等）的 zone 檔內 CNAME 結構共存：

```
canonical:  host IN CNAME host.root.com.cdn.example.net.
alias #1:   host IN CNAME host.backup.com.cdn.example.net.
alias #2:   host IN CNAME host.mirror.com.cdn.example.net.
```

CNAME target 的 root 名（`root.com`）落在 **中間 label**，不是 suffix（target 結尾是 `.cdn.example.net.`），因此目前 `RewriteName` 不會改寫，alias 端回的 CNAME 帶錯 label。

調查上游 BIND9 zone 產生器後確認：BIND9 端**沒有** alias / templating 概念，每個 zone 的每筆 CNAME 都是 DB 獨立 row，verbatim 寫入 zone 檔。alias group 內各 zone 看起來像 template 純粹是維運慣例，上游 schema 不強制這點。換言之：「alias group 全體的 CNAME 都是 templated」這個假設不能由程式 silently 假設，必須由 config 顯式聲明。

## Goals / Non-Goals

**Goals:**

- 讓 ShadowDNS 對 alias 端查詢能正確回應 templated CNAME 的 backup-origin label，與 BIND9 的逐 zone 獨立資料行為一致。
- 把「此 alias group 的 RDATA 套用 label-anywhere 改寫」設為**顯式 opt-in**，不在 code 內隱假設。
- 維持 owner name 的 in-bailiwick suffix 規則（DNS 標準語意），不污染既有正確路徑。
- 設計時考慮 query hot path 效能 — 改寫實作必須 allocation-free（不使用 `strings.Split`）。

**Non-Goals:**

- 不修改 `RewriteName` 既有語意。
- 不在 zone load 階段做 alias 物化複本（zone-level pre-rewrite）— 留待後續效能 change。
- 不改 BIND9 generator。
- 不調整 dnspyre 一致性檢查工具。
- 不引入「per-record」level 的改寫旗標（粒度太細，現階段需求只到 alias group level）。

## Decisions

### Decision: 採用 B + 顯式 opt-in 旗標（owner 維持 suffix-only，RDATA opt-in 走 anywhere-match）

`internal/alias/rewrite.go` 維持現有 `RewriteName`（in-bailiwick suffix-only）作為 owner name 改寫；新增 `RewriteNameAnywhere` 作為 RDATA 名稱欄改寫；`RewriteRR` 接收一個布林旗標決定 RDATA 走哪條 path：

```go
func RewriteRR(rr dns.RR, root, backup string, rewriteRDATALabels bool) dns.RR
```

Owner name 永遠走 `RewriteName`；RDATA 名稱欄在 `rewriteRDATALabels==true` 時走 `RewriteNameAnywhere`，否則走 `RewriteName`（維持現行行為）。

**為何不選 A（全部走 anywhere-match）：** 雖然兩者對 owner name 結果相同（owner 必在 bailiwick 內），但混用單一規則模糊了「DNS 標準語意（owner）vs team 慣例（RDATA template）」的設計分界，且未來若 owner 路徑出現非 in-bailiwick 邊角 case 會 silent breakage。

**為何不選 C（移除 alias，每 zone 獨立載入）：** 違背使用者明確指示「backup.com 是 root.com 的 alias 這點是固定的」。

### Decision: schema 形狀採 "discriminated map" — 既支援舊 list 形式也支援新物件形式

`shadowdns.yaml` 的 `aliases` 欄位從：

```yaml
aliases:
  root.com:
    - backup.com
    - mirror.com
```

擴充為支援以下兩種形狀並存（per-key discriminator）：

```yaml
aliases:
  # 舊形式（list-of-strings）：等同 rewrite_rdata_labels: false，向後相容
  some-other-canonical.net:
    - alias-x.net
    - alias-y.net

  # 新形式（object）：明確聲明旗標
  root.com:
    rewrite_rdata_labels: true
    members:
      - backup.com
      - mirror.com
      - another-alias.com
```

YAML 解析端在 `internal/shadowdnscfg/config.go` 的 `aliases` 欄位採用 `yaml.Node` 或 custom unmarshaler，依節點型別（sequence vs mapping）分流：

- sequence → 視為 `members` list、`rewrite_rdata_labels: false`
- mapping → 解析 `members` + `rewrite_rdata_labels`（後者預設 false）

解析後在 `internal/config/aliases.go` 統一物件 `AliasGroup{ Members []string; RewriteRDATALabels bool }` 供下游使用。

**為何不選「另起一個並行 map」**（如 `aliases_rewrite_rdata: [root.com]`）：兩個欄位需保持同步，違反 single source of truth；且 reviewer 看 yaml 不容易一眼看出某 group 是否啟用旗標。

**為何不選「全面 break，只保留新物件形式」：** v0.x.x 雖可接受 break，但向後相容成本低（map vs sequence 分流是 yaml 既有能力），保留舊形式對「不需要 RDATA 改寫的單純 alias group」更簡潔。

### Decision: `RewriteNameAnywhere` 採 allocation-free byte-scan 實作

不使用 `strings.Split(".")`（會分配 slice）或 `strings.Replace`（無 label boundary 保護）。實作步驟：

1. 把輸入 name 視為 byte slice，把 root 拆成 label 序列僅在初始化或函式入口計算一次（root 通常很短，可在呼叫端 cache，或函式入口做一次 split）。
2. 用單次 forward pass 找出 root labels 在 name 中的對齊位置（從每個 label 邊界 `.` 之後嘗試比對）。比對成功必須前後都是 label boundary（前是 name 開頭或 `.`，後是 `.`）。
3. 找到後輸出 `name[:start] + backup + name[start+len(root):]`，使用 `strings.Builder` 並預先 `Grow` 到合適 capacity。

**Label boundary 保護**：`myroot.com.foo.com.` 的「myroot」是單一 label，前面不是 `.`（是 `m`），不該匹配。`prefixroot.com.foo.com.` 同理。

**多次出現策略**：採 **左起第一次匹配**（first match wins）。實務上 templated CNAME 只會有一個 root label 序列；多次出現則保留後續部分不動，避免 over-rewrite。

**為何不在 RR 之外快取 root labels：** 每次 query 處理 RR 數量有限（通常 1-3），重複 split 成本低，過早優化。若 future profile 顯示是熱點，再把 root labels 預計算並隨 alias group 物件帶入 `RewriteRR`。

### Decision: 旗標傳遞鏈 — alias group → resolver → RewriteRR

`internal/alias/override.go` 的 `Resolve` / `ResolveExact` / `ResolveWildcard` / `finalizeBackupRRs` 接收一個 `rewriteRDATALabels bool` 參數（或包含此旗標的 `AliasGroup` 物件），並在呼叫 `RewriteRR` 時帶入。呼叫端（`internal/server/handler.go`）在判定 backup zone 時已從 alias map 取得對應 root，改為取得整個 `AliasGroup` 物件即可一併取得旗標。

**為何不用 package-level global：** alias group 旗標是 per-group 設定，全域變數無法表達；且不利於測試。

## Risks / Trade-offs

- **`root.com` 子串誤匹配 alias group 內非 templated CNAME** → 由 `rewrite_rdata_labels` opt-in 控管，預設 false；只在已驗證為 templated 的 group 啟用；建議搭配 zone load 期掃描警告（在後續 change 處理）。
- **YAML schema 雙形支援增加解析複雜度** → 用 custom unmarshaler 集中處理；單元測試覆蓋兩形混用、純舊、純新、空 list。
- **Query hot path 多一次條件判斷** → 旗標檢查為 O(1) bool 比較，可忽略；anywhere-match 本身的 allocation-free 實作才是效能 load-bearing 點，已在 Decision 中明確要求。
- **未來新增非 templated 的 alias group 時，維運者忘記設旗標** → 預設 false 是安全側（行為退回現行），不會 silent 出錯；dnspyre 一致性檢查會在差異發生時告警。
- **記憶體中現有 `[]string` aliases 介面 break** → 內部 API 改 `AliasGroup` 物件，影響 `internal/config/aliases.go` 既有呼叫端；v0.x.x 階段 OK。
