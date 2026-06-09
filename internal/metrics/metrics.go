// Package metrics provides Prometheus metrics collection for ShadowDNS.
// All metrics are registered on a dedicated registry (not the default global registry)
// to avoid interference with other libraries that may also use prometheus.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

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
	panicsTotal     prometheus.Counter
	rateLimitTotal  *prometheus.CounterVec
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
		Buckets:   prometheus.DefBuckets,
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

	reg.MustRegister(
		requestsTotal,
		responsesTotal,
		requestDuration,
		buildInfo,
		zonesLoaded,
		zonesBackup,
		geoipDBInfo,
		panicsTotal,
		rateLimitTotal,
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

// SetGeoIPInfo sets the shadowdns_geoip_db_info gauge for each database.
// Expected keys are "country" and "asn"; values are Unix epoch timestamps
// from maxminddb.Metadata.BuildEpoch.
func (m *Metrics) SetGeoIPInfo(buildEpochs map[string]uint) {
	for db, epoch := range buildEpochs {
		ts := time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339)
		m.geoipDBInfo.WithLabelValues(db, ts).Set(1)
	}
}

// RecordRateLimit increments the rate-limit decision counter for the given
// response category and action. It satisfies ratelimit.Recorder.
func (m *Metrics) RecordRateLimit(category, action string) {
	m.rateLimitTotal.WithLabelValues(category, action).Inc()
}
