# 以 Prometheus 與 Grafana 監控

ShadowDNS 透過 HTTP 暴露 Prometheus 指標，並隨附一份可直接 import 的 Grafana
dashboard。本頁說明指標端點、Prometheus scrape 設定、可繪製的指標家族、如何載入
隨附的 dashboard，以及如何用內建的 `pprof` profiler 擷取 CPU／記憶體火焰圖。

## 指標端點

ShadowDNS 在一個由 [`--metrics-addr`](../reference/cli.md) 控制的專用 HTTP
listener 上提供指標。預設為 `:9153`；將其設為空字串即停用端點（此時不會註冊任何
指標，包含 Go runtime 與 process collector）。

```bash
curl -s http://127.0.0.1:9153/metrics | head
```

指標位於自己的 registry，因此回應同時包含 `shadowdns_*` 家族，以及標準的 `go_*`
（Go runtime）與 `process_*` 家族。

!!! note "`process_*` 僅限 Linux"
    `process_*` 家族（resident memory、CPU 秒數、file descriptor、process 啟動
    時間）由 process collector 產生，僅在 Linux 上回報資料。在其他平台上這些
    series 會直接缺席 —— 這是預期行為，不是錯誤。`go_*` 家族則在所有平台都會出現。

## Prometheus scrape 設定

新增一個指向指標端點的 scrape job。`9153` 埠是 ShadowDNS 的預設值。

```yaml
scrape_configs:
  - job_name: shadowdns
    static_configs:
      - targets:
          - "ns1.example.com:9153"
          - "ns2.example.com:9153"
```

進到 Grafana 之前，先在 Prometheus 的 **Status → Targets** 頁面確認 target 為
`up`。

## 指標參考

| 指標 | 型別 | Labels | 意義 |
|------|------|--------|------|
| `shadowdns_build_info` | gauge | `version`、`goversion` | 永遠為 1；建置識別 |
| `shadowdns_dns_requests_total` | counter | `proto`、`family`、`type`、`view` | 收到的 DNS 請求 |
| `shadowdns_dns_responses_total` | counter | `rcode`、`view` | 送出的 DNS 回應 |
| `shadowdns_dns_request_duration_seconds` | histogram | `view` | 請求處理延遲（bucket 100µs–100ms） |
| `shadowdns_dns_ecs_queries_total` | counter | `family`、`status` | ECS 選項分類（僅在 `--ecs-enable` 時） |
| `shadowdns_dns_view_selected_total` | counter | `view`、`ecs_geo` | 主查詢路徑上成功解析的 view |
| `shadowdns_dns_rate_limit_total` | counter | `category`、`action` | RRL 決策 |
| `shadowdns_zones_loaded` | gauge | `view` | 各 view 載入的 root zone 數 |
| `shadowdns_zones_backup` | gauge | `view` | 各 view 載入的 backup-override zone 數 |
| `shadowdns_geoip_db_info` | gauge | `database`、`build_time` | 已載入 GeoIP 資料庫的 build time |
| `shadowdns_reload_total` | counter | `result` | SIGHUP reload 嘗試次數 |
| `shadowdns_config_last_reload_success_timestamp_seconds` | gauge | — | 最近一次成功載入的 Unix 時間 |
| `shadowdns_panics_total` | counter | — | handler 復原的 panic 數 |
| `shadowdns_doh_acme_dropped_total` | counter | `reason` | ACME HTTP-01 listener 中止的連線，依 `reason` 分類 |
| `go_*` | 多種 | — | Go runtime（goroutine、heap、GC） |
| `process_*` | 多種 | — | 行程資源使用（僅限 Linux） |

### ECS 分類指標

`shadowdns_dns_ecs_queries_total` 會在**ECS 處理啟用時**，對每個攜帶 EDNS Client
Subnet 選項的查詢遞增一次（見 [ECS 指南](../guides/ecs.md)）。當 `--ecs-enable`
關閉、或查詢未攜帶 ECS 選項時，此 counter 不會被觸碰。

- `status` 為 `valid`、`opt_out` 或 `malformed` 之一，對應該選項的分類。malformed
  選項仍會如同以往以 FORMERR 回應 —— 記錄指標不會改變回應行為。
- `family` 由 ECS 選項本身的位址家族欄位推導：family 1 為 `ipv4`、family 2 為
  `ipv6`、其餘為 `unknown`。

ECS 攜帶率為 `sum(rate(shadowdns_dns_ecs_queries_total[5m])) /
sum(rate(shadowdns_dns_requests_total[5m]))`。

