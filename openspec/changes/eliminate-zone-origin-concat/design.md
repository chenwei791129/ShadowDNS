## Context

`eliminate-isinzone-alloc`（已 archive 於 2026-05-09）把 `dnsutil.IsInZone` 的 `"."+zone` concat 移除，QPS 從 ~10.5k → ~32k。同期 dnspyre run #2 的 CPU profile 顯示 `runtime.concatstring2` cum 殘存 4.71%，callers 集中在：

- `internal/zone/Zone.FollowCNAME`（line 243）— `originSuffix := "." + z.Origin`
- `internal/zone/Zone.LookupWildcard`（line 127）— 同上

兩處都使用 `originSuffix` 做 boundary 判定（`strings.HasSuffix(name, originSuffix)`），語義等同 `dnsutil.IsInZone(name, z.Origin)`（且已涵蓋 `name == z.Origin` 的 true 分支）。

`dnsutil.IsInZone` 上一個 change 已驗證為 0 alloc + inline-eligible，直接複用即可。

### Constraints

- 不得改變兩個函式的對外行為：wildcard fallback semantics、CNAME chain depth、break/return 路徑必須與 pre-change byte-equivalent。
- 不得引入循環 import（`dnsutil` 目前不依賴 `zone`，加入 `dnsutil` 至 `zone` 安全）。
- 必須通過既有 `internal/zone/zone_test.go` 的 LookupWildcard 與 FollowCNAME test。

## Goals / Non-Goals

**Goals**
- 消除 `LookupWildcard` 與 `FollowCNAME` 的 per-call `"."+z.Origin` alloc。
- 部署後 pprof 驗證 `runtime.concatstring2` cum 從 4.71% 降至 < 1%。
- 達 QPS 相對 run #2 baseline +2% 門檻、+4% 主目標。

**Non-Goals**
- 不改 `Zone` struct 加 cache 欄位（會影響 reload／SIGHUP 路徑）。
- 不重構 `LookupWildcard` 或 `FollowCNAME` 的 loop 結構。
- 不動 `internal/alias/rewrite.go` 的 `"."+backup` concat（語義不同，是 suffix-replace 而非 boundary-check）。
- 不改 spec 級需求。

## Decisions

### 用 `dnsutil.IsInZone` 而非 inline boundary-check

兩個 callsite 改用 `dnsutil.IsInZone(name, z.Origin)`，移除本地 `originSuffix` 變數。

**Alternatives considered**：
- **在 callsite inline boundary-check（複製 IsInZone 邏輯）**：被否決，會讓 boundary-check pattern 在 codebase 三處重複（`dnsutil` + 兩個 zone callsite），未來改動需同步三處。
- **在 `Zone` struct 加 `dottedOrigin string`**：被否決，需動 `Zone` 結構與 zone-load／reload 路徑，blast radius 過大；`IsInZone` 已達同效果。
- **保留 originSuffix 但用 `sync.Pool` 重用 string**：被否決，string 不可變、pool 沒幫助。

### LookupWildcard 的 `parent == z.Origin` 早 break 保留

原碼：

```go
if parent == z.Origin { break }
if !strings.HasSuffix(parent, originSuffix) { return nil, false }
```

改後：

```go
if parent == z.Origin { break }
if !dnsutil.IsInZone(parent, z.Origin) { return nil, false }
```

`IsInZone` 內部會再做一次 `parent == z.Origin` check，但這在進入此分支時已知為 false（前一行已 break）。多一次 cmp 成本可忽略，且邏輯讀起來清楚。

**Alternatives considered**：
- **拆開 break 條件，全部交給 IsInZone**：被否決，`break` 與 `return nil, false` 是不同 control flow（前者繼續向 caller 找 wildcard，後者明確「qname 已超出 zone」），合併會讓 wildcard fallback 行為改變。

### FollowCNAME 的 `target != z.Origin && ...` 折疊成單一 IsInZone

原碼：

```go
if target != z.Origin && !strings.HasSuffix(target, originSuffix) {
    break
}
```

改後：

```go
if !dnsutil.IsInZone(target, z.Origin) {
    break
}
```

