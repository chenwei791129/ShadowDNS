package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

// gather collects all metric families from the registry via the exported handler,
// using the Gatherer interface exposed by the Metrics struct.
func gatherMetrics(t *testing.T, m *metrics.Metrics) map[string]*dto.MetricFamily {
	t.Helper()
	mfs, err := m.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	result := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		result[mf.GetName()] = mf
	}
	return result
}

// TestNew_RegistersAllMetrics verifies that New() registers all 8 expected metric families.
// Because prometheus Vec collectors are lazy (they only appear in Gather output after at
// least one label set has been initialised), this test seeds each Vec with one observation
// before gathering.  The seed values themselves are not asserted here — dedicated tests
// cover the exact counter/gauge semantics.
func TestNew_RegistersAllMetrics(t *testing.T) {
	m := metrics.New()

	// Seed every Vec so the metric families appear in the Gather output.
	m.RecordRequest("udp", "ipv4", "A", "default")
	m.RecordResponse("NOERROR", "default")
	m.ObserveDuration("default", 0)
	m.SetBuildInfo("v0.0.0", "go0")
	m.SetZoneCounts(map[string]int{"default": 0}, map[string]int{"default": 0})
	m.SetGeoIPInfo(map[string]uint{"country": 1700000000})
	m.RecordPanic()

	families := gatherMetrics(t, m)

	expected := []string{
		"shadowdns_dns_requests_total",
		"shadowdns_dns_responses_total",
		"shadowdns_dns_request_duration_seconds",
		"shadowdns_build_info",
		"shadowdns_zones_loaded",
		"shadowdns_zones_backup",
		"shadowdns_geoip_db_info",
		"shadowdns_panics_total",
	}

	for _, name := range expected {
		if _, ok := families[name]; !ok {
			t.Errorf("expected metric family %q to be registered, but it was not found", name)
		}
	}
}

