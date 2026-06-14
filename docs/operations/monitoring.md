# Monitoring with Prometheus and Grafana

ShadowDNS exposes Prometheus metrics over HTTP and ships a ready-to-import
Grafana dashboard. This page covers the metrics endpoint, a Prometheus scrape
configuration, the metric families you can graph, and how to load the bundled
dashboard.

## The metrics endpoint

ShadowDNS serves metrics on a dedicated HTTP listener controlled by
[`--metrics-addr`](../reference/cli.md). The default is `:9153`; set it to an
empty string to disable the endpoint (no metrics — including the Go runtime and
process collectors — are registered in that case).

```bash
curl -s http://127.0.0.1:9153/metrics | head
```

Metrics live on their own registry, so the response contains the `shadowdns_*`
families plus the standard `go_*` (Go runtime) and `process_*` families.

!!! note "`process_*` is Linux-only"
    The `process_*` family (resident memory, CPU seconds, file descriptors,
    process start time) is produced by the process collector, which only reports
    data on Linux. On other platforms those series are simply absent — this is
    expected, not an error. The `go_*` family is present on every platform.

## Prometheus scrape configuration

Add a scrape job that points at the metrics endpoint. The `9153` port is the
ShadowDNS default.

```yaml
scrape_configs:
  - job_name: shadowdns
    static_configs:
      - targets:
          - "ns1.example.com:9153"
          - "ns2.example.com:9153"
```

Confirm the target is `up` on the Prometheus **Status → Targets** page before
moving on to Grafana.

## Metric reference

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `shadowdns_build_info` | gauge | `version`, `goversion` | Always 1; build identification |
| `shadowdns_dns_requests_total` | counter | `proto`, `family`, `type`, `view` | DNS requests received |
| `shadowdns_dns_responses_total` | counter | `rcode`, `view` | DNS responses sent |
| `shadowdns_dns_request_duration_seconds` | histogram | `view` | Request processing latency (buckets 100µs–100ms) |
| `shadowdns_dns_ecs_queries_total` | counter | `family`, `status` | ECS option classifications (only with `--ecs-enable`) |
| `shadowdns_dns_view_selected_total` | counter | `view`, `ecs_geo` | Successful view resolutions on the main query path |
| `shadowdns_dns_rate_limit_total` | counter | `category`, `action` | RRL decisions |
| `shadowdns_zones_loaded` | gauge | `view` | Root zones loaded per view |
| `shadowdns_zones_backup` | gauge | `view` | Backup-override zones loaded per view |
| `shadowdns_geoip_db_info` | gauge | `database`, `build_time` | Loaded GeoIP database build time |
| `shadowdns_reload_total` | counter | `result` | SIGHUP reload attempts |
| `shadowdns_config_last_reload_success_timestamp_seconds` | gauge | — | Unix time of the last successful load |
| `shadowdns_panics_total` | counter | — | Panics recovered by handlers |
| `go_*` | various | — | Go runtime (goroutines, heap, GC) |
| `process_*` | various | — | Process resource usage (Linux-only) |

### ECS classification metrics

`shadowdns_dns_ecs_queries_total` is incremented once per query that carries an
EDNS Client Subnet option **while ECS handling is enabled** (see the
[ECS guide](../guides/ecs.md)). When `--ecs-enable` is off, or a query carries
no ECS option, this counter is not touched.

- `status` is one of `valid`, `opt_out`, or `malformed`, matching the option's
  classification. A malformed option is still answered with FORMERR exactly as
  before — recording the metric does not change the response.
- `family` is derived from the ECS option's own address-family field: `ipv4`
  for family 1, `ipv6` for family 2, `unknown` otherwise.

The ECS carry rate is `sum(rate(shadowdns_dns_ecs_queries_total[5m])) /
sum(rate(shadowdns_dns_requests_total[5m]))`.

### View selection metrics

`shadowdns_dns_view_selected_total` is incremented once for each query whose
view resolves on the main query path. Queries refused before a view is resolved
(no view matched, unparseable client IP) do not increment it, and zone transfers
(AXFR/IXFR) are out of scope.

!!! warning "What `ecs_geo` means"
    `ecs_geo="true"` means **an ECS-derived geo address was available to the
    matcher** for that query — not that ECS decided the final view. The view may
    still have been chosen by an IP/CIDR ACL rule, which always evaluates the
    real source IP. Read this label as "ECS geo participation", not "ECS-driven
    view selection".

## Importing the Grafana dashboard

The repository ships a dashboard at
[`grafana/shadowdns-overview.json`](https://github.com/chenwei791129/ShadowDNS/blob/main/grafana/shadowdns-overview.json).
It is not packaged into the `.deb`; fetch it from the repository.

1. In Grafana, go to **Dashboards → New → Import**.
2. Upload `grafana/shadowdns-overview.json` (or paste its contents).
3. When prompted, select your Prometheus data source for the `DS_PROMETHEUS`
   input.
4. Click **Import**.

The dashboard provides `Job` and `Instance` template variables at the top so you
can scope every panel to a single ShadowDNS process or view the fleet in
aggregate.

### Panel groups

- **Overview** — build info, process uptime, total QPS.
- **Traffic** — QPS by protocol/family/query type, responses by rcode, and
  SERVFAIL/REFUSED/NXDOMAIN ratios (ratios fall back to `0` on zero-traffic
  windows).
- **Latency** — p50/p90/p99 from the request-duration histogram, overall and
  per view.
- **ECS & Views** — per-view selection rate, ECS-geo participation ratio, ECS
  classification by status/family, and the ECS carry rate.
- **Rate Limiting** — RRL decisions by category and action.
- **Config & Zones** — reload attempts, time since last successful reload, the
  GeoIP database table, and zones loaded per view.
- **Runtime** — process CPU, memory (RSS and Go heap), goroutines, file
  descriptors, and GC pause quantiles.
- **Panics** — panic total and rate.

!!! note "Empty panels before traffic"
    The ECS and per-view panels stay empty until matching traffic arrives, and
    the `process_*`-based panels stay empty on non-Linux hosts. Neither is an
    error.
