## Context

ShadowDNS 的 SIGHUP reload 目前由 `internal/server/build.go` 的 `BuildState()` 實作：每次都從零建立全新的 `ServerState`，把 named.conf 裡所有 zone file 完整 re-parse 成新的 `*zone.Zone`，接著 `cmd/shadowdns/main.go` 呼叫 `srv.SwapState(state)` 以 `atomic.Pointer` 原子切換。

生產環境載入 3609 個 root zone 時，in-memory 佔用約 10GB。Reload 期間因為新舊兩份 `ServerState` 並存（舊的要等 in-flight query 釋放 snapshot 後才能被 GC），peak memory 會暫時增長到約 20GB，之後靠 Go runtime 的 GC 才慢慢回收。這對需要預留 memory headroom 的大型部署代價明顯。

實務上每次 reload 幾乎不會有全部 zone 都變更的情境（通常只有少數幾個 zone 因業務需求被編輯 / rsync）。Reload 流程被設計成 full rebuild 的理由是實作簡單、state 彼此完全獨立；這個 trade-off 在 zone 數量不大時成本可接受，但在 3609 個 zone 的規模下已不合理。

另外，release 流程使用 `rsync -avc --inplace` 同步 zone file 到 ShadowDNS node，其中 `-a` 隱含 `-t`（preserve mtime），且 `-c` 明示「source 端可能存在 mtime 相同但內容不同的檔案」。因此變更偵測不能只依賴 mtime + size，必須讀檔做 content hash 才能可靠判斷。

## Goals / Non-Goals

**Goals:**

- Reload 期間 peak memory 接近 baseline（10GB），而非翻倍到 20GB；實際增量只對應「真正變更的 zone」的新版本大小
- 未變更的 `*zone.Zone` 物件在新舊 `ServerState` 之間共用 pointer（shallow reuse），避免重複分配
- 首次 reload（無 `prev` state）自動退回 full rebuild，與現有行為完全相容
- 提供 CLI 旋鈕讓運維人員能在緊急情況下 fallback 到舊行為（escape hatch）
- Reload 失敗時完全 rollback，舊 state 毫無損傷（與現有 failure semantics 一致）

**Non-Goals:**

- **不做 string interning**：雖然 parser 的 `strings.ToLower()` 會為每個 owner name 重複配置 string，降低 baseline memory 理論上有效，但 interning 會引入跨 reload 的全域 state，且與本 change 的 diff reload 正交。留給獨立的後續 change
- **不做 per-zone atomic swap / incremental publish**：不把 `ServerState.RootZones` 的 inner map 改成 `atomic.Pointer`。整體仍維持「先建好新 state → 原子替換」的 blue-green 語意，只是新 state 大量 shallow-reuse 舊 state 的 `*zone.Zone`
- **不做 mmap-based zone data / arena allocator**：過度工程、風險高
- **不改 BIND named.conf schema**：`-reload-verify` 是 ShadowDNS 特有的 operational toggle，不污染 BIND 相容性
- **不動 GeoIP 載入流程**：GeoIP 已經是 reload 時重用，不在本 change 範圍
- **不調整 `runtime/debug.SetGCPercent` 全域 GC 行為**：只在 reload 完成當下觸發一次 GC，不改 runtime tuning

## Decisions

### Diff-based state rebuild with pointer reuse

`BuildState()` 簽名新增 `prev *ServerState` 與 verify 策略參數。當 `prev != nil` 且 verify 策略允許時，對每個 zone file 計算 fingerprint，與 `prev` 中同一 (view, origin) 的 fingerprint 比對：

- 命中且 fingerprint 相同 → 直接 `newRootZones[view][origin] = prev.RootZones[view][origin]`（pointer 共用）
- 未命中 / fingerprint 不同 / `prev == nil` → 呼叫 `zone.ParseFile()` 重新解析

Zone file parse 的結果 `*zone.Zone` 本身在 parse 完成後是 immutable（沒有任何程式碼會 mutate 它的 `Records` map），因此跨 `ServerState` 共用 pointer 是安全的。`ServerState.Aliases`、`ZoneOrigins`、`Matcher`、`AllowTransferACL` 仍完整重建（這些都是 config-level metadata，成本遠小於 zone data）。

**Alternatives considered:**