// TestRecordRequest_IncrementsCounter verifies that RecordRequest increments
// shadowdns_dns_requests_total by 1 for the given label combination.
func TestRecordRequest_IncrementsCounter(t *testing.T) {
	m := metrics.New()

	m.RecordRequest("udp", "ipv4", "A", "default")

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_dns_requests_total"]
	if !ok {
		t.Fatal("metric shadowdns_dns_requests_total not found")
	}

	// Find the metric with matching labels.
	var found bool
	for _, metric := range mf.GetMetric() {
		labels := labelMap(metric.GetLabel())
		if labels["proto"] == "udp" &&
			labels["family"] == "ipv4" &&
			labels["type"] == "A" &&
			labels["view"] == "default" {
			val := metric.GetCounter().GetValue()
			if val != 1.0 {
				t.Errorf("expected counter value 1.0, got %f", val)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("no metric found with labels proto=udp family=ipv4 type=A view=default")
	}
}

// TestRecordPanic_IncrementsCounter verifies that RecordPanic increments
// shadowdns_panics_total by 1.
func TestRecordPanic_IncrementsCounter(t *testing.T) {
	m := metrics.New()

	m.RecordPanic()

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_panics_total"]
	if !ok {
		t.Fatal("metric shadowdns_panics_total not found")
	}

	metrics := mf.GetMetric()
	if len(metrics) == 0 {
		t.Fatal("no metrics in shadowdns_panics_total")
	}
	val := metrics[0].GetCounter().GetValue()
	if val != 1.0 {
		t.Errorf("expected panics_total value 1.0, got %f", val)
	}
}

// TestSetBuildInfo_SetsGauge verifies that SetBuildInfo sets the build_info gauge to 1
// with the correct version and goversion labels.
func TestSetBuildInfo_SetsGauge(t *testing.T) {
	m := metrics.New()

	m.SetBuildInfo("v1.2.3", "go1.25.6")

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_build_info"]
	if !ok {
		t.Fatal("metric shadowdns_build_info not found")
	}

	var found bool
	for _, metric := range mf.GetMetric() {
		labels := labelMap(metric.GetLabel())
		if labels["version"] == "v1.2.3" && labels["goversion"] == "go1.25.6" {
			val := metric.GetGauge().GetValue()
			if val != 1.0 {
				t.Errorf("expected build_info gauge value 1.0, got %f", val)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("no metric found with labels version=v1.2.3 goversion=go1.25.6")
	}
}

// TestSetZoneCounts_SetsGauges verifies that SetZoneCounts sets the per-view
// zone gauges and resets stale views on reload.
func TestSetZoneCounts_SetsGauges(t *testing.T) {
	m := metrics.New()

	m.SetZoneCounts(
		map[string]int{"view-th": 3, "view-jp": 2},
		map[string]int{"view-th": 5},
	)

	families := gatherMetrics(t, m)

	// Check zones_loaded.
	loaded := families["shadowdns_zones_loaded"]
	if loaded == nil {
		t.Fatal("shadowdns_zones_loaded not found")
	}
	for _, metric := range loaded.GetMetric() {
		labels := labelMap(metric.GetLabel())
		val := metric.GetGauge().GetValue()
		switch labels["view"] {
		case "view-th":
			if val != 3 {
				t.Errorf("zones_loaded{view=view-th} = %f, want 3", val)
			}
		case "view-jp":
			if val != 2 {
				t.Errorf("zones_loaded{view=view-jp} = %f, want 2", val)
			}
		}
	}

	// Check zones_backup.
	backup := families["shadowdns_zones_backup"]
	if backup == nil {
		t.Fatal("shadowdns_zones_backup not found")
	}
	for _, metric := range backup.GetMetric() {
		labels := labelMap(metric.GetLabel())
		if labels["view"] == "view-th" {
			val := metric.GetGauge().GetValue()
			if val != 5 {
				t.Errorf("zones_backup{view=view-th} = %f, want 5", val)
			}
		}
	}

	// Simulate reload: view-jp is removed, view-th count changes.
	m.SetZoneCounts(
		map[string]int{"view-th": 4},
		map[string]int{"view-th": 6},
	)

	families = gatherMetrics(t, m)
	loaded = families["shadowdns_zones_loaded"]
	for _, metric := range loaded.GetMetric() {
		labels := labelMap(metric.GetLabel())
		if labels["view"] == "view-jp" {
			t.Error("stale view-jp should have been cleared after reload")
		}
		if labels["view"] == "view-th" {
			val := metric.GetGauge().GetValue()
			if val != 4 {
				t.Errorf("zones_loaded{view=view-th} after reload = %f, want 4", val)
			}
		}
	}
}

// TestSetGeoIPInfo_SetsGauge verifies that SetGeoIPInfo sets the geoip_db_info
// gauge with the correct database name and ISO 8601 build_time label.
func TestSetGeoIPInfo_SetsGauge(t *testing.T) {
	m := metrics.New()

	m.SetGeoIPInfo(map[string]uint{
		"country": 1700000000, // 2023-11-14T22:13:20Z
		"asn":     1700100000, // 2023-11-16T02:00:00Z
	})

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_geoip_db_info"]
	if !ok {
		t.Fatal("shadowdns_geoip_db_info not found")
	}

	found := map[string]bool{}
	for _, metric := range mf.GetMetric() {
		labels := labelMap(metric.GetLabel())
		db := labels["database"]
		bt := labels["build_time"]
		val := metric.GetGauge().GetValue()

		if val != 1.0 {
			t.Errorf("geoip_db_info{database=%s} = %f, want 1", db, val)
		}

		switch db {
		case "country":
			if bt != "2023-11-14T22:13:20Z" {
				t.Errorf("country build_time = %q, want 2023-11-14T22:13:20Z", bt)
			}
			found["country"] = true
		case "asn":
			if bt != "2023-11-16T02:00:00Z" {
				t.Errorf("asn build_time = %q, want 2023-11-16T02:00:00Z", bt)
			}
			found["asn"] = true
		}
	}

	if !found["country"] {
		t.Error("missing geoip_db_info for database=country")
	}
	if !found["asn"] {
		t.Error("missing geoip_db_info for database=asn")
	}
}

// TestHandler_ReturnsHTTP200 verifies that Handler() returns an http.Handler that
// responds with HTTP 200 and a Prometheus text/plain content type.
func TestHandler_ReturnsHTTP200(t *testing.T) {
	m := metrics.New()
	handler := m.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", ct)
	}
}

// TestRecordResponse_IncrementsCounter verifies that RecordResponse increments
// the responses_total counter with the correct rcode and view labels.
func TestRecordResponse_IncrementsCounter(t *testing.T) {
	m := metrics.New()

	m.RecordResponse("NOERROR", "default")

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_dns_responses_total"]
	if !ok {
		t.Fatal("metric shadowdns_dns_responses_total not found")
	}
	if len(mf.GetMetric()) == 0 {
		t.Error("expected at least one metric in shadowdns_dns_responses_total")
	}
}

// TestObserveDuration_RecordsHistogram verifies that ObserveDuration records
// an observation in the request duration histogram.
func TestObserveDuration_RecordsHistogram(t *testing.T) {
	m := metrics.New()

	m.ObserveDuration("default", time.Millisecond)

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_dns_request_duration_seconds"]
	if !ok {
		t.Fatal("metric shadowdns_dns_request_duration_seconds not found")
	}
	if len(mf.GetMetric()) == 0 {
		t.Error("expected at least one metric in shadowdns_dns_request_duration_seconds")
	}
}

// labelMap converts a slice of label pairs to a map for easier lookup.
func labelMap(pairs []*dto.LabelPair) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p.GetName()] = p.GetValue()
	}
	return m
}
