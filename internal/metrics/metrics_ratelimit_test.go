package metrics_test

import (
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
)

// TestRateLimitMetrics verifies that RecordRateLimit increments
// shadowdns_dns_rate_limit_total for the given (category, action) label pair,
// and that un-called pairs produce no counter entry.
func TestRateLimitMetrics(t *testing.T) {
	m := metrics.New()

	// Record a mix of category/action pairs with varying call counts.
	m.RecordRateLimit("nxdomains", "dropped")
	m.RecordRateLimit("nxdomains", "dropped") // called twice
	m.RecordRateLimit("responses", "slipped")
	m.RecordRateLimit("responses", "exempted")
	m.RecordRateLimit("responses", "logonly_would_drop")

	families := gatherMetrics(t, m)

	mf, ok := families["shadowdns_dns_rate_limit_total"]
	if !ok {
		t.Fatal("metric family shadowdns_dns_rate_limit_total not found")
	}

	// Build a lookup table: (category, action) -> counter value.
	type labelKey struct{ category, action string }
	counts := make(map[labelKey]float64)
	for _, metric := range mf.GetMetric() {
		labels := labelMap(metric.GetLabel())
		key := labelKey{labels["category"], labels["action"]}
		counts[key] = metric.GetCounter().GetValue()
	}

	// Assert called pairs have the expected counts.
	cases := []struct {
		category, action string
		want             float64
	}{
		{"nxdomains", "dropped", 2},
		{"responses", "slipped", 1},
		{"responses", "exempted", 1},
		{"responses", "logonly_would_drop", 1},
	}
	for _, tc := range cases {
		got := counts[labelKey{tc.category, tc.action}]
		if got != tc.want {
			t.Errorf("rate_limit_total{category=%q, action=%q} = %v, want %v",
				tc.category, tc.action, got, tc.want)
		}
	}

	// Assert a never-called pair is absent (counter value zero or not present).
	neverCalled := counts[labelKey{"errors", "dropped"}]
	if neverCalled != 0 {
		t.Errorf("rate_limit_total{category=errors, action=dropped} = %v, want 0 (not present)",
			neverCalled)
	}
}