### View 選擇指標

`shadowdns_dns_view_selected_total` 會對每個在主查詢路徑上成功解析 view 的查詢
遞增一次。在解析出 view 之前就被拒絕的查詢（無 view 命中、無法解析 client IP）
不會遞增，且 zone transfer（AXFR/IXFR）不在範圍內。

!!! warning "`ecs_geo` 的真正語意"
    `ecs_geo="true"` 表示該查詢在解析過程中**有 ECS 衍生的 geo 位址可供 matcher
    評估** —— 並不表示 ECS 決定了最終 view。該 view 仍可能是由 IP/CIDR ACL rule
    選中的，而 ACL rule 永遠評估真實來源 IP。請把這個 label 讀作「ECS geo 參與」，
    而非「ECS 驅動的 view 選擇」。

### ACME HTTP-01 listener 指標

port 80 上的 ACME HTTP-01 listener 是 ShadowDNS 唯一完全暴露於公開網際網路的
HTTP 面。對它的每個請求都會在連線層被中止 —— **不會回任何 HTTP response**（nginx
`return 444` 語意）—— **唯一例外**是針對有效 challenge token 的 `GET`，這類請求會
正常提供服務。中止之前，listener 會將 `shadowdns_doh_acme_dropped_total` 遞增一次，
並以中止的 `reason` 作為 label。

- `reason` 為下列之一：
    - `unknown_path` —— 請求路徑落在 `/.well-known/acme-challenge/` 之外。
    - `unknown_token` —— 路徑在 `/.well-known/acme-challenge/` 之下，但 token 未知
      或為空（含結尾無斜線的 `/.well-known/acme-challenge`）。
    - `bad_method` —— 在符合 challenge 路徑上使用非 `GET` 的 method。
- 三個 `reason` series 在啟動時即預先初始化為 `0`，因此在任何探測到來前就會出現在
  端點上。

可用以下查詢觀測公開 port 80 面被探測的量與型態：

```promql
sum(rate(shadowdns_doh_acme_dropped_total[5m])) by (reason)
```

!!! note "中止不計入 panic"
    listener 透過 `panic(http.ErrAbortHandler)` 中止連線，但這**不會**遞增
    `shadowdns_panics_total` —— 該 counter 只追蹤 DNS `ServeDNS` 路徑上復原的
    panic，而此 listener 不在該路徑上。

## 匯入 Grafana dashboard

