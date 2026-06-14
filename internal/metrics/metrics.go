// Package metrics provides Prometheus metrics collection for ShadowDNS.
// All metrics are registered on a dedicated registry (not the default global registry)
// to avoid interference with other libraries that may also use prometheus.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// dnsDurationBuckets are the request duration histogram boundaries,
// optimised for authoritative DNS (100µs–100ms): queries typically complete
// well below prometheus.DefBuckets' 5ms minimum.
var dnsDurationBuckets = []float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1}

// Metrics holds all Prometheus collectors used by ShadowDNS. The zero value
// is intentionally non-functional; use New() to obtain a valid instance.
type Metrics struct {
	registry        *prometheus.Registry
	requestsTotal   *prometheus.CounterVec
	responsesTotal  *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	buildInfo       *prometheus.GaugeVec
	zonesLoaded     *prometheus.GaugeVec
	prevZoneViews   map[string]bool // tracks views from last SetZoneCounts
	zonesBackup     *prometheus.GaugeVec
	geoipDBInfo     *prometheus.GaugeVec
	prevGeoIPLabels map[string]string // database → build_time from last SetGeoIPInfo
	panicsTotal     prometheus.Counter
	rateLimitTotal  *prometheus.CounterVec
	reloadTotal     *prometheus.CounterVec
	ecsQueriesTotal *prometheus.CounterVec
	viewSelected    *prometheus.CounterVec

	lastReloadSuccessTimestamp prometheus.Gauge
}

// New creates a fresh prometheus.Registry, registers all ShadowDNS collectors
// onto it, and returns a ready-to-use Metrics pointer.
// New() is the only way to obtain a valid Metrics instance.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Subsystem: "dns",
		Name:      "requests_total",
		Help:      "Total number of DNS requests received, partitioned by protocol, address family, query type, and view.",
	}, []string{"proto", "family", "type", "view"})

	responsesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Subsystem: "dns",
		Name:      "responses_total",
		Help:      "Total number of DNS responses sent, partitioned by response code and view.",
	}, []string{"rcode", "view"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "shadowdns",
		Subsystem: "dns",
		Name:      "request_duration_seconds",
		Help:      "Histogram of DNS request processing durations in seconds, partitioned by view.",
		Buckets:   dnsDurationBuckets,
	}, []string{"view"})

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shadowdns",
		Name:      "build_info",
		Help:      "A constant gauge with labels describing the build. Value is always 1.",
	}, []string{"version", "goversion"})

	zonesLoaded := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shadowdns",
		Name:      "zones_loaded",
		Help:      "Number of primary zones currently loaded, partitioned by view.",
	}, []string{"view"})

	zonesBackup := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shadowdns",
		Name:      "zones_backup",
		Help:      "Number of backup-override zones currently loaded, partitioned by view.",
	}, []string{"view"})

	geoipDBInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shadowdns",
		Name:      "geoip_db_info",
		Help:      "A constant gauge with labels describing the loaded GeoIP database. Value is always 1.",
	}, []string{"database", "build_time"})

	panicsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Name:      "panics_total",
		Help:      "Total number of panics recovered by ShadowDNS handlers.",
	})

	rateLimitTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Subsystem: "dns",
		Name:      "rate_limit_total",
		Help:      "RRL decisions partitioned by response category and action.",
	}, []string{"category", "action"})

	reloadTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Name:      "reload_total",
		Help:      "Total number of SIGHUP reload attempts, partitioned by result (success or failure).",
	}, []string{"result"})
	// Pre-initialise both label combinations so the series are present with
	// value 0 from startup; alert expressions then never see an absent metric.
	reloadTotal.WithLabelValues("success")
	reloadTotal.WithLabelValues("failure")

	lastReloadSuccessTimestamp := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "shadowdns",
		Name:      "config_last_reload_success_timestamp_seconds",
		Help:      "Unix timestamp of the most recent configuration load that completed without error (set at startup and on each successful SIGHUP reload).",
	})

	ecsQueriesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Subsystem: "dns",
		Name:      "ecs_queries_total",
		Help:      "ECS option classifications for queries carrying EDNS Client Subnet while ECS handling is enabled, partitioned by address family and classification status.",
	}, []string{"family", "status"})

	viewSelected := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shadowdns",
		Subsystem: "dns",
		Name:      "view_selected_total",
		Help:      "Successful view resolutions on the main query path, partitioned by view and whether an ECS-derived geo address was available to the matcher.",
	}, []string{"view", "ecs_geo"})

	reg.MustRegister(
		// Standard Go runtime and process collectors. The custom registry does
		// not auto-register these (unlike the default registerer), so add them
		// explicitly to expose go_* and process_* on /metrics.
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		requestsTotal,
		responsesTotal,
		requestDuration,
		buildInfo,
		zonesLoaded,
		zonesBackup,
		geoipDBInfo,
		panicsTotal,
		rateLimitTotal,
		reloadTotal,
		lastReloadSuccessTimestamp,
		ecsQueriesTotal,
		viewSelected,
	)

	return &Metrics{
		registry:        reg,
		requestsTotal:   requestsTotal,
		responsesTotal:  responsesTotal,
		requestDuration: requestDuration,
		buildInfo:       buildInfo,
		zonesLoaded:     zonesLoaded,
		zonesBackup:     zonesBackup,
		geoipDBInfo:     geoipDBInfo,
		panicsTotal:     panicsTotal,
		rateLimitTotal:  rateLimitTotal,
		reloadTotal:     reloadTotal,
		ecsQueriesTotal: ecsQueriesTotal,
		viewSelected:    viewSelected,

		lastReloadSuccessTimestamp: lastReloadSuccessTimestamp,
	}
}

