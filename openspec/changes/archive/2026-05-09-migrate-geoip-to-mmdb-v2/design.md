## Context

`internal/view/geoip_country.go` 與 `internal/view/geoip_asn.go` 是 `view.Matcher` 解析 client IP → view name 的兩個 GeoIP 查詢點。當前實作使用 `maxminddb-golang v1.13.1`（檔案頂層 `import "github.com/oschwald/maxminddb-golang"`）的 reflection-based decode：

```
var rec struct { Country struct { ISOCode string `maxminddb:"iso_code"` } `maxminddb:"country"` }
netIP := ip.AsSlice()
if err := c.db.Lookup(netIP, &rec); err != nil { return "", false }
```

2026-05-09 在 bench-ns2 上 dnspyre `-c 100 -d 3m` 工作負載抓的 30s CPU profile 顯示：

| 函式 | flat | flat% | cum | cum% |
|---|---|---|---|---|
| view.(*CountryDB).Lookup | 0.62s | 0.52% | 15.35s | **12.85%** |
| └─ runtime.newobject | — | — | 6.59s | **42.93% of Lookup** |
| └─ maxminddb.(*Reader).Lookup | — | — | 7.98s | 51.99% of Lookup |
| └─ netip.Addr.AsSlice | — | — | 0.16s | 1.04% of Lookup |
| view.(*ASNDB).Lookup | 0.04s | 0.033% | 0.75s | 0.63% |

整個 process `runtime.newobject` 累積成本中，**62.88% (6.59s/10.48s) 來自 CountryDB.Lookup 一處**。源頭是 v1 reflection 路徑為了寫入 `&rec` 結構而每次 alloc 一個 record 物件 + `reflect.Value` 中介物。ASNDB 流量在當前 view 設定下用得少（cum 0.63%），但實作病灶相同。

`maxminddb-golang/v2 v2.1.1` 已透過某個 transitive dependency 進入 `go.sum`（`go.mod:187` 標記為 indirect）。v2 的 API 變動：

- `(*Reader).Lookup(ip netip.Addr) Result` — 直接吃 `netip.Addr`，無需 `ip.AsSlice()`；回傳 value 型別 `Result`（非 pointer）。
- `Result.DecodePath(v any, path ...any) error` — 把 mmdb 路徑指定的 leaf 值寫入 `v`，不需要傳完整 struct。
- `Result.Found() bool` — 顯式判斷 lookup 是否命中 record。
- `Metadata` struct 的 `DatabaseType`、`BuildEpoch` 欄位名稱與型別在 v2 沿用，現有 callers (`cmd/shadowdns/main.go:409-410` 與 tests) 不需改動。

`grep -rn '"github.com/oschwald/maxminddb' cmd/ internal/` 確認 v1 import 只在 `internal/view/geoip_country.go` 與 `internal/view/geoip_asn.go` 兩處。`Metadata()` 的型別變動透過 caller 端只摸 `.DatabaseType` / `.BuildEpoch` 欄位達成 source-compatible。

## Goals / Non-Goals

**Goals**
- 消除 `view.(*CountryDB).Lookup` 與 `view.(*ASNDB).Lookup` 路徑上的 per-call record-struct alloc（pprof 上的 `runtime.newobject` 來源）。
- 對外行為（input → output）byte-for-byte 等價：相同 IP 輸入下 ISO code / ASN 輸出值與目前一致；相同錯誤情境（lookup error、空結果）視為 no-match。
- `(*CountryDB).Lookup(netip.Addr) (string, bool)` 與 `(*ASNDB).Lookup(netip.Addr) (uint32, bool)` 對外簽章不變。
- v1 dependency 從 `go.mod` direct require 完全移除；v2 從 indirect 升為 direct。
- 加 `Benchmark*` 量化 alloc/op 變化作為後續 regression baseline。

