## Context

兩個獨立的中間 string concat alloc 在 hot path 上：

**A. `dnsutil.LookupKey`（`internal/dnsutil/dnsutil.go:37-42`）**

```go
func LookupKey(name string) string {
    if name == "" {
        return ""
    }
    return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}
```

`strings.ToLower(s)` 在 Go 1.25+ 對全 ASCII 小寫輸入會 fast-return 同一字串（不 alloc）。但 `TrimSuffix(s, ".")`（已帶尾 `.`）回傳 `s[:len(s)-1]` 子字串、沒有 alloc，再 `+ "."` concat 永遠 alloc 一個新 string。即便輸入「全小寫且帶尾 `.`」（dnspyre 測資 + production zone data 100% 命中），仍每 call 1 alloc。

caller surface（grep `LookupKey` 33 處 non-test 命中）：分布在 `alias.Detect`、`view.Matcher.Resolve`、`zone load time`、`alias.Override`。hot path 命中率 100%（30k QPS）→ 約 30k allocs/sec。

**B. `alias.RewriteName`（`internal/alias/rewrite.go:54`）**

```go
suffix := "." + root
if strings.HasSuffix(lower, suffix) { ... }
```

`"." + root` 每 call 1 alloc。透過 `RewriteRR` 從 `alias.Override` 進入 NOERROR 回應 finalize 路徑、per-RR 命中。Plan §1.5 pivot pprof 顯示殘留 `concatstring2 56.66%` 主要在這。

caller surface：`alias/soa.go:20-21` (per MakeSOA, not hot)、`alias/override.go:150` (per query hot)、`transfer/axfr.go:117` (AXFR only)、`alias/rewrite.go:143` (RewriteRR per-RR, hot)、`alias/rewrite.go:145` (RewriteRR per-RR alt path, hot)。

**C. `alias.RewriteNameAnywhere`（`internal/alias/rewrite.go:80-110`）**

無 `"." + root` concat（不適用 — 從 anywhere 找而非 suffix）。但 boundary check 結構與 RewriteName 不一致。本 change 同步調整風格、不影響 alloc。

## Goals / Non-Goals

**Goals:**

- `LookupKey` 對「全 ASCII 小寫且帶尾 `.`」輸入：0 alloc/call、return 等價於原行為（測試驗證 string 內容相同）。
- `LookupKey` 對 mixed-case 或無尾 `.` 輸入：行為與原邏輯 byte-equivalent。
- `RewriteName`：`"." + root` concat 消除（從 2 alloc 降到 1，最終 `prefix + "." + backup` 無法消）。簽章不變、6 個 caller 透明。
- `RewriteNameAnywhere`：boundary check 結構對齊 RewriteName 寫法、alloc 不變、行為不變。
- 為 `LookupKey` fast-path / `RewriteName` boundary case 各加 benchmark 與單元測試。

**Non-Goals:**

- 不動 `LookupKey` 對 IDN / non-ASCII 的處理（fast-path 只 cover ASCII，否則退回原路徑）。
- 不動 `RewriteName` / `RewriteNameAnywhere` 對外簽章。
- 不消除 `RewriteName` 最終 `prefix + "." + backup` concat — output 為新字串、無法消。
- 不消除 `RewriteNameAnywhere` 的 `Builder` 輸出 alloc — 同上。
- 不消除 mixed-case input 的 `strings.ToLower(n)` alloc — caller 端 cache lower form 超出本 change scope。
- 不順便處理 plan §4 Tier B2 (Prometheus counter pre-resolve)、#2 (alias.Detect 資料結構)。

## Decisions

### 決策 1：`LookupKey` fast-path 用 ASCII range check 而非 Unicode 判斷

**Choice**：用簡單 byte loop 檢查 `c >= 'A' && c <= 'Z'`，而不是 `unicode.IsUpper(rune)`。

**Rationale**：
- DNS name 在線上 wire format 為 ASCII（IDN 在 client 側已 punycode 轉 ASCII），hot path 上的 query name 不會出現 non-ASCII。
- `unicode.IsUpper` 比 byte range check 慢 5~10x（rune decode + table lookup）。
- 如果遇到 non-ASCII（罕見），fast-path return false → 退回 `strings.ToLower` 原路徑，行為仍正確（`strings.ToLower` 處理 Unicode）。

**Alternatives**：
- 用 `strings.IndexFunc(s, isUpper)` — 拒絕。`IndexFunc` 內部仍逐 byte 取 rune，效能與手寫 byte loop 相比無優勢，多一層函式呼叫成本。
- 也檢查 control chars / 非法字元 — 拒絕。`LookupKey` 不負責 input validation；DNS layer 已上游驗證。

### 決策 2：fast-path 條件「全小寫 ASCII && 尾部 `.`」是雙重必要條件

