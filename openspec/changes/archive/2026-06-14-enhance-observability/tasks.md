## 1. Metrics 層：runtime collector 與新指標定義

> 對應 spec requirement「Expose Go runtime and process metrics」、「Count ECS option classifications」、「Count view selections by ECS-geo participation」；對應 design 決策 D1：在自訂 registry 上顯式註冊 Go 與 process collector、D2：ECS 分類計數 `shadowdns_dns_ecs_queries_total{family, status}`、D3：view 選擇之 ECS-geo 參與計數 `shadowdns_dns_view_selected_total{view, ecs_geo}`。

- [x] 1.1 在 `internal/metrics/metrics.go` 的 `New()` 中，於 `reg.MustRegister(...)` 加入 `collectors.NewGoCollector()` 與 `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})`（import `github.com/prometheus/client_golang/prometheus/collectors`），維持自訂 registry 不變。實作 requirement「Expose Go runtime and process metrics」。驗證：`go run`/`go build ./...` 通過。
- [x] 1.2 在同檔新增 `ecsQueriesTotal` `CounterVec`（`Namespace: "shadowdns"`、`Subsystem: "dns"`、`Name: "ecs_queries_total"`、labels `family`,`status`）與 `viewSelectedTotal` `CounterVec`（`Name: "view_selected_total"`、labels `view`,`ecs_geo`），加入 `Metrics` struct、`New()` 建構與 `reg.MustRegister`。
- [x] 1.3 在同檔新增方法 `RecordECS(family, status string)` 與 `RecordViewSelected(view string, ecsGeo bool)`（`ecsGeo` 映射為 `"true"`/`"false"`）。兩方法 MUST 以 `if m == nil { return }` 開頭做 **nil-receiver safe**（沿用 `RecordReload` 慣例），因其呼叫點位於 `ServeDNS` 主流程、不在 `s.Metrics != nil` 守衛內。

## 2. Handler 層：埋點

> 依 design 決策 D4：不改 view matcher（geo-vs-ACL provenance 延後），本次僅以 `ecsGeoIP.IsValid()` 取布林，不改動 `Matcher.Resolve` 契約。

- [x] 2.1 在 `internal/server/handler.go` 的 ECS 處理區塊（`s.ECSEnabled && qo.ecs != nil` 的 switch）為三個分類各加一次 `s.Metrics.RecordECS(...)`：`ECSValid`→`status="valid"`、`ECSOptOut`→`status="opt_out"`、`ECSMalformed`→`status="malformed"`（malformed 分支須在回 FORMERR 的 `return` 之前呼叫）。`family` 由 ECS 選項的 `Family` 欄位（`*dns.EDNS0_SUBNET`，1=IPv4、2=IPv6）映射為 `ipv4`/`ipv6`/`unknown`，不使用 `ClassifyECS` 回傳位址。此 switch 位於主流程、不在 `s.Metrics != nil` 守衛內，故直接呼叫 `s.Metrics.RecordECS(...)`，安全性由 1.3 的 nil-receiver safe 保證。實作 requirement「Count ECS option classifications」。
- [x] 2.2 在同檔主查詢路徑 view 解析成功處（設定 `viewLabel = viewName` 之後）呼叫一次 `s.Metrics.RecordViewSelected(viewName, ecsGeoIP.IsValid())`。此處主流程不在 `s.Metrics != nil` 守衛內（`mw.SetView` 才在 `if mw != nil` 內），故直接呼叫，安全性由 1.3 的 nil-receiver safe 保證。不涉及 `handleTransfer` 路徑。實作 requirement「Count view selections by ECS-geo participation」。

## 3. 測試

- [x] 3.1 在 `internal/metrics/metrics_test.go` 新增測試：透過 `Gather()` 斷言 registry 含 `go_goroutines` 系列（驗證 collector 註冊接線，跨平台穩定，不斷言 `process_*`）；斷言 `RecordECS("ipv4","valid")` 使 `shadowdns_dns_ecs_queries_total{family="ipv4",status="valid"}` 為 1；斷言 `RecordViewSelected("default", true)` 使 `shadowdns_dns_view_selected_total{view="default",ecs_geo="true"}` 為 1。
- [x] 3.2 在 `internal/server` 的 handler 測試新增案例：ECS `valid`/`opt_out`/`malformed` 三路徑各使對應 `status` 計數遞增（malformed 仍回 FORMERR）；ECS 衍生 geo 命中時 `view_selected_total` 的 `ecs_geo="true"`，無 ECS 時為 `"false"`；無 view 命中（REFUSED）不遞增 `view_selected_total`。