`IsInZone` 對 `target == z.Origin` 直接回 true，所以 `!IsInZone(...)` 等同 `target != z.Origin && !HasSuffix(target, "."+z.Origin)`。語義 byte-equivalent。

**Alternatives considered**：
- **保持兩個 condition**：被否決，IsInZone 已內含 equal-fast-path，重複會多一個無意義的 `target != z.Origin` 檢查。

## Risks / Trade-offs

- **Risk**：`IsInZone` 對 edge case（`parent == ""` 等）的 return value 與 `HasSuffix(parent, "."+z.Origin)` 不同 → Mitigation：`eliminate-isinzone-alloc` 已涵蓋 12 fixture，包含 `name == zone`、子網域、後綴像 zone 但邊界不對、`name` 短於 zone、空字串 — 所有 case `IsInZone` 與舊 `HasSuffix(name, "."+zone)` 行為一致。本 change 信任那組 fixture 為基準，無需重新驗證 `IsInZone` 自身。
- **Risk**：`zone.go` 加 `dnsutil` import 觸發循環依賴 → Mitigation：已驗證 `dnsutil` 只 import `net`、`strings`、`miekg/dns`，不依賴 `zone`。
- **Trade-off**：CFG 上 `IsInZone` 對 `parent != z.Origin` case 多執行一次 `name == zone` cmp（之前 break 已處理）。成本約 0.1 ns/call，相對省下的 alloc + memmove 可忽略。

## Migration Plan

1. 在 `internal/zone/zone_test.go` 加（或擴充）覆蓋 `LookupWildcard` 與 `FollowCNAME` 的 fixture，包含：
   - parent/target 等於 z.Origin（早 break 路徑）
   - parent/target 是 z.Origin 嚴格子網域（HasSuffix true 路徑）
   - parent/target 後綴像 zone 但邊界不對（HasSuffix false 路徑，例如 origin="foo.com.", target="barfoo.com." → break）
   - parent/target 完全無關（HasSuffix false）
2. 跑 `go test ./internal/zone/...`，確認舊實作下 fixture 全 pass（建立 TDD baseline）。
3. 改寫 `LookupWildcard`（line 122-150）與 `FollowCNAME`（line 236-265），移除 `originSuffix` 變數，改用 `dnsutil.IsInZone`，加 `dnsutil` import。
4. 跑 `go test ./internal/zone/...`，確認 fixture 在新實作下全 pass（語義不變）。
5. 跑 `make test`（含 race）確認 handler / alias / api 等下游無 regression。
6. 跑 `make lint` / `make smoke` 全綠。
7. 本地 `make deb` → scp → dpkg -i 至 `bench-ns2`，systemctl restart shadowdns。
8. 等 ≥15 min warm-up（dig probe ×3 NOERROR 確認 ready）。
9. 從 ns1 跑 dnspyre CNAME + A 各 2 輪。
10. 同條件再從 ns1 抓 30s CPU profile，驗證 `concatstring2` cum < 1%。
11. 寫 `compare-isinzone-vs-eliminate-zone-origin-concat.md`，含 dnspyre 數字、pprof before/after、是否達 +2% / +4% 門檻判定。
12. **Rollback**：working tree discard 即回到 main；ns2 端可由下次 deploy 自然覆蓋。

## Open Questions

- **A workload NXDOMAIN +1.58 pp 是否會在本 change 後消失？** 推測無相關（NXDOMAIN 增加是 view.CountryDB.Lookup 在高負載的副作用，與 zone concat 無關）。本 change 不解，留 #2 解。比較報告中需驗證 NXDOMAIN rate 與 run #2 是否一致（即仍 ~2.55%），確認 #1 沒誤打誤撞改變什麼。
- **profile 驗證後 concatstring2 < 1%，剩下的 source 是否值得繼續攻擊？** 預期剩餘 source 為 `dnsutil.LookupKey`（`strings.ToLower(...) + "."`），cum 可能 0.5% 以下。值不值得做 follow-up 由本 change 完成後的 profile 決定，不在本 change scope。
