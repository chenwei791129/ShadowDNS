## Summary

把 `internal/zone/Zone.LookupWildcard` 與 `Zone.FollowCNAME` 的 `"."+z.Origin` per-call concat 改用既有 `dnsutil.IsInZone` 的 boundary-byte 模式，消除 hot-path 上殘存的 `runtime.concatstring2` 4.71%。

## Motivation

`eliminate-isinzone-alloc` change（已 archive）已把 `dnsutil.IsInZone` 的 concat 消除，QPS 從 ~10.5k → ~32k（+200%）。但 2026-05-09 run #2 pprof 顯示 `runtime.concatstring2` cum 仍佔 4.71%，且 callers 集中在：

- `zone.(*Zone).FollowCNAME` 47.07% of concat samples
- `zone.(*Zone).LookupWildcard` 38.19% of concat samples

兩處都用同一模式：

```go
originSuffix := "." + z.Origin                  // alloc per call
...
if !strings.HasSuffix(parent, originSuffix) { ... }
```

語義恰好等於 `dnsutil.IsInZone(parent, z.Origin)`（前面已單獨處理 `parent == z.Origin` 的 case），可直接替換消除 alloc。

預期影響：~+4% QPS（32k → ~33.5k），延續同一已驗證的 boundary-check 技術。

## Proposed Solution

替換點 1：`internal/zone/zone.go:127, 143`（`LookupWildcard`）

```go
// 移除
originSuffix := "." + z.Origin

// 修改
if parent == z.Origin {
    break
}
if !strings.HasSuffix(parent, originSuffix) {
    return nil, false
}

// 改為
if parent == z.Origin {
    break
}
if !dnsutil.IsInZone(parent, z.Origin) {
    return nil, false
}
```

替換點 2：`internal/zone/zone.go:243, 252`（`FollowCNAME`）

```go
// 移除
originSuffix := "." + z.Origin

// 修改
if target != z.Origin && !strings.HasSuffix(target, originSuffix) {
    break
}

// 改為（IsInZone 已涵蓋 target == z.Origin 的 true case）
if !dnsutil.IsInZone(target, z.Origin) {
    break
}
```

兩處都加 `dnsutil` import 至 zone.go（目前未使用，無循環引用風險 — `dnsutil` 不依賴 `zone`）。

## Non-Goals

- **不**改 `IsInZone` 自身（已是 boundary-check + slice equality 終態，0 alloc、inline-friendly）。
- **不**動 `Zone` struct 加 cache 欄位（如 `dottedOrigin`）— 改 struct 影響 reload／SIGHUP path、blast radius 大；用 `IsInZone` 已達同等 0-alloc 效果且不需動 struct。
- **不**改其他 `strings.HasSuffix` 使用點（`internal/alias/rewrite.go` 的 suffix-replace 語義不同、`internal/zone/parser.go` 是 parse-time 不在 hot path）。
- **不**為 `Zone.LookupWildcard` / `Zone.FollowCNAME` 自身重構結構（loop 結構保留）。
- **不**改變 observable DNS 行為：wildcard fallback、CNAME chain following、break/return 路徑皆需與 pre-change 一致。

## Alternatives Considered

- **在 `Zone` struct 加 `dottedOrigin string` 預存 leading-dot 版本**：被否決，需動 Zone struct 與 reload 流程，影響面遠大於本 change；`IsInZone` boundary-check 已達同等 0-alloc 效果且零結構動。
- **在兩個 callsite inline 重寫 boundary-check（不呼叫 `IsInZone`）**：被否決，會造成 boundary-check pattern 在 codebase 三處重複（`dnsutil.IsInZone` + 兩個 zone callsite）。直接呼叫 `IsInZone` 既複用又避免重複。
- **完全保留現狀，等 #2 view.CountryDB.Lookup 改完再回頭做**：被否決，#1 是已驗證的低風險改動，可獨立 ship；先做 #1 累積 +4% gain，#2 規劃時可獨立比對。

## Impact

- Affected specs: 無。本 change 為內部 implementation refactor，不改變 `dns-server` 或 `zone-parser` 的 observable 行為或 spec 級需求。
- Affected code:
  - Modified: internal/zone/zone.go
  - Modified: internal/zone/zone_test.go
  - New: (none)
  - Removed: (none)
- 部署與驗收：
  - 本地 `make deb` 產出 `shadowdns_0.0.0~eliminate-zone-origin-concat_amd64.deb`，scp 至 `bench-ns2`，dpkg -i 安裝。
  - 等待 ≥15 min warm-up 後，從 `bench-ns1` 對 `198.18.0.8` 跑 dnspyre `-t A -d 3m -c 100 --no-distribution`，CNAME 與 A 各跑 2 輪。
  - 同條件再從 ns1 抓 30s CPU profile，驗證 `runtime.concatstring2` cum 從 4.71% 降至 < 1%（僅留 `dnsutil.LookupKey` 等次級 source）。
  - 比對對象：`.local/dnspyre/report/compare-baseline-vs-eliminate-isinzone-alloc.md` 的 Run #2 數字（CNAME 31,619 / A 32,886 QPS）。
  - 驗收門檻：QPS 相對 run #2 baseline +2% 以上（CNAME ≥ 32,250 / A ≥ 33,540），p99 退步 ≤ 5%，NXDOMAIN/REFUSED rate 變化在 ±0.5 pp 以內。
  - 主目標：+4% QPS（CNAME ≥ 32,884 / A ≥ 34,201）。
  - 產出 `.local/dnspyre/report/compare-isinzone-vs-eliminate-zone-origin-concat.md` 與 raw txt 輸出、新一份 pprof profile（before/after diff）。