## 4. Grafana dashboard

> 對應 design 決策 D5：dashboard 設計、D7：本地 podman Prometheus + Grafana 測試 harness（`.local/`，不 committed）。

- [x] 4.1 本地 podman Prometheus + Grafana 測試 harness **已建置並驗證連通**，位於 `.local/observability-harness/`（gitignored，含測試主機真實 host/IP，依 sanitize gate 不得 committed；完整用法與具體主機名見該目錄 `README.md`）。**後續所有 dashboard 開發與驗收（4.2、6.2）一律使用此 harness，不需重建**：在該目錄執行 `./up.sh` 啟動（idempotent；自建 SSH tunnel 將測試主機的 `:9153` 轉到本機、起 Prometheus 抓 `host.containers.internal:9153`、Grafana provision datasource uid `shadowdns-prometheus`、自動同步 repo 的 `grafana/*.json`），`./down.sh` 關閉。本 session 已驗證 Prometheus target `up`、`shadowdns_*` 可查；`go_*`/`process_*`/新 ECS 指標在測試主機部署本 change 前為 no data（預期，正是本 change 要補的缺口）。Grafana `http://localhost:3000`、Prometheus `http://localhost:9090`。
- [x] 4.2 [P] 新增 `grafana/shadowdns-overview.json`：datasource 用 `${DS_PROMETHEUS}` 與 `job`/`instance` template 變數；面板分組 Overview、Traffic（含 rcode 比率，零流量以 `or vector(0)` 兜底）、Latency（p50/p90/p99 取自 `shadowdns_dns_request_duration_seconds_bucket`）、ECS & Views（per-view QPS、`ecs_geo` 比率、ECS `status` 分布）、Rate Limiting、Config & Zones、Runtime（`process_*`/`go_*`）、Panics。PromQL 僅引用本 change 暴露或既有的 `shadowdns_*`/`go_*`/`process_*` 指標。先在 4.1 的本地 Grafana import 並以真實 ns2 資料逐面板檢查，再以 `jq . grafana/shadowdns-overview.json` 驗證解析成功。

## 5. 文件（雙語）

> 對應 design 決策 D6：文件與比較表。

- [x] 5.1 [P] 新增 `docs/operations/monitoring.md`（英文）與 `docs/operations/monitoring.zh.md`（繁中）：說明 `-metrics-addr` 端點、Prometheus scrape job 設定範例、`grafana/shadowdns-overview.json` 的 import 步驟、各面板含義，並註明 `process_*` 為 Linux-only 且 `ecs_geo` 的誠實語意（「ECS geo 位址可供 matcher 評估」而非「ECS 決定 view」）。範例僅用 RFC 2606 網域與 RFC 5737 IP。`.zh.md` 連結指向 base `.md` 路徑、使用中文標題錨點。
- [x] 5.2 [P] 在 `mkdocs.yml` 新增 Operations 區的英文 `nav:` 條目指向 `operations/monitoring.md`，並在 i18n plugin 的 `zh` 語言 `nav_translations` 加入對應中文標題條目。
- [x] 5.3 [P] 更新 `docs/index.md` 與 `docs/index.zh.md` 的功能比較表，以及 `README.md` 的功能列表，反映已提供開箱即用 Grafana dashboard。

## 6. 驗證與收尾

- [x] 6.1 執行 `make test`（race）、`make lint`、`make docs-build`（strict）、`make smoke`，全部通過。
- [x] 6.2 以 `release-shadowdns` local-change 模式將本 change 部署到 ns2（Linux）後，透過 4.1 的本地 podman harness 驗收：`curl :9153/metrics`（經轉發埠）確認 `go_*` 與 `process_*` 系列存在，並在啟用 ECS 的查詢下確認 `shadowdns_dns_ecs_queries_total` 與 `shadowdns_dns_view_selected_total` 有資料；在本地 Grafana 開啟已 import 的 `grafana/shadowdns-overview.json`，逐面板確認以真實 ns2 資料正常渲染。