**Choice**：兩條件缺一不可。如果輸入 `"example.com"`（無尾 `.`），fast-path return false，走原路徑加上 `.`。

**Rationale**：
- `LookupKey` 對外契約是「輸出帶尾 `.`」（`Canonicalize` 同樣保證），必須維持。
- 如果允許 fast-path 命中無尾 `.` 輸入，會回傳不帶 `.` 的字串，破壞 caller 假設（`alias.Detect`、`Matcher.ruleMatches` 都靠尾 `.` 做 boundary check）。
- 雙條件命中率：dnspyre 測資 + zone data 中所有 lookup-fold 形式都同時滿足，命中率接近 100%。

**Alternatives**：
- 只檢查全小寫、不檢查尾 `.` — 拒絕（破壞 contract）。
- 也對「不帶 `.` + 全小寫」輸入做 fast-path（concat 一次） — 拒絕。仍 1 alloc，與原路徑相同；增加 helper 複雜度無收益。

### 決策 3：`RewriteName` boundary check 用 index 數學取代 `"." + root` concat

**Choice**：

```go
rl := len(root)
ll := len(lower)
if ll > rl+1 &&
    lower[ll-rl-1] == '.' &&
    lower[ll-rl:] == root {
    prefix := n[:ll-rl-1]
    return prefix + "." + backup
}
```

等價於 `strings.HasSuffix(lower, "." + root)` + boundary check。

**Rationale**：
- 直接消除 `"." + root` 中間字串 alloc（原版每 call 1 alloc）。
- `lower[ll-rl-1] == '.'` 是 byte compare，inline 友善；`lower[ll-rl:] == root` 是 string equality，編譯器優化為 byte compare。
- 邏輯與原版完全等價：`HasSuffix(lower, "." + root)` 必要條件是 `len(lower) > len(root)`（嚴格大於、因為前面必須有 `.`），所以 `ll > rl+1` 即 `ll >= rl+2` 等價於原檢查。

**Alternatives**：
- 把 `"." + root` 預 cache 進 alias group struct 並改 `RewriteName` 簽章 — 拒絕。簽章改 → 6 個 caller 都要改，blast radius 大；alloc 收益相同。
- 用 `strings.HasSuffix(lower[1:], root)` 等 substring 取巧 — 拒絕。可讀性差、`lower[1:]` 仍需 boundary check 確保前一字元是 `.`。

### 決策 4：`RewriteNameAnywhere` boundary check 風格對齊但保留 ToLower + Builder

**Choice**：把 line 92 的 `strings.Index(lower[start:], root)` 邏輯保持不變；只把現有的 `if absIdx == 0 || lower[absIdx-1] == '.'` 重寫為與 RewriteName 同樣的 index 數學風格（如果有可消除的 concat）。實際上 RewriteNameAnywhere 內部已無 `"." + root` 類 concat（用 `Index` 而非 HasSuffix），故只是文字註解 / 變數命名對齊，無功能變化。

**Rationale**：
- 維護一致性 — 兩函式形似但寫法分歧讓 reviewer 容易誤改。
- 純 hygiene；不增不減 alloc。

**Alternatives**：
- 完全不動 `RewriteNameAnywhere` — 接受。如果註解 / 變數命名對齊空間不大，可整段保留。實作時若發現無調整空間就跳過此項（不算 regression）。

### 決策 5：兩優化合併單一 change（B4 + #3 一起）

**Choice**：B4 與 #3 在同一個 change 同一次部署同一次壓測驗收。

**Rationale**：
- 單獨各打預估 +0.5~2%，落在 ±2~3% run variance 內、難量化。合併打靠累積收益（+1~3%）跨過 sub-threshold gate 機率較高。
- 兩者改動 surface 不重疊（dnsutil.go vs alias/rewrite.go），單一 commit 內回滾切割容易（commit 本身就可拆 file-level revert）。
- 部署 / 壓測流程成本高（~10min build + scp + restart + 3min dnspyre + 30s pprof），合併能節省一半。

**Alternatives**：
- 分兩個 change 順序做 — 拒絕。double 部署 / 壓測成本，且每次都會被 noise 蓋掉。
- 先 B4 快速回收、看實測再決定要不要 #3 — 拒絕。實測 noise 大、B4 單獨多半 sub-threshold，看不到「真實 signal」做不出決定。

## Implementation Contract

### 觀察行為（必須與舊邏輯 byte-equivalent）