**Non-Goals**
- 不動 `internal/view/matcher.go` 與 `(*Matcher).Resolve` / `ruleMatches` 邏輯。
- 不動 `internal/view/loader.go` 與 `OpenCountryDB`/`OpenASNDB` 的 path-based 構造方式。
- 不動 `cmd/shadowdns/main.go`、`internal/server/build.go` 對 `*CountryDB`/`*ASNDB` 的注入與啟動使用。
- 不順便處理 `view.(*Matcher).Resolve` 的 linear-scan rules（cum 15.18%，是另一個獨立優化目標）。
- 不順便加 per-IP cache（dnspyre 單 source-IP 會 100% 命中假象，production gain 不會 transfer）。
- 不順便處理 plan §1.6 排序的 #2（`alias.Detect` 資料結構）或 #3（`alias.RewriteName` concat）。

## Decisions

### 決策 1：CountryDB 與 ASNDB 同 change 一起遷移

**Choice**：兩個檔案在同一個 change 內遷移到 v2。

**Rationale**：
- 兩檔結構為 mirror image（70 行幾乎 copy-paste，差別只在 record struct 的 leaf field 與 Lookup 簽章回傳型別）。
- 只動 CountryDB 會留下 ASNDB 的 v1 import → `go.mod` 必須兩版本並存 → 後續任何 maxminddb 升級都要動兩處。
- ASN 雖然 cum 只 0.63%，但實作病灶相同；分批遷移帶來的 process overhead 大於同 change 完成的邊際成本。

**Alternatives considered**：
- 只遷移 CountryDB（拒絕 — 上述 lingering migration debt）。
- 分兩個 change 連續做（拒絕 — 沒有額外風險隔離價值，code 結構幾乎一致）。

### 決策 2：使用 `Result.DecodePath` 而非 `Result.Decode`

**Choice**：CountryDB 用 `result.DecodePath(&iso, "country", "iso_code")`；ASNDB 用 `result.DecodePath(&asn, "autonomous_system_number")`。`iso` 為 `var string`、`asn` 為 `var uint`，皆 stack-local。

**Rationale**：
- `Result.Decode(v any)` 仍需要傳一個 destination struct（雖比 v1 reflection 路徑輕，但仍會建 struct）。
- `Result.DecodePath(&leaf, path...)` 直接寫進 leaf 變數，stack-allocated，最大化 alloc 消除。
- 程式碼更短，意圖更清楚（只摸需要的欄位）。

**Alternatives considered**：
- `Result.Decode(&rec)` + 保留原 struct（拒絕 — 仍需 struct alloc）。
- 自寫 mmdb byte-level decoder（拒絕 — 重造輪子、與 lib 維護路徑脫鉤）。

### 決策 3：對外 `Lookup` 簽章不變

**Choice**：保留 `(*CountryDB).Lookup(netip.Addr) (string, bool)` 與 `(*ASNDB).Lookup(netip.Addr) (uint32, bool)`。

**Rationale**：
- 此 change 純為內部實作優化；caller (`internal/view/matcher.go`) 透明。
- 對外不洩漏 maxminddb 型別，封裝邊界乾淨。
- 任何簽章變動都會強制 `internal/view/matcher.go` 跟改 → 違反 Non-Goal「不動 matcher」。

### 決策 4：`Metadata()` 回傳型別跟著 v2 換

**Choice**：`(*CountryDB).Metadata() v2.Metadata`、`(*ASNDB).Metadata() v2.Metadata`（型別變動但欄位不變）。

**Rationale**：
- 隱藏 v2 型別需要新增 wrapper struct，沒有實際好處（callers 只摸 `.DatabaseType`/`.BuildEpoch` 兩個欄位，v2 兩個欄位都有）。
- v2 `Metadata` struct 與 v1 在 `DatabaseType`（string）、`BuildEpoch`（uint）等欄位名稱與 type 一致 — caller-side source compatible。
- grep 確認除 `cmd/shadowdns/main.go:409-410`（讀 `.BuildEpoch`）與 tests（讀 `.DatabaseType`/`.BuildEpoch`）外無其他 caller。