本倉庫於
[`grafana/shadowdns-overview.json`](https://github.com/chenwei791129/ShadowDNS/blob/main/grafana/shadowdns-overview.json)
隨附一份 dashboard。它**不會**被打包進 `.deb`；請從倉庫取得。

1. 在 Grafana 進入 **Dashboards → New → Import**。
2. 上傳 `grafana/shadowdns-overview.json`（或貼上其內容）。
3. 出現提示時，為 `DS_PROMETHEUS` 輸入選擇你的 Prometheus 資料來源。
4. 點選 **Import**。

dashboard 在頂端提供 `Job` 與 `Instance` template 變數，讓你可將每個面板限縮到
單一 ShadowDNS 行程，或以聚合方式檢視整個 fleet。

### 面板分組

- **Overview** —— build 資訊、process uptime、總 QPS。
- **Traffic** —— 依 protocol/family/查詢型別的 QPS、依 rcode 的回應，以及
  SERVFAIL/REFUSED/NXDOMAIN 比率（比率在零流量視窗會兜底為 `0`）。
- **Latency** —— 取自 request-duration histogram 的 p50/p90/p99，整體與各 view。
- **ECS & Views** —— 各 view 的選擇速率、ECS-geo 參與比率、依 status/family 的 ECS
  分類，以及 ECS 攜帶率。
- **Rate Limiting** —— 依 category 與 action 的 RRL 決策。
- **Config & Zones** —— reload 嘗試、距上次成功 reload 的時間、GeoIP 資料庫表，
  以及各 view 載入的 zone 數。
- **Runtime** —— process CPU、記憶體（RSS 與 Go heap）、goroutine、file
  descriptor，以及 GC pause 分位數。
- **Panics** —— panic 總數與速率。

!!! note "有流量前面板為空"
    ECS 與各 view 面板在相符流量到來前會保持空白，而以 `process_*` 為基礎的面板
    在非 Linux 主機上也會保持空白。兩者都不是錯誤。

## 用 `go tool pprof` 繪製火焰圖

ShadowDNS 內建 Go 的 `pprof` profiler。當設定
[`--pprof-enable`](../reference/cli.md) 時，profiling 端點會掛在
**與指標端點相同的 HTTP listener**（`--metrics-addr`，預設 `:9153`）底下的
`/debug/pprof/`。它讓你能從運行中的伺服器擷取 CPU 與記憶體 profile，並繪製成
火焰圖找出熱點 —— 不需重啟、不需重新編譯、不需額外的 agent。

!!! warning "僅限信任網路 —— 無認證"
    `/debug/pprof/` handler **沒有任何存取控制**。任何能連到端點的人都能擷取
    profile，並讀取 `cmdline`。啟用 pprof 時，**絕不**將 `--metrics-addr` 暴露到
    不受信任的網路。請透過私有網路或 SSH tunnel（見下）連線，且在非主動 profiling
    時讓 `--pprof-enable` 保持關閉。

### 啟用端點

`--pprof-enable` 需要 `--metrics-addr` 非空，因為 handler 掛在指標伺服器上：

```bash
shadowdns --metrics-addr :9153 --pprof-enable ...
```

確認 profile index 可連線：

```bash
curl -s http://127.0.0.1:9153/debug/pprof/
```

### 在有負載時擷取 CPU profile

profile 只有在伺服器實際處理查詢時才有意義 —— 請在 benchmark 或真實流量尖峰時
擷取，**絕不要**在閒置行程上抓。CPU 幾乎總是查詢吞吐量的瓶頸所在，因此 CPU
profile 是排查 QPS 瓶頸的首選工具。

由於端點僅限信任網路，典型流程會先把指標埠 tunnel 到你的工作站：

```bash
ssh -L 9153:localhost:9153 ns1.example.com
```

接著把 `go tool pprof` 指向被 tunnel 的埠。`-http` 旗標會開啟一個互動式 web UI，
內建火焰圖（開啟 **View → Flame Graph**）：

```bash
# 30-second CPU profile — the primary tool for QPS / throughput bottlenecks
go tool pprof -http=:8080 'http://localhost:9153/debug/pprof/profile?seconds=30'
```

### 如何判讀 CPU 火焰圖

- 每個 frame 的**寬度是累積 CPU 時間** —— frame 越寬，代表它與其被呼叫者消耗的
  CPU 越多。高度只是呼叫深度。
- 由上往下沿著呼叫堆疊看，找出請求熱路徑上**最寬的 leaf frame**：query 解析、
  view 比對（GeoIP/ACL）、zone 查找、alias 改寫、CNAME 收合，以及回應序列化。
- 若 `runtime.mallocgc` / GC 子樹很寬，代表瓶頸是**記憶體配置壓力**而非邏輯 ——
  請改看下方的 allocation profile，找出配置最多的呼叫點。

### 記憶體與其他 profile

同一個 `-http` 火焰圖 UI 適用於所有 profile 類型；只要改 URL 路徑即可：

```bash
# Heap — live (in-use) memory, for leak / footprint investigation
go tool pprof -http=:8080 'http://localhost:9153/debug/pprof/heap'

# Allocs — cumulative allocations since start, for GC-pressure hot spots
go tool pprof -http=:8080 'http://localhost:9153/debug/pprof/allocs'
```

| 端點 | 回答什麼問題 |
|------|--------------|
| `profile?seconds=N` | CPU 時間花在哪（QPS 天花板） |
| `heap` | 依配置點的 live 記憶體（洩漏、佔用量） |
| `allocs` | 累積配置量（GC 壓力） |
| `goroutine` | goroutine 數量與堆疊（洩漏、阻塞） |
| `block` / `mutex` | 鎖競爭 —— **預設關閉**；需先在程式碼設定取樣率才會有資料 |

!!! note "保存 profile 供日後分析"
    若想封存或分享 profile 而非即時開啟，先下載檔案，再讓 UI 指向它：
    ```bash
    curl -o cpu.pprof 'http://localhost:9153/debug/pprof/profile?seconds=30'
    go tool pprof -http=:8080 cpu.pprof
    ```

!!! tip "前置需求與持續 profiling"
    互動式 UI 需要本機 Go 工具鏈（`go tool pprof`）；火焰圖檢視不需 Graphviz 即可
    渲染，而呼叫關係圖（call-graph）檢視則需安裝 Graphviz。`go tool pprof` 擷取的是
    某個時間點的快照 —— 若想要與 dashboard 時間軸對齊的常時火焰圖，可用如 Grafana
    Pyroscope 這類 continuous-profiling 後端來 scrape 這些相同的 `/debug/pprof/`
    端點。
