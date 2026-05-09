## Summary

把 `internal/dnsutil.IsInZone` 的 `"."+zone` hot-path string concat 改為純邊界檢查，消除每 query × loaded zones 量級的 alloc + memmove。

## Motivation

依 plan `.local/plans/2026-05-06-qps-regression-vs-bind9-fix.md` §1.5 Pivot — 2026-05-09 在 `bench-ns2` 跑 dnspyre `-c 100` 壓測同時抓的 30s CPU profile 顯示：

| Function | Cum % | Flat % |
|---|---|---|
| `internal/alias.Detect` | 79.54% | 1.07% |
| `internal/dnsutil.IsInZone` (inline) | 78.21% | 1.16% |
| `runtime.concatstring2 / concatstrings` | 70.32% | 12.64% |
| `runtime.memmove` | 54.03% | 54.03% |
| `internal/server.replyWithAnswer` | 5.01% | 0.017% |

`IsInZone` 函式本體：

```go
func IsInZone(name, zone string) bool {
    return name == zone || strings.HasSuffix(name, "."+zone)
}
```

`"."+zone` 每次呼叫一次 string concat。`alias.Detect` 對每 query 遍歷整份 `loadedZones`（`ServerState.ZoneOrigins[viewName]`），每 zone 呼叫 `IsInZone` 一次。N=2000 zones × 10k QPS = 約 20M concat/sec，這就是 `runtime.memmove` 吃 54% 單機 CPU 的來源。

之前依本 plan §3 推導出的兩個 change（`eliminate-udp-response-double-pack` A1、`pool-response-msg` A2）都失敗：profile 證實兩者攻的是 5% CPU 區（`replyWithAnswer`），ceiling 不可能超過 +5%。本 change 是 pivot 後的第一個基於 profile 證據的 fix。

## Proposed Solution

`internal/dnsutil/dnsutil.go:47-49` 改為三層邊界檢查：

```go
func IsInZone(name, zone string) bool {
    if name == zone {
        return true
    }
    return len(name) > len(zone) &&
           name[len(name)-len(zone)-1] == '.' &&
           strings.HasSuffix(name, zone)
}
```

語義不變（仍判定 `name` 等於 `zone` 或為其子網域），但消除 `"."+zone` concat。三條件順序：先 `len` 比較（O(1)）、再 byte 邊界檢查（O(1)）、最後 `strings.HasSuffix`（內部走 `memequal` fast path，從 string 尾端比起，false case 通常 1-2 byte 即 fail）。

同 change 補一組 `BenchmarkIsInZone` micro-benchmark 在 `internal/dnsutil/dnsutil_test.go`，事前驗證 `b.ReportAllocs()` 顯示 0 allocs/op、ns/op 相對舊實作顯著下降。

## Non-Goals

- **不**改 `alias.Detect` 的 O(N_zones) loop 結構（不引入 trie / sorted-suffix bsearch / 預 cache `dottedZones` slice）。pprof 顯示 `alias.Detect` 自己 flat 僅 1.07%，loop overhead 微小；移除 concat 後預期 cum 會從 79.54% 大幅下降。若部署後實測仍 > 30%，再規劃 follow-up change 攻 loop。
- **不**改 `ServerState.ZoneOrigins` 結構或新增 parallel slice。
- **不**改其他 `IsInZone` callers（`internal/zone/parser.go:66` zone parse-time、`internal/api/server.go:223` ephemeral API path）— 兩者語義透明、不在 hot path、不需特殊處理。
- **不**改變任何 observable DNS 行為：response code、RR 內容、TC bit、AA flag、wire size、truncation 行為皆需與 pre-change 一致。
- **不**改 `dns-server` capability spec 級需求 — 純內部 hot-path 優化。

## Alternatives Considered

- **預 cache `dottedZones []string`**（在 `ServerState` 加一個 leading-dot prepended 版本）：被否決，需動 `ServerState` 結構與 reload 流程，影響面比 `dnsutil` 一個 file 大很多；且邊界檢查方案已 0 alloc 達同等效果。
- **`alias.Detect` 同 change 改 trie / sorted-suffix bsearch**：被否決，pprof 證據不支持。Loop overhead 才 1.07%，先驗證單點修法後再決定是否需要。避免重蹈 A1/A2「沒 profile 證據就動結構」的覆轍。
- **完全跳過 `strings.HasSuffix`，純手寫 byte-by-byte 比較**：被否決，`strings.HasSuffix` 已是 stdlib 且內建 `memequal` 快速路徑，重寫無收益且增加維護成本。

## Impact

- Affected specs: 無。本 change 為內部 implementation refactor，不改變 `dns-server` 的 observable 行為或 spec 需求。
- Affected code:
  - Modified: internal/dnsutil/dnsutil.go
  - Modified: internal/dnsutil/dnsutil_test.go
  - New: (none)
  - Removed: (none)
- 部署與驗收：
  - 本地 `make deb` 產出 `shadowdns_0.0.0~eliminate-isinzone-alloc_amd64.deb`，scp 至 `bench-ns2`，dpkg -i 安裝。
  - 等待 ≥12 min warm-up 後，從 `bench-ns1` 對 `198.18.0.8` 跑 dnspyre `-t A -d 3m -c 100 --no-distribution`，CNAME 與 A 各跑 2 輪（已知同 binary run-to-run variance 可達 4.4%）。
  - 同條件再從 ns1 抓 30s CPU profile（`http://198.18.0.8:9153/debug/pprof/profile?seconds=30`），驗證 `runtime.concatstring2 + memmove` cum 從合計 70% 降至 < 5%。
  - 比對對象：`.local/dnspyre/report/baseline-shadowdns-pre-eliminate-udp-double-pack.md`（pre-A1 baseline，CNAME 10,538 QPS / A 10,741 QPS）。
  - 驗收門檻：QPS ≥ baseline +30%（CNAME ≥ 13,700、A ≥ 13,950）；主目標 +50%（CNAME ≥ 15,800、A ≥ 16,100）。p99 退步 ≤ 10%；NXDOMAIN/REFUSED rate 相對 baseline ±0.05 pp 以內。
  - 產出 `.local/dnspyre/report/compare-baseline-vs-eliminate-isinzone-alloc.md` 與 raw txt 輸出、新一份 pprof profile（before/after diff）。
