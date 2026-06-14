# 以 Prometheus 與 Grafana 監控

ShadowDNS 透過 HTTP 暴露 Prometheus 指標，並隨附一份可直接 import 的 Grafana
dashboard。本頁說明指標端點、Prometheus scrape 設定、可繪製的指標家族，以及如何
載入隨附的 dashboard。

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