- **Per-zone atomic swap**：讓 `RootZones` 的 inner map 變成 `atomic.Pointer`，一個 zone 一個 zone 替換。缺點：handler 讀取路徑（`internal/server/handler.go`）要改、失去「整體 reload 是 atomic」的語意、per-view 仍需並存兩份 map。收益不如 pointer reuse。
- **Incremental publish（邊 parse 邊加入 live state）**：打破 reload 失敗時 rollback 的保證。拒絕。

### Zone file fingerprint: size + xxhash64

每個 zone file 的 fingerprint 記錄 `(size int64, hash uint64)`：

- **`size`** 作為 pre-filter — `os.Stat()` 取得，不需讀檔；若 size 不同直接判定變更（快路徑，不浪費 I/O 去算 hash）
- **`hash`** 是檔案內容的 xxhash64；只有在 size 相同時才計算（fallback）。實務上也可以無條件計算，但 size pre-filter 讓新增 / 刪除 / 大幅變更的 zone 省掉一次讀檔

Fingerprint 存在 `ServerState` 內（結構：`map[string]map[string]zoneFingerprint` with key (view, origin)），每次 `BuildState()` 回填新的 fingerprint 到新 state，供下次 reload 比對用。

**Alternatives considered:**

- **mtime + size**：最快（純 stat）但**無法偵測 `rsync -avc --inplace` 保留 mtime 時的內容變更**。這是我們 release 流程的真實情境，不能當預設。作為 `-reload-verify=size` 模式保留，讓 release 流程不保留 mtime 的環境選用
- **ctime + size**：`rsync --inplace` 寫入時會更新 ctime，但若 rsync 判斷內容沒變而完全不寫（`-c` 模式），ctime 也不會動；相當於部分偵測，半調子方案，拒絕
- **CRC32C**：硬體加速極快，但 32-bit 輸出對 3609 個 zone 有約 0.15% 生日碰撞機率，不可接受
- **SHA-256 / MD5**：throughput 低於 xxhash 一個量級，且本場景不需要 crypto hash，過度工程
- **blake3**：快但 API 較新、外部依賴較重，收益不足以 justify

### xxhash library: github.com/cespare/xxhash/v2

