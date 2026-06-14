## Context

ShadowDNS 的指標集中在 `internal/metrics`。`New()` 以 `prometheus.NewRegistry()` 建立**自訂** registry（刻意不用全域預設 registry，以隔離第三方函式庫的 collector），並僅 `MustRegister` 11 個 ShadowDNS 自訂 collector。因為自訂 registry 不像 `DefaultRegisterer` 會自動帶入 Go / process collector，`/metrics` 上完全沒有 `go_*` 與 `process_*` 系列。

DNS 查詢的 ECS 與 view 解析發生在 `internal/server` 的 `ServeDNS`：

- ECS 選項處理：在一段以 `s.ECSEnabled && qo.ecs != nil` 為條件的區塊內，呼叫 `dnsutil.ClassifyECS` 得到 `ECSValid` / `ECSOptOut` / `ECSMalformed` 三種分類；`ECSValid` 時取得 ECS 衍生位址 `ecsGeoIP`，`ECSMalformed` 時立即回 FORMERR 並 return。
- view 解析：`geoIP` 預設為來源 IP，當 `ecsGeoIP` 有效時改用之；`viewName := st.Matcher.Resolve(clientIP, geoIP)`。`Matcher.Resolve` 內部以 BIND first-match 語意逐條評估 rule，但**只回傳 view 名稱字串，不回報命中的是 geo rule（country/ASN，吃 geoIP）還是 net/ACL rule（ip/cidr/localhost/localnets，吃 srcIP）**。

既有 `addrFamily(netip.Addr)` 回傳 `ipv4` / `ipv6` / `unknown`，是 `requests_total` 的 `family` label 來源。

## Goals / Non-Goals

**Goals:**

- 讓 `/metrics` 暴露標準 `go_*` 與 `process_*` runtime 指標。
- 提供 ECS 攜帶率與分類健康度的可觀測信號。
- 提供「各 view 的查詢有多少使用了 ECS 衍生 geo 位址」的信號。
- 提供一份可直接 import、開箱即用的 Grafana dashboard 與雙語操作手冊頁。

**Non-Goals:**

- 不變更 `request_duration_seconds` 的 histogram bucket（既有刻意決定）。
- 不改動 `Matcher.Resolve` 契約 / view matcher 熱路徑，故不提供 geo-vs-ACL 的 view 選擇 provenance。
- 不新增 per-zone SOA serial、zone transfer / NOTIFY 計數、request/response size histogram，不修 transfer 計進 `view="refused"` 的污染。
- 不將 dashboard 打包進 `.deb`，不提供 Prometheus alerting rules。

## Decisions

### D1：在自訂 registry 上顯式註冊 Go 與 process collector

在 `metrics.New()` 的 `reg.MustRegister(...)` 呼叫中加入 `collectors.NewGoCollector()` 與 `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})`（`github.com/prometheus/client_golang/prometheus/collectors`）。維持自訂 registry 的隔離設計，只是補上預設 registry 本來會自動帶的兩個 collector。

### D2：ECS 分類計數 `shadowdns_dns_ecs_queries_total{family, status}`

- 新增方法 `func (m *Metrics) RecordECS(family, status string)`，遞增 `CounterVec`。
- `status` ∈ `valid` / `opt_out` / `malformed`，對應 `ClassifyECS` 的三個分類。
- `family` 由 **ECS 選項本身的 Family 欄位**（1=IPv4、2=IPv6）映射為 `ipv4` / `ipv6`，其餘碼為 `unknown`；不使用 `ClassifyECS` 回傳的位址，因為 opt-out 與 malformed 兩種分類不保證有有效位址。
- 在 ECS 處理區塊內，每個分類分支各呼叫一次（malformed 分支需在 `return` FORMERR 之前呼叫）。
- 僅在 `s.ECSEnabled` 為真且查詢攜帶 ECS 選項時才會遞增；ECS 停用或查詢無 ECS 選項時不產生系列。攜帶率以 `sum(ecs_queries_total) / requests_total` 計算。

### D3：view 選擇之 ECS-geo 參與計數 `shadowdns_dns_view_selected_total{view, ecs_geo}`