// Handler returns an http.Handler that exposes the custom registry's metrics
// in the Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Gather implements prometheus.Gatherer by delegating to the underlying registry.
// It is used by tests (and potentially by the HTTP handler) to collect metric families.
func (m *Metrics) Gather() ([]*dto.MetricFamily, error) {
	return m.registry.Gather()
}

// RecordRequest increments the shadowdns_dns_requests_total counter for the
// given protocol, address family, query type, and view.
func (m *Metrics) RecordRequest(proto, family, qtype, view string) {
	m.requestsTotal.WithLabelValues(proto, family, qtype, view).Inc()
}

// RecordPanic increments the shadowdns_panics_total counter.
// Call this inside a recover() block to track unexpected panics.
func (m *Metrics) RecordPanic() {
	m.panicsTotal.Inc()
}

// SetBuildInfo sets the shadowdns_build_info gauge to 1 for the given
// version and Go runtime version labels. It is safe to call multiple times;
// subsequent calls overwrite the previous label set.
func (m *Metrics) SetBuildInfo(version, goversion string) {
	m.buildInfo.WithLabelValues(version, goversion).Set(1)
}

// RecordResponse increments the shadowdns_dns_responses_total counter
// for the given rcode label and view.
func (m *Metrics) RecordResponse(rcode, view string) {
	m.responsesTotal.WithLabelValues(rcode, view).Inc()
}

// ObserveDuration records a request processing duration in the
// shadowdns_dns_request_duration_seconds histogram for the given view.
func (m *Metrics) ObserveDuration(view string, d time.Duration) {
	m.requestDuration.WithLabelValues(view).Observe(d.Seconds())
}

// SetZoneCounts updates the per-view zone gauges. Views that no longer
// appear in the new counts are deleted so stale labels don't linger.
// Uses differential updates instead of Reset() to avoid a scrape seeing
// empty gauges between reset and re-set.
func (m *Metrics) SetZoneCounts(rootCounts, backupCounts map[string]int) {
	current := make(map[string]bool, len(rootCounts)+len(backupCounts))

	for view, n := range rootCounts {
		m.zonesLoaded.WithLabelValues(view).Set(float64(n))
		current[view] = true
	}
	for view, n := range backupCounts {
		m.zonesBackup.WithLabelValues(view).Set(float64(n))
		current[view] = true
	}

	// Remove views that existed previously but are absent now.
	for view := range m.prevZoneViews {
		if !current[view] {
			m.zonesLoaded.DeleteLabelValues(view)
			m.zonesBackup.DeleteLabelValues(view)
		}
	}
	m.prevZoneViews = current
}

