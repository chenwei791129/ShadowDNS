## Summary

把 `dnsutil.LookupKey`（`internal/dnsutil/dnsutil.go:37-42`）與 `alias.RewriteName` / `alias.RewriteNameAnywhere`（`internal/alias/rewrite.go:45-60` + `:80-110`）裡每呼叫 1 次就分配 1 個中間 string 的 concat 路徑（`ToLower + TrimSuffix + "."`、`"." + root`）改為 inline 的 boundary check + fast-path，hot path 0~1 alloc/call（從目前的 2~3）。簽章不變、行為等價。

## Motivation

連續三次優化（`eliminate-zone-origin-concat`、`migrate-geoip-to-mmdb-v2`、`eliminate-client-addr-string-alloc`）都打 sub-threshold（+0.6% / +1.35% / -0.24%），原因之一是各自 base CPU 占比都 < 1%，物理上限 < +1% QPS、必落在 ±2~3% run variance 之下。本 change 把 plan §4 Tier B4 與 §1.6 #3 兩個獨立小優化合併打一次，靠累積收益（預估 +1~3%）跨過 sub-threshold gate；即便仍 sub-threshold，hot-path alloc 數量結構性下降的價值與前三次一致。

具體 alloc 來源（pprof on bench-ns2 CNAME workload，30k QPS）：

1. `dnsutil.LookupKey(name)`（`internal/dnsutil/dnsutil.go:41`）：`strings.ToLower(strings.TrimSuffix(name, ".")) + "."` 在 caller 端輸入「全小寫且帶尾 `.`」（測資與 production zone data 的常態）時，仍因 concat `+ "."` 強制 alloc 一次新 string。`alias.Detect` 與 `view.Matcher.Resolve` 兩條 hot path 都會走。預估 ~9k allocs/sec。

2. `alias.RewriteName(n, root, backup)`（`internal/alias/rewrite.go:54`）：`suffix := "." + root` 每 call 1 alloc。透過 `alias.RewriteRR` 從 `alias.Override` 進入 NOERROR 回應的 finalize 路徑，per-RR 命中。Plan §1.5 pivot pprof 顯示殘留 `concatstring2 56.66%` 主要在這。

3. `alias.RewriteNameAnywhere(n, root, backup)`（`internal/alias/rewrite.go:88-104`）：未使用 `"." + root` concat，但 `strings.ToLower(n)` 對 mixed-case 0x20 query 也 alloc。本 change 不動 ToLower（無法消除，必須轉小寫做比對）；只動其調用點與 boundary check 結構，使其與 RewriteName 修法一致風格。

## Proposed Solution

### 1. `LookupKey` 加 fast-path

```go
func LookupKey(name string) string {
    if name == "" {
        return ""
    }
    if isAlreadyLookupKey(name) {
        return name
    }
    return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}

// 全小寫 ASCII + 已帶尾 "." → 輸出 == 輸入，可直接 return
func isAlreadyLookupKey(s string) bool {
    n := len(s)
    if n == 0 || s[n-1] != '.' {
        return false
    }
    for i := 0; i < n-1; i++ {
        c := s[i]
        if c >= 'A' && c <= 'Z' {
            return false
        }
    }
    return true
}
```

「dnspyre 測資 + production zone data」常態命中 fast path → 0 alloc/call；mixed-case 0x20 query 走原路徑（行為不變）。

### 2. `RewriteName` 用 index 數學取代 `"." + root` concat

```go
func RewriteName(n, root, backup string) string {
    if n == "" {
        return n
    }
    lower := strings.ToLower(n)
    if lower == root {
        return backup
    }
    // 等價於 strings.HasSuffix(lower, "." + root) 但不分配中間字串。
    // root 必含尾 "."（caller 合約），故 root 不可能為空。
    rl := len(root)
    ll := len(lower)
    if ll > rl+1 &&
        lower[ll-rl-1] == '.' &&
        lower[ll-rl:] == root {
        prefix := n[:ll-rl-1]
        return prefix + "." + backup
    }
    return n
}
```

簽章不變、4 個外部 caller（`alias/soa.go:20-21`、`alias/override.go:150`、`transfer/axfr.go:117`）+ 2 個內部 caller（`alias/rewrite.go:143,145` via `RewriteRR`）皆透明。每 call 1 alloc（最終 `prefix + "." + backup` concat，無法消除 — output 本來就是新 string）。

### 3. `RewriteNameAnywhere` 不改 alloc 路徑，但同步調整 boundary check 風格

讓兩函式 boundary check 結構一致以利後續維護（lower 字串 index 比對、無中間 concat）。alloc 行為不變（仍 1 ToLower + 1 Builder.Grow 出來的最終 string）。此項屬 code hygiene 而非效能優化。

## Non-Goals

- 不動 `dnsutil.IsInZone`（已在 `eliminate-zone-origin-concat` 完成 byte-equality fast path）。
- 不動 `LookupKey` 對 non-ASCII / IDN 的處理 — fast-path 只命中全 ASCII 小寫，否則退回 `strings.ToLower` 原路徑。
- 不動 `RewriteName` / `RewriteNameAnywhere` 的對外簽章。caller surface（4 外部 + 2 內部）不需任何修改。
- 不動 `RewriteRR`（純 dispatcher，無 alloc）。
- 不消除 `RewriteName` / `RewriteNameAnywhere` 的最終 `prefix + "." + backup` 或 `Builder` 輸出 — output 是新字串、無法消除。
- 不消除 `strings.ToLower(n)` 對 mixed-case input 的 alloc — 這是必要的轉換步驟，只能在 caller 端 cache `lower` form 而那超出本 change scope。
- 不順便處理 plan §4 Tier B2 (Prometheus counter pre-resolve) 或 #2 (alias.Detect 資料結構)。
- 不評估 miekg/dns v2 遷移。

## Alternatives Considered

- **caller 端把 `"." + root` 預 cache 進 alias group struct**（`dottedRoot` field）：拒絕。需改 `RewriteName` / `RewriteNameAnywhere` 簽章 → 6 個 caller 都要改、blast radius 大。inline-only 改法用 index 數學達到完全相同的 alloc reduction（1 → 0 中間 concat），更乾淨。
- **`LookupKey` 在 caller 端 cache lookup form**：拒絕。caller 多數已是 zone load time 算好的（`alias.Detect` 拿 `Group.RootKey`），剩下 hot path 上的 query name 是逐 query 變動，cache 不可行。fast-path 是唯一合理途徑。
- **改用 `bytes` package 的 `EqualFold` 取代 `strings.ToLower + ==`**：拒絕。`EqualFold` 對 Unicode case folding 較慢，且 0x20 query 是純 ASCII，`ToLower` fast path 已足。

## Impact

- Affected specs: 無觀察行為變化（純內部優化；對 alias-rewrite 與 dns-server capability 行為要求 byte-equivalent）。
- Affected code:
  - Modified: internal/dnsutil/dnsutil.go (LookupKey + 新 helper isAlreadyLookupKey)
  - Modified: internal/alias/rewrite.go (RewriteName + RewriteNameAnywhere boundary check 改 index 數學)
  - Modified: internal/dnsutil/dnsutil_test.go (新增 LookupKey fast-path 測試 + benchmark)
  - Modified: internal/alias/rewrite_test.go (新增 RewriteName boundary case + benchmark)