- 新增方法 `func (m *Metrics) RecordViewSelected(view string, ecsGeo bool)`，遞增 `CounterVec`；`ecs_geo` label 取 `"true"` / `"false"`。
- 在主查詢路徑 view 解析成功後（設定 `viewLabel` / 呼叫 `mw.SetView` 的同一處）呼叫一次，`ecsGeo` 取 `ecsGeoIP.IsValid()`。
- **誠實語意**：`ecs_geo="true"` 表示「該查詢的 view 解析過程中有 ECS 衍生 geo 位址可供 geo rule 評估」，**不**等於「ECS 決定了最終 view」（view 可能仍由 ACL rule 命中）。spec 與手冊均以此語意描述，避免誤導。
- 僅涵蓋主查詢路徑；AXFR/IXFR transfer 路徑不在範圍內。

### D4：不改 view matcher（geo-vs-ACL provenance 延後）

要區分 view 是被 geo rule 還是 ACL rule 選中，需讓 `Matcher.Resolve` 將命中元素的種類沿 `listAccepts` / `elementFires` / `leafMatches` 回傳。這會改動 view matcher 熱路徑與其 spec 契約，風險與 perf-guard 成本高於本次效益，列為後續獨立 change。本次以 D3 的 `ecs_geo` 布林提供誠實但較輕量的信號。

### D5：dashboard 設計

- 單一檔 `grafana/shadowdns-overview.json`，datasource 以 Prometheus 變數 `${DS_PROMETHEUS}`、並提供 `job` / `instance` template 變數。
- 面板分組（rows）：Overview（build_info、process uptime、總 QPS）、Traffic（QPS by proto/family/type、responses by rcode、SERVFAIL/REFUSED/NXDOMAIN 比率，比率查詢以 `... or vector(0)` 處理零流量空向量）、Latency（p50/p90/p99 取自 `request_duration_seconds` histogram）、ECS & Views（per-view QPS、`ecs_geo` 比率、ECS status 分布）、Rate Limiting（`rate_limit_total`）、Config & Zones（`reload_total`、last-reload age、`zones_loaded`/`zones_backup`、`geoip_db_info`）、Runtime（process CPU、RSS、`go_goroutines`、FD、GC duration）、Panics。
- 不打包進 `.deb`；使用者從 repo 取得 JSON。

### D6：文件與比較表

- 新增 `docs/operations/monitoring.md` 與 `docs/operations/monitoring.zh.md`，含 Prometheus scrape 設定範例與 dashboard import 步驟；`.zh.md` 連結指向 base `.md` 路徑並使用中文標題錨點。
- 在 `mkdocs.yml` 新增 Operations 區的英文 `nav:` 條目與 i18n `zh` 的 `nav_translations` 條目。
- 更新 `docs/index.md` / `docs/index.zh.md` 比較表與 `README.md` 功能列表，反映 Grafana dashboard 已提供。
- 文件範例僅用 RFC 2606 網域與 RFC 5737 IP。

### D7：本地 podman Prometheus + Grafana 測試 harness（`.local/`，不 committed）

為了用 ns2 的真實指標資料開發與驗證 dashboard（而非僅 `jq` 解析），建立一個本地 podman 堆疊：Prometheus 抓取 ns2 的 `:9153/metrics`，Grafana provision 本地 Prometheus datasource 並載入 `grafana/shadowdns-overview.json`。**此 harness 已於 propose 階段建置並驗證連通**（target `up`、`shadowdns_*` 可查），落地於 `.local/observability-harness/`（`up.sh`/`down.sh`/`README.md`），任務 4.1 已完成。

- **Sanitization 邊界**：此 harness 的設定檔指向測試主機的真實 host/IP，依 CLAUDE.local sanitize-first gate **MUST 置於 `.local/`（gitignored），不得進入任何 committed 檔案**（本 design 在內，故此處不寫出具體主機名/IP；它們只存在於 `.local/observability-harness/`）。倉庫的 `grafana/shadowdns-overview.json` 維持用 `${DS_PROMETHEUS}` template datasource、不寫死 host，故可 committed。
- **連線方式**：測試主機的 `:9153` 經 SSH local-forward 暴露到本機；macOS 上 podman 跑在 VM 內，Prometheus 以 `host.containers.internal:9153` 抓取轉發埠。具體 SSH 指令見 `.local/observability-harness/up.sh`。
- **用途**：作為任務 4.1 dashboard 開發期間的真實資料驗證迴圈，以及 6.2 最終人工驗證的本地替代/輔助。此 harness 不是出貨產物，不列入 proposal Impact 的 committed 檔案。

