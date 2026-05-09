## Context

ShadowDNS 在 v0.16.0（current main）每 DNS query 進入 `internal/server.(*Server).ServeDNS` 後會呼叫 `alias.Detect(qname, origins, st.Aliases)`（`internal/server/handler.go:147` 與 `:476`）做最長後綴匹配，找出 query 對應的 loaded zone。`alias.Detect` 對 `loadedZones`（`ServerState.ZoneOrigins[viewName]`，每 view 預建好的 slice）做線性遍歷，每個 zone 呼叫 `dnsutil.IsInZone(qname, zone)` 一次。

`IsInZone`（`internal/dnsutil/dnsutil.go:47-49`）目前實作：

```go
func IsInZone(name, zone string) bool {
    return name == zone || strings.HasSuffix(name, "."+zone)
}
```

`"."+zone` 是 Go runtime 的 `runtime.concatstring2`，每次呼叫 alloc 一個新 string + memmove。2026-05-09 ns2 dnspyre 壓測 30s CPU profile（檔案 `.local/dnspyre/pprof/cpu-20260509-104840.pprof`）顯示 `runtime.memmove` flat 54.03%、`runtime.concatstrings` cum 70.32%、`alias.Detect` cum 79.54%、`IsInZone` cum 78.21% — concat 是當前最大 CPU 消費點。

### Constraints

- 修改不得改變 `IsInZone` 對任何輸入的回傳值（語義透明）。其他 callsite 如 `internal/zone/parser.go:66`（zone parse-time bailiwick check）與 `internal/api/server.go:223`（ephemeral API delete-by-fqdn）皆假設原語義。
- 不得 alloc：本 change 的點睛之處就是消除 hot-path alloc，新實作必須 `b.ReportAllocs()` 顯示 0 allocs/op。
- 部署目標 `bench-ns2` 為 v0.x.x 實驗階段，可接受 breaking 風險，但同 binary 必須通過既有 unit test 與 integration test。

## Goals / Non-Goals

**Goals**
- 移除 `IsInZone` hot-path 的 `"."+zone` concat。
- 達成 0 allocs/op，micro-benchmark `BenchmarkIsInZone` 事前驗證。
- 部署 ns2 後同條件再抓 pprof，驗證 `runtime.concatstring2 + memmove` cum 從合計 70% 降至 < 5%。
- 達 dnspyre QPS +30% 門檻，主目標 +50%。

**Non-Goals**
- 不改 `alias.Detect` 的 O(N_zones) loop 結構（trie / sorted-suffix bsearch / dotted-cache 皆 out of scope）。
- 不改 `ServerState.ZoneOrigins` 資料結構。
- 不改其他 `IsInZone` callers（zone parser、ephemeral API）的呼叫端 — 純函式 in-place 改寫即達。
- 不動 `dns-server` capability spec 的需求。

## Decisions

### 修法用「邊界檢查」而非「預 cache dottedZones」

採用三層條件：

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

**Alternatives considered**：
- **預 cache `dottedZones []string`**：在 `ServerState` 增 parallel slice，每個 zone 預先 prepend "." 一次。被否決，需動 `ServerState` 結構與 reload／SIGHUP 路徑，影響面遠大於單檔；且邊界檢查方案已 0 alloc 達同等效果。
- **`name[idx:] == "." + zone[0:1]` 做 prefix-byte 比對**：被否決，與 `strings.HasSuffix` 內建 `memequal` fast path 等價但寫法繞，沒收益。
- **改用 `dns.IsSubDomain`（miekg lib）**：被否決，miekg 版本要 split 兩 name 為 label 後比較，比目前重，且 vendor 改動風險高。

### 條件順序：`len` → byte 邊界 → `HasSuffix`

短路順序最佳化讓 99% 不匹配 zone 在 O(1) 內 fail：

1. `name == zone`：handle 完全相等（極少數 case，但避免後續 `len(name) > len(zone)` 為 false 走 fall-through）。
2. `len(name) > len(zone)`：O(1) 比較。長度相等已被條件 1 涵蓋；長度小於 zone 不可能是子網域。
3. `name[len(name)-len(zone)-1] == '.'`：O(1) byte access，確認 zone 邊界。在 false case（例如 `qname=foo.example.com.` 對 `zone=mple.com.`，倒數第 9 byte 是 'a' 非 '.'），這條會立刻 fail，避免進入 `HasSuffix` 的 `memequal`。
4. `strings.HasSuffix(name, zone)`：stdlib，內部走 `memequal` fast path，false case 平均 1-2 byte 即 fail。

**Alternatives considered**：
- **先 `HasSuffix` 再邊界檢查**：被否決，`HasSuffix` 在 false case 仍要走 `memequal` 一次；先做 byte 邊界檢查可在更多 case 早 fail。
- **省略 byte 邊界檢查**：被否決，`HasSuffix` 對 `qname=foo.example.com.` vs `zone=ample.com.` 會誤判 true（後綴 `ample.com.` 確實匹配）— 沒 byte 邊界檢查就破壞語義。

