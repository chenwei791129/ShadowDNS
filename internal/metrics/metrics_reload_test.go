package metrics_test

import (
	"testing"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

// findCounterValue returns the counter value for the series in mf whose labels
// match want exactly, and reports whether such a series exists.
func findCounterValue(mf *dto.MetricFamily, want map[string]string) (float64, bool) {
	for _, metric := range mf.GetMetric() {
		labels := labelMap(metric.GetLabel())
		match := true
		for k, v := range want {
			if labels[k] != v {
				match = false
				break
			}
		}
		if match {
			return metric.GetCounter().GetValue(), true
		}
	}
	return 0, false
}

// TestReloadMetrics covers the "Reload outcome is tracked by a total counter"
// and "Last-reload-success timestamp is exposed as a gauge" requirements:
// pre-initialised series at registration, increment/update semantics, and
// nil-receiver safety.
func TestReloadMetrics(t *testing.T) {
	t.Run("PreInitialisedAtZero", func(t *testing.T) {
		m := metrics.New()

		families := gatherMetrics(t, m)
		mf, ok := families["shadowdns_reload_total"]
		if !ok {
			t.Fatal("shadowdns_reload_total not found before any observation")
		}
		for _, result := range []string{"success", "failure"} {
			val, found := findCounterValue(mf, map[string]string{"result": result})
			if !found {
				t.Errorf("series shadowdns_reload_total{result=%q} missing at startup", result)
				continue
			}
			if val != 0 {
				t.Errorf("shadowdns_reload_total{result=%q} = %f, want 0", result, val)
			}
		}

		gf, ok := families["shadowdns_config_last_reload_success_timestamp_seconds"]
		if !ok {
			t.Fatal("shadowdns_config_last_reload_success_timestamp_seconds not found at startup")
		}
		if got := gf.GetMetric()[0].GetGauge().GetValue(); got != 0 {
			t.Errorf("last-reload-success gauge = %f at registration, want 0", got)
		}
	})

	t.Run("RecordReloadIncrements", func(t *testing.T) {
		m := metrics.New()

		m.RecordReload("success")
		m.RecordReload("success")
		m.RecordReload("failure")

		families := gatherMetrics(t, m)
		mf := families["shadowdns_reload_total"]
		if val, _ := findCounterValue(mf, map[string]string{"result": "success"}); val != 2 {
			t.Errorf("success counter = %f, want 2", val)
		}
		if val, _ := findCounterValue(mf, map[string]string{"result": "failure"}); val != 1 {
			t.Errorf("failure counter = %f, want 1", val)
		}
	})

	t.Run("SetLastReloadSuccessUpdatesGauge", func(t *testing.T) {
		m := metrics.New()

		ts := time.Unix(1750000000, 0)
		m.SetLastReloadSuccess(ts)

		families := gatherMetrics(t, m)
		gf := families["shadowdns_config_last_reload_success_timestamp_seconds"]
		if got := gf.GetMetric()[0].GetGauge().GetValue(); got != 1750000000 {
			t.Errorf("gauge = %f, want 1750000000", got)
		}
	})

	t.Run("NilReceiverNoPanic", func(t *testing.T) {
		var m *metrics.Metrics
		m.RecordReload("success")
		m.RecordReload("failure")
		m.SetLastReloadSuccess(time.Now())
	})
}

// TestSetGeoIPInfo_DifferentialUpdate verifies that a second SetGeoIPInfo call
// with a different build epoch removes the stale build_time series, so at most
// one build_time series exists per database label.
func TestSetGeoIPInfo_DifferentialUpdate(t *testing.T) {
	m := metrics.New()

	m.SetGeoIPInfo(map[string]uint{
		"country": 1700000000, // 2023-11-14T22:13:20Z
		"asn":     1700100000, // 2023-11-16T02:00:00Z
	})
	m.SetGeoIPInfo(map[string]uint{
		"country": 1710000000, // 2024-03-09T16:00:00Z
		"asn":     1710100000, // 2024-03-10T19:46:40Z
	})

	families := gatherMetrics(t, m)
	mf, ok := families["shadowdns_geoip_db_info"]
	if !ok {
		t.Fatal("shadowdns_geoip_db_info not found")
	}

	perDB := map[string][]string{}
	for _, metric := range mf.GetMetric() {
		labels := labelMap(metric.GetLabel())
		perDB[labels["database"]] = append(perDB[labels["database"]], labels["build_time"])
	}

	wantBuildTime := map[string]string{
		"country": "2024-03-09T16:00:00Z",
		"asn":     "2024-03-10T19:46:40Z",
	}
	for db, want := range wantBuildTime {
		series := perDB[db]
		if len(series) != 1 {
			t.Errorf("database=%q has %d build_time series %v, want exactly 1", db, len(series), series)
			continue
		}
		if series[0] != want {
			t.Errorf("database=%q build_time = %q, want %q", db, series[0], want)
		}
	}
}

// TestSetGeoIPInfo_NilReceiver verifies that the reload path can call
// SetGeoIPInfo when metrics are disabled (srv.Metrics == nil) without panic.
func TestSetGeoIPInfo_NilReceiver(t *testing.T) {
	var m *metrics.Metrics
	m.SetGeoIPInfo(map[string]uint{"country": 1700000000, "asn": 1700100000})
}