| 輸入 | 函式 | 舊行為 | 新行為（必須相同） |
|---|---|---|---|
| `LookupKey("")` | LookupKey | `""` | `""` |
| `LookupKey("example.com.")` | LookupKey | `"example.com."` (alloc) | `"example.com."` (0 alloc, fast-path) |
| `LookupKey("example.com")` | LookupKey | `"example.com."` (alloc) | `"example.com."` (alloc, no fast-path due to no trailing `.`) |
| `LookupKey("Example.COM.")` | LookupKey | `"example.com."` | 相同（fast-path miss、走原路徑 ToLower）|
| `LookupKey("εxample.com.")` | LookupKey | `"εxample.com."` | 相同（non-ASCII fast-path miss、走原路徑）|
| `RewriteName("", "alias.com.", "real.com.")` | RewriteName | `""` | `""` |
| `RewriteName("WWW.alias.com.", "alias.com.", "real.com.")` | RewriteName | `"WWW.real.com."` | 相同 |
| `RewriteName("alias.com.", "alias.com.", "real.com.")` | RewriteName | `"real.com."` | 相同 |
| `RewriteName("other.com.", "alias.com.", "real.com.")` | RewriteName | `"other.com."` (n unchanged) | 相同 |
| `RewriteName("XXalias.com.", "alias.com.", "real.com.")` | RewriteName | `"XXalias.com."` (no boundary `.`) | 相同（boundary check `[ll-rl-1] == '.'` 擋住）|

### 介面

- `LookupKey(name string) string` — 簽章不變。
- `RewriteName(n, root, backup string) string` — 簽章不變。
- `RewriteNameAnywhere(n, root, backup string) string` — 簽章不變。
- 6 個 caller（4 外部 + 2 內部 RewriteRR）皆透明、無需改動。

### 失敗模式

無新失敗模式。所有 fast-path miss 退回原路徑、行為不變。

### 驗收條件

- `make test`（含 race detector）全綠，包含新增的 LookupKey fast-path / RewriteName boundary 單元測試。
- `make lint` clean。
- `make smoke` clean。
- `BenchmarkLookupKey_FastPath`：全小寫帶尾 `.` 輸入 0 allocs/op。
- `BenchmarkLookupKey_SlowPath`：mixed-case 輸入 alloc 數與重構前相同（baseline 1 alloc/op）。
- `BenchmarkRewriteName_SuffixMatch`：suffix-match path alloc 數從 baseline 2 allocs/op 降到 1 allocs/op。
- 部署到 `bench-ns2`、從 `bench-ns1` 跑 dnspyre `-c 100 -d 3m` CNAME 工作負載 + 30s pprof：
  - (a) NXDOMAIN/REFUSED rate 與 baseline 變化 ≤ 0.07pp（接受 variance；前次 migrate-geoip 與 eliminate-client-addr 都觀察到 ±0.06pp 的 timing noise）。
  - (b) `concatstring2` 在 pprof flame graph 上下降，特別是 `RewriteName` 與 `LookupKey` 的子節點。
  - (c) QPS 變化記錄（預期 +1~3%、可能 sub-threshold）。

### 範圍邊界

**In scope**:
- internal/dnsutil/dnsutil.go (LookupKey 函式 body + 新 helper isAlreadyLookupKey)
- internal/alias/rewrite.go (RewriteName 函式 body 第 49-58 行附近)
- internal/dnsutil/dnsutil_test.go (新增 fast-path 測試 + benchmark)
- internal/alias/rewrite_test.go (新增 boundary case + benchmark)

**Out of scope**:
- internal/dnsutil/dnsutil.go 的其他函式（IsInZone 等）
- internal/alias/rewrite.go 的 RewriteRR 與 RewriteQName
- internal/alias/detect.go、internal/alias/override.go、internal/alias/soa.go
- 任何 capability 行為變化

## Risks / Trade-offs

- **`LookupKey` fast-path 漏掉某種 corner case** → 雙條件（全 ASCII 小寫 + 尾 `.`）是必要充分條件、所有 miss 都退回原路徑、行為不變。新增測試會 cover empty / pure-ASCII / mixed-case / no-trailing-dot / non-ASCII 五個 case。
- **`RewriteName` index 數學寫錯 boundary** → 風險低，但測試新增「`XXalias.com.`」（前綴無 `.` boundary）case 確保不誤判。
- **加 helper `isAlreadyLookupKey` 增加 code surface** → 接受。8 行純函式、單一職責；單獨測試容易；未來若 `LookupKey` callers 移轉到 byte-form 可直接用此 helper 的衍生版本。
- **`RewriteNameAnywhere` 對齊風格但無實際收益** → 接受 hygiene 改動，可選；若實作時發現空間有限可跳過。
- **效能不達預期 +1~3%** → 即便 sub-threshold，hot-path alloc 數量結構性下降的價值與前三次 sub-threshold change 一致；commit 標準照舊（只要行為等價即 commit）。
- **commit 內含兩個邏輯獨立的優化** → 切割成 2 個 logical commit（`refactor(dnsutil)` + `refactor(alias)`），git history 仍乾淨。