**Alternatives considered**：
- 在 `view` package 自定義 `type Metadata struct { ... }` wrapper（拒絕 — YAGNI；現有兩個欄位已足夠且 v2 type 穩定）。

### 決策 5：v1 dependency 完全移除

**Choice**：`go.mod` 移除 `github.com/oschwald/maxminddb-golang v1.13.1` direct entry，v2 從 indirect 升為 direct，跑 `go mod tidy`。

**Rationale**：
- 遷移後沒有任何 internal code 引用 v1 import path。
- 兩版本並存會增加 binary size、build time 與 dependency audit 負擔，且沒有功能性收益。
- 若某個 transitive dependency 仍需 v1，`go mod tidy` 會自動保留為 indirect — 不會破壞 build。

### 決策 6：以 `Result.Found()` 作為命中判斷，保留 v1 audit 行為

**Choice**：`if !result.Found() { return "", false }`；`result.Err()` 與 `DecodePath` 的 error 也視為 no-match。

**Rationale**：
- v1 路徑下 `c.db.Lookup(netIP, &rec)` 的 error（無論是 IP not in db 還是 mmdb 結構錯誤）都被歸類為 no-match（v1 註解：「treat as no-match (not error) per audit discipline」）。
- v2 把「沒 record」與「真正錯誤」分開為 `Found()` 與 `Err()`，但對 view-matcher 規格而言兩者都應視為 no-match — 保持 byte-equivalent。
- 同時保留「ISO code 為空字串視為 no-match」、「ASN 為 0 視為 no-match」的 v1 過濾規則。

## Implementation Contract

### 觀察行為（必須與 v1 路徑等價）

| 輸入 | 預期輸出（v1 與 v2 必須一致） |
|---|---|
| `nil` 接收者或 `c.db == nil` | `("", false)` 或 `(0, false)` |
| 有效 IP 命中 record，ISO code 非空 | `(<ISO>, true)`，`<ISO>` 為 mmdb 內字面值（可能含大小寫） |
| 有效 IP 命中 record，ASN ≠ 0 | `(<ASN>, true)` |
| 有效 IP 命中 record，但 leaf 為空字串或 0 | `("", false)` 或 `(0, false)` |
| 有效 IP 但 db 中無 record | `("", false)` 或 `(0, false)` |
| Lookup 過程任何 error | `("", false)` 或 `(0, false)` — 不 panic、不傳播 error |

### 介面與資料形狀

- `(*CountryDB).Lookup(ip netip.Addr) (string, bool)` — 簽章不變。
- `(*ASNDB).Lookup(ip netip.Addr) (uint32, bool)` — 簽章不變。
- `(*CountryDB).Metadata() v2.Metadata` — 型別由 `v1.Metadata` 變為 `v2.Metadata`；欄位 `DatabaseType string`、`BuildEpoch uint` 不變（caller-side source compatible）。
- `(*ASNDB).Metadata() v2.Metadata` — 同上。
- `OpenCountryDB(path string) (*CountryDB, error)` 與 `OpenASNDB(path string) (*ASNDB, error)` — 簽章不變；內部 `maxminddb.Open` → v2 同名函式。
- `(*CountryDB).Close() error`、`(*ASNDB).Close() error` — 簽章不變。

### 失敗模式

- `OpenCountryDB`/`OpenASNDB` 對檔案不存在或 mmdb 格式錯誤回傳 error — 由 caller (`internal/view/loader.go`) 在 startup 視為 fatal（既有行為，不變）。
- `Lookup` 路徑下任何 error（v2 `result.Err()`、`DecodePath` error）一律 swallow 為 no-match — 與 v1 audit discipline 一致。

### 驗收條件