// SetGeoIPInfo declares the COMPLETE desired set of shadowdns_geoip_db_info
// series. Expected keys are "country" and "asn"; values are Unix epoch
// timestamps from maxminddb.Metadata.BuildEpoch. Databases present in the
// previous call but absent from buildEpochs have their series deleted, and a
// stale series carrying the previous build_time for the same database is also
// deleted, so at most one build_time series exists per database label (mirrors
// SetZoneCounts' differential update). Pass an empty map when GeoIP is not
// loaded to remove all series. Safe to call on a nil receiver — the reload
// path runs with metrics disabled too.
func (m *Metrics) SetGeoIPInfo(buildEpochs map[string]uint) {
	if m == nil {
		return
	}
	if m.prevGeoIPLabels == nil {
		m.prevGeoIPLabels = make(map[string]string, len(buildEpochs))
	}
	for db, epoch := range buildEpochs {
		ts := time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339)
		m.geoipDBInfo.WithLabelValues(db, ts).Set(1)
		if prev, ok := m.prevGeoIPLabels[db]; ok && prev != ts {
			m.geoipDBInfo.DeleteLabelValues(db, prev)
		}
		m.prevGeoIPLabels[db] = ts
	}
	// Remove databases that existed previously but are absent now.
	for db, prev := range m.prevGeoIPLabels {
		if _, ok := buildEpochs[db]; !ok {
			m.geoipDBInfo.DeleteLabelValues(db, prev)
			delete(m.prevGeoIPLabels, db)
		}
	}
}

// RecordReload increments the shadowdns_reload_total counter for the given
// result ("success" or "failure"). Safe to call on a nil receiver so the
// reload path needs no metrics-disabled special case.
func (m *Metrics) RecordReload(result string) {
	if m == nil {
		return
	}
	m.reloadTotal.WithLabelValues(result).Inc()
}

// SetLastReloadSuccess sets the last-reload-success timestamp gauge to t.
// Called at startup once the initial configuration load succeeds and after
// each successful SIGHUP reload. Safe to call on a nil receiver.
func (m *Metrics) SetLastReloadSuccess(t time.Time) {
	if m == nil {
		return
	}
	m.lastReloadSuccessTimestamp.Set(float64(t.Unix()))
}

// RecordRateLimit increments the rate-limit decision counter for the given
// response category and action. It satisfies ratelimit.Recorder.
func (m *Metrics) RecordRateLimit(category, action string) {
	m.rateLimitTotal.WithLabelValues(category, action).Inc()
}

// RecordECS increments shadowdns_dns_ecs_queries_total for the given address
// family ("ipv4"/"ipv6"/"unknown") and classification status
// ("valid"/"opt_out"/"malformed"). Safe to call on a nil receiver: its call
// site in ServeDNS sits outside the s.Metrics != nil guard, so metrics being
// disabled must be a no-op rather than a nil dereference.
func (m *Metrics) RecordECS(family, status string) {
	if m == nil {
		return
	}
	m.ecsQueriesTotal.WithLabelValues(family, status).Inc()
}

// RecordViewSelected increments shadowdns_dns_view_selected_total for the
// resolved view, mapping ecsGeo to the "true"/"false" label value. ecsGeo
// true means an ECS-derived geo address was available to the matcher for this
// query; it does not assert the ECS address determined the view. Safe to call
// on a nil receiver for the same reason as RecordECS.
func (m *Metrics) RecordViewSelected(view string, ecsGeo bool) {
	if m == nil {
		return
	}
	ecsGeoLabel := "false"
	if ecsGeo {
		ecsGeoLabel = "true"
	}
	m.viewSelected.WithLabelValues(view, ecsGeoLabel).Inc()
}