## Implementation Contract

**Behavior（出貨後可觀測）：**

- `/metrics` 除既有 11 個 `shadowdns_*` 指標外，另暴露標準 `go_*` 系列（如 `go_goroutines`、`go_memstats_*`、`go_gc_duration_seconds`）與 `process_*` 系列（如 `process_resident_memory_bytes`、`process_cpu_seconds_total`、`process_open_fds`；Linux only）。
- 啟用 ECS 後，攜帶 ECS 選項的查詢會在 `shadowdns_dns_ecs_queries_total{family, status}` 依分類遞增。
- 每個成功解析 view 的查詢會在 `shadowdns_dns_view_selected_total{view, ecs_geo}` 遞增。
- 倉庫提供 `grafana/shadowdns-overview.json`，import 後即顯示上述面板。

**Interface / data shape：**

- `func (m *Metrics) RecordECS(family, status string)`
- `func (m *Metrics) RecordViewSelected(view string, ecsGeo bool)`
- 兩方法 MUST 為 **nil-receiver safe**（開頭 `if m == nil { return }`，沿用既有 `RecordReload` / `SetLastReloadSuccess` / `SetGeoIPInfo` 慣例）。理由：兩者的呼叫點（ECS 處理 switch、view 解析成功處）位於 `ServeDNS` 主流程，**不**在 `RecordRequest` 所在的 `s.Metrics != nil` defer 守衛內，故不可比照 `RecordRequest` 仰賴呼叫端守衛——否則 metrics 停用（`s.Metrics == nil`）時會 nil-deref panic。
- 新指標 namespace/subsystem 沿用 `shadowdns` / `dns`。

**Failure modes：**

- `process_*` 在非 Linux 平台（macOS 開發機）不會出現，屬預期，非 bug；測試與手冊須註明。
- ECS malformed 查詢在記錄計數後仍回 FORMERR；記錄不可改變既有回應行為。
- 零流量時 rcode 比率查詢可能得空向量，dashboard 以 `or vector(0)` 兜底。

**Acceptance criteria：**

- `internal/metrics` 單元測試：透過 `Gather()` 斷言 registry 含至少一個 `go_` 系列（驗證我們的註冊接線，跨平台穩定）；斷言 `RecordECS` / `RecordViewSelected` 以正確 label 遞增正確系列。`process_*` 因 Linux-only 不在單元測試斷言，改於 ns2 以 `curl /metrics` 人工確認。
- `internal/server` handler 測試：ECS valid / opt_out / malformed 三路徑各使 `ecs_queries_total` 對應 `status` 遞增；ECS 衍生 geo 命中時 `view_selected_total` 的 `ecs_geo="true"`，否則 `"false"`。
- dashboard：`make docs-build`（strict）通過；`grafana/shadowdns-overview.json` 可被 `jq` 解析，且其 PromQL 僅引用本 change 暴露的指標名稱。
- 文件：`make docs-build` strict 通過（nav、連結、雙語一致）。

**Scope boundaries：**

- In scope：D1–D6 所列的 collector 註冊、兩個新指標、dashboard JSON、雙語手冊頁與比較表/README 更新。
- Out of scope：matcher provenance、bucket 變更、transfer/serial/size 指標、transfer 的 refused 污染修正、alerting rules、dashboard 進 `.deb`。

## Risks / Trade-offs

- **`process_*` 平台差異**：Linux-only。ns2（Linux）正常，Mac 開發機與 CI 若在非 Linux 會缺；測試只斷言 `go_`，避免假性失敗。
- **誠實但較弱的 ECS/view 信號**：`ecs_geo` 布林無法區分 geo-vs-ACL 命中，這是 D4 為避開 matcher 熱路徑風險的取捨；手冊明述語意，完整 provenance 留待後續 change。
- **熱路徑開銷**：主路徑每查詢多一次 counter `Inc`，ECS 路徑在攜帶 ECS 時多一次；屬可忽略量級，仍由 spectra 結束時的 perf-guard 對 ns2 量測確認無回歸。
- **dashboard 與指標名稱耦合**：dashboard PromQL 直接引用指標名，未來若改名會破圖；以手冊註明並將 JSON 與指標同 repo 版本管理降低脫鉤。
- **基數**：`ecs_queries_total` 約 `family(2) × status(3)`；`view_selected_total` 約 `views × 2`，皆有界。