- `make test`（`go test -race -count=1 ./...`）全綠。
- `make lint`（`golangci-lint run`）clean。
- `make smoke`（`shadowdns --dry-run`）clean。
- `make build` 與 `make build-linux` 均產出 binary，無 v1 import 殘留：`go list -deps ./... | grep 'oschwald/maxminddb-golang$'` 應為空。
- 新增 `BenchmarkCountryDB_Lookup` 與 `BenchmarkASNDB_Lookup` 跑 `go test -bench=. -benchmem ./internal/view/...`，記錄 alloc/op 並對比遷移前數值（預期下降）。
- 部署到 `bench-ns2` 後，從 `bench-ns1` 跑 dnspyre `-t A -d 3m -c 100 --no-distribution @cname-domains.txt`，並擷取 30s CPU pprof，比對 `view.(*CountryDB).Lookup` cum 應顯著下降（目標：< 7%，從 12.85%）；`runtime.newobject` cum 也應顯著下降（目標：< 6%，從 8.77%）。

### 範圍邊界

**In scope**：
- internal/view/geoip_country.go
- internal/view/geoip_asn.go
- internal/view/geoip_country_test.go（加 benchmark）
- internal/view/geoip_asn_test.go（加 benchmark）
- go.mod、go.sum

**Out of scope**：
- internal/view/matcher.go、internal/view/loader.go、internal/view/netmatch.go
- cmd/shadowdns/main.go、internal/server/build.go
- 其他 capability（alias、zone、server、metrics 等）

## Risks / Trade-offs

- **v2 API 對 leaf type 的支援邊界未驗證** → 在 task 1 透過撰寫 `DecodePath(&iso, ...)` 與 `DecodePath(&asn, ...)` 並讓現有 unit test 跑過驗證；若 v2 不支援直接寫入 `*string` / `*uint`，退回 `Result.Decode` + 小 struct 並更新 design 決策 2。
- **`Metadata` 型別變動可能 break 第三方 caller** → grep 整個 repo 確認 import path 只有那兩個檔案；caller 只摸 `.DatabaseType` / `.BuildEpoch`，v2 兩個欄位都有；測試會 catch 任何 build break。
- **`go mod tidy` 可能保留 v1 為 indirect** → 若某個 transitive dependency 仍 import v1，go.mod 會自動標記為 indirect 而不刪除；功能無影響，binary size 略大；可以接受作為實際限制（task 3 用 `go list -deps ./... | grep 'maxminddb-golang$'` 驗證 first-party 程式碼無 v1 引用）。
- **效能未達 +5~8% QPS 預期** → benchmark 失敗時用 pprof diff（`-base`）量化 alloc/newobject 變化，根據實測數據更新 plan §1.6 並決定下一步（不撤回此 change，因為內部 code 簡化本身有價值）。
- **v2 行為微小語意差異**（例如 `Found()` 對 default-route 結果的判斷與 v1 swallow-error 是否完全一致）→ 現有 unit test 在 country 命中、IP 不在 mmdb、nil receiver 三個 case 都已 cover；若有差異會在 `make test` 暴露。

## Migration Plan

此 change 為「就地置換」，不需 staged rollout：

1. 在 working tree 完成所有檔案遷移與 benchmark 撰寫；run `make test/lint/smoke`。
2. 用 `release-shadowdns` skill local-change mode build deb，deploy 到 `bench-ns2`。
3. 從 `bench-ns1` 跑 dnspyre `-c 100 -d 3m` CNAME workload，擷取 30s pprof。
4. 比對遷移前 baseline-recheck（`.local/dnspyre/pprof/cpu-after-20260509-162811-baseline-recheck.pprof`）與遷移後 pprof：`go tool pprof -base <pre> <post>` 應顯示 `view.(*CountryDB).Lookup` cum 下降、`runtime.newobject` cum 下降、QPS 在 +5~8% 區間。
5. 若達標：commit + archive；不達標：保留實作（因內部簡化有價值），更新 plan §1.6 標記實際結果。