### 同 change 補 micro-benchmark

`internal/dnsutil/dnsutil_test.go` 新增 `BenchmarkIsInZone`，覆蓋四個 case：
- `name == zone`（命中條件 1）
- `name` 是 `zone` 子網域，匹配（命中條件 2/3/4 全 true）
- `name` 後綴像 zone 但邊界不對（條件 3 fail，例如 `qname="foo.eample.com."`、`zone="ample.com."`）
- `name` 完全無關（`HasSuffix` fail）

每 case 用 `b.ReportAllocs()` 斷言 0 allocs/op；同時記錄 ns/op 作為 commit message 的事前證據。

**Alternatives considered**：
- **不寫 benchmark**：被否決，A1/A2 失敗的關鍵教訓是「沒事前 micro-benchmark」— dnspyre 是黑盒、太晚發現方向錯。事前 benchmark 是低成本 sanity check。
- **整合 `BenchmarkAliasDetect`**：被否決，跨 package（alias → dnsutil）超出本 change scope。本 change 純粹驗證 `IsInZone` 自己。

## Risks / Trade-offs

- **Risk**：邊界檢查在某 edge case 與舊實作行為不同 → Mitigation：unit test 補一組 fixture 覆蓋（a）`name == zone`、（b）正常子網域、（c）後綴像 zone 但邊界不對（false negative 必須維持）、（d）`name` 比 `zone` 短（false 必須維持）、（e）空字串（兩端皆 false 必須維持）。實作前先寫 test，TDD 驗證。
- **Risk**：`alias.Detect` cum 從 79.54% 預期降到 < 10%，但實際可能仍有 30-50% 因為 loop 本身與 `strings.HasSuffix` 的 `memequal` 仍佔一些 — 此時 dnspyre QPS 提升可能不及 +50% 主目標 → Mitigation：以 +30% 為門檻，主目標 +50% 為 stretch；profile-after 仍有 cum > 30% 時規劃 follow-up 攻 loop（trie / bsearch）。
- **Risk**：邊界檢查對 unicode / non-ASCII zone 行為改變 → Mitigation：DNS zone 必為 ASCII（IDN 在更上層 punycode 轉碼），且 `len(name) > len(zone)` 的 byte length 比較對 ASCII 全 case 正確。test 補一組 punycode fixture 確認。
- **Trade-off**：本修法不解決 `alias.Detect` 的 O(N_zones) loop scaling 問題。當 N_zones 漲到萬級或 query rate 拉到 50k+ QPS 時，loop 本身會浮上來成為新瓶頸。本 change 接受此 trade-off，留 follow-up 解。

## Migration Plan

1. 在 `internal/dnsutil/dnsutil_test.go` 先寫一組 unit test 覆蓋語義 edge case（5 種輸入 fixture）+ 一組 micro-benchmark，且 unit test 對舊實作仍 pass（建立 baseline）。
2. 修改 `internal/dnsutil/dnsutil.go:47-49` 為新實作。
3. 跑 `make test`（含 race）確認既有 handler / parser / alias / api test 全綠。
4. 跑 `go test -bench=BenchmarkIsInZone -benchmem ./internal/dnsutil/...`，記錄 ns/op + allocs/op 對比寫入 commit message。
5. `make lint` 確認 golangci-lint 無新 warning。
6. 跑 `make smoke` 確認 `--dry-run` 不 panic。
7. 本地 `make deb` 產出 `shadowdns_0.0.0~eliminate-isinzone-alloc_amd64.deb`，scp 至 `bench-ns2`，dpkg -i 安裝。
8. systemctl restart shadowdns，等 ≥12 min warm-up（dig probe ×3 NOERROR 確認 ready）。
9. 從 ns1 跑 dnspyre CNAME + A 各 2 輪（解決 4.4% variance 問題）。
10. 同條件再從 ns1 抓 30s CPU profile（pprof endpoint 已於 ns2 override.conf 啟用 `--pprof-enable`）。
11. 寫 `compare-baseline-vs-eliminate-isinzone-alloc.md`，含 dnspyre 數字對照、pprof before/after `top -cum` 對比、是否達門檻判定。
12. **Rollback**：working tree discard 即回到 main；ns2 端可由下次 deploy 自然覆蓋（v0.x.x 實驗環境，不需主動 rollback）。

## Open Questions

- **profile-after 仍 > 30% 時要不要 commit？** 提議：仍 commit。本 change 已 0 alloc 為 hot path 移除可量化的退步來源；即使 QPS 提升不及 +50% 也不應退回有 alloc 的舊版。Follow-up change 攻 loop。
- **是否同 change 把 `internal/zone/parser.go:66` 與 `internal/api/server.go:223` 的非 hot-path callsite 也改為新實作？** 提議：純函式 in-place 改寫，這兩處自動受益、無需改 callsite。design 沒額外 decision。