採用 [cespare/xxhash/v2](https://github.com/cespare/xxhash)：

- 純 Go 實作，amd64/arm64 有 assembly 加速，throughput 可達 ~13 GB/s
- 廣泛採用（Prometheus、etcd、CockroachDB 等都在用），API 穩定、維護活躍
- 無 cgo 依賴，對 packaging / cross-compile 無影響

**Alternatives considered:**

- **zeebo/xxh3**：throughput 更高（~30 GB/s），但 API 較新、社群採用度較低。cespare/xxhash 已經遠超 I/O bottleneck，多出的速度沒有實際收益
- **hash/maphash（stdlib）**：同 process 內才穩定，跨 process 的 seed 不同，不能跨 reload 比對 fingerprint
- **hash/crc32（Castagnoli）**：碰撞機率問題如上述

### Reload verify mode: CLI flag `-reload-verify=hash|size|none`

選擇 CLI flag 而非 named.conf option，理由：

- BIND 無對應 option，放 named.conf 會誤導熟悉 BIND 的運維人員
- named.conf 的設定要透過 reload 生效；`reload-verify` 若放 named.conf 會造成 chicken-and-egg（改 verify 策略本身的那次 reload 還在用舊策略）
- 此為 process-level operational toggle，與 `-dry-run`、`-no-notify` 同性質
- Enum 而非 bool：為未來擴展（例如加入更積極的比對策略）保留空間

三個值的語意：

| 值 | 行為 | 適用場景 |
|---|---|---|
| `hash`（預設） | size pre-filter + xxhash64 content 驗證 | 一般情境；`rsync -avc --inplace` 保留 mtime 的 release 流程必須用這個 |
| `size` | 僅比對 mtime + size | Release 流程不保留 mtime 的環境；略微省 I/O |
| `none` | 關閉 diff，每次都 full rebuild | Escape hatch：懷疑 diff 邏輯有 bug 時立即 fallback 到舊行為，不需重新 build / deploy 舊版 binary |

與 `-no-notify` 相同，此 flag 是 "sticky across SIGHUP" — 啟動後決定、process 生命期固定。SIGHUP reload 不重讀 CLI，這是刻意的設計（避免策略在 reload 中途改變造成難以推論的行為）。

**Alternatives considered:**

- **named.conf `options { reload-verify }`**：如上述被拒絕
- **Env var**：與 ShadowDNS 現有其他 toggle 不一致（其他都用 CLI flag）
- **硬編碼預設 `hash` 不給 flag**：失去 escape hatch，遇到 diff 邏輯 bug 時只能 downgrade binary

### Post-swap GC trigger

在 `Server.SwapState()` 完成 `s.state.Store()` 之後，呼叫 `runtime.GC()` + `debug.FreeOSMemory()`：

- `runtime.GC()` 強制一次 STW GC，立即回收被舊 state 釋放的物件
- `debug.FreeOSMemory()` 進一步向 OS 歸還 memory（對長駐 daemon process 的 RSS 觀測很重要，否則 runtime 可能保留在 heap 等下次重用）

兩個呼叫都是同步阻塞 — 但這只發生在 reload 完成那一刻（低頻操作）、且 reload 本身不處理 DNS query（listeners 仍在 serve，只是 state swap 後很快觸發 GC）。對 tail latency 有短暫影響但對長期 throughput 無影響。

若 `-reload-verify=none`，舊 state 幾乎完整被丟棄，GC 收益最大；即使在 `hash` 模式下多數 zone 被 reuse，仍有被重建的小量物件 + 舊 `ServerState` 外層結構需要回收，觸發 GC 仍然有意義。

**Alternatives considered:**

- **依賴 Go runtime 自動 GC**：就是現狀，不解決「慢慢降」的 observability 問題；運維看到 RSS 高會誤判為 memory leak
- **只呼叫 `runtime.GC()` 不呼叫 `FreeOSMemory`**：heap 回收但 RSS 不降，監控面仍失真
- **改 GOGC / GOMEMLIMIT**：全域調整，影響 normal 請求處理路徑，scope 不符

### First-reload / startup fallback

啟動時的第一次 `BuildState()` 呼叫 `prev` 為 `nil`，此時 diff 邏輯自動退化成 full rebuild（所有 zone 都走 parse 路徑並記錄 fingerprint）。這也涵蓋：

- 首次 SIGHUP 發生前的啟動路徑
- Test 場景直接呼叫 `BuildState(cfg, aliases, nil, ...)`

沒有額外的 code path，diff 邏輯以 `if prev != nil && prevFingerprint matches` 為條件分支。

### Rollback semantics on partial failure

若任何 zone parse 失敗，`BuildState()` 回傳 error，`reload()` 不呼叫 `SwapState()`，舊 state 完整保留（現有行為）。Pointer reuse 不影響這個保證 — 即使新 state 已經組了一半（部分 zone 是 reused pointer、部分是新 parsed），只要沒呼叫 `SwapState()`，新的 `ServerState` 物件會被直接丟棄、由 GC 回收，reused 的 `*zone.Zone` 仍被舊 state reference 而不會被回收。

## Risks / Trade-offs

- **[Risk] 誤判未變更（fingerprint 碰撞）** → xxhash64 的碰撞機率對 3609 個 zone 實務上可忽略（64-bit 生日問題需要約 2³² 個 input 才有 50% 碰撞），且同時要求 `size` 相同，再疊一層過濾。若真的發生，影響是「內容變了但 ShadowDNS 沒偵測到，繼續用舊資料」→ 運維可透過 `-reload-verify=none` 強制重建。接受此風險
- **[Risk] 讀檔做 hash 增加 reload 耗時** → 1-2GB zone file total，在 SSD 上 < 4 秒、NVMe 上 < 1 秒。Reload 低頻、可接受；`size` 模式可跳過
- **[Risk] Post-swap GC 造成短暫 latency spike** → STW GC 對 10GB heap 的暫停通常在 10-50ms 等級；reload 是營運事件、允許此抖動。若對 tail latency 極度敏感可改 `-reload-verify=none` 並自行管理 GC
- **[Risk] Pointer 共用後意外 mutation** → `*zone.Zone` 現行程式碼無任何 post-parse mutation（parse 完只讀），但若未來有人加功能改 `Records` map 會破壞 reuse 安全。Mitigation：design.md 明文聲明「`*zone.Zone` 為 immutable after parse」，並在 code review 把關；可考慮加 `go vet` 類的靜態檢查於後續 change
- **[Trade-off] diff 邏輯增加 `BuildState()` 複雜度** → 多約 30-50 行程式碼與一個新檔案 `fingerprint.go`，換取 memory peak 從 2× 降到接近 1×，收益遠大於成本
- **[Trade-off] 外部依賴 xxhash/v2** → 多一個 go.mod entry，但 cespare/xxhash 是業界標配、穩定、無 cgo，風險極低
