package metrics_test

import (
	"testing"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
)

// TestDoHCertMetrics covers the DoH certificate renewal counter and expiry
// gauge: pre-initialised series, failure/success increments, expiry gauge, and
// nil-receiver safety.
func TestDoHCertMetrics(t *testing.T) {
	t.Run("RenewalFailureRecorded", func(t *testing.T) {
		m := metrics.New()
		m.RecordDoHCertRenewal("failure")

		mf := gatherMetrics(t, m)["shadowdns_doh_cert_renewals_total"]
		if mf == nil {
			t.Fatal("shadowdns_doh_cert_renewals_total not found")
		}
		if v, ok := findCounterValue(mf, map[string]string{"result": "failure"}); !ok || v != 1 {
			t.Errorf("renewals_total{result=failure} = %v (found=%v), want 1", v, ok)
		}
		// success series exists (pre-initialised) and stays 0.
		if v, ok := findCounterValue(mf, map[string]string{"result": "success"}); !ok || v != 0 {
			t.Errorf("renewals_total{result=success} = %v (found=%v), want 0", v, ok)
		}
	})

	t.Run("ExpiryGaugeSet", func(t *testing.T) {
		m := metrics.New()
		exp := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)
		m.SetDoHCertNotAfter(exp)

		gf := gatherMetrics(t, m)["shadowdns_doh_cert_not_after_timestamp_seconds"]
		if gf == nil {
			t.Fatal("shadowdns_doh_cert_not_after_timestamp_seconds not found")
		}
		if got := gf.GetMetric()[0].GetGauge().GetValue(); got != float64(exp.Unix()) {
			t.Errorf("cert_not_after gauge = %f, want %d", got, exp.Unix())
		}
	})

	t.Run("NilReceiverSafe", func(t *testing.T) {
		var m *metrics.Metrics
		m.RecordDoHCertRenewal("failure") // must not panic
		m.SetDoHCertNotAfter(time.Now())  // must not panic
	})
}

// TestDoHACMEDroppedMetric covers the ACME HTTP-01 listener drop counter:
// the three bounded reason series are pre-initialised at zero, each reason is
// counted independently, and the nil receiver is a safe no-op.
func TestDoHACMEDroppedMetric(t *testing.T) {
	reasons := []string{"unknown_path", "unknown_token", "bad_method"}

	t.Run("PreInitialisedAtZero", func(t *testing.T) {
		m := metrics.New()

		mf := gatherMetrics(t, m)["shadowdns_doh_acme_dropped_total"]
		if mf == nil {
			t.Fatal("shadowdns_doh_acme_dropped_total not found at registration")
		}
		for _, reason := range reasons {
			if v, ok := findCounterValue(mf, map[string]string{"reason": reason}); !ok || v != 0 {
				t.Errorf("dropped_total{reason=%q} = %v (found=%v), want 0", reason, v, ok)
			}
		}
	})

	t.Run("ReasonsCountedIndependently", func(t *testing.T) {
		m := metrics.New()
		m.RecordDoHACMEDropped("unknown_path")
		m.RecordDoHACMEDropped("unknown_path")
		m.RecordDoHACMEDropped("unknown_token")

		mf := gatherMetrics(t, m)["shadowdns_doh_acme_dropped_total"]
		if v, _ := findCounterValue(mf, map[string]string{"reason": "unknown_path"}); v != 2 {
			t.Errorf("dropped_total{reason=unknown_path} = %f, want 2", v)
		}
		if v, _ := findCounterValue(mf, map[string]string{"reason": "unknown_token"}); v != 1 {
			t.Errorf("dropped_total{reason=unknown_token} = %f, want 1", v)
		}
		if v, _ := findCounterValue(mf, map[string]string{"reason": "bad_method"}); v != 0 {
			t.Errorf("dropped_total{reason=bad_method} = %f, want 0", v)
		}
	})

	t.Run("NilReceiverSafe", func(t *testing.T) {
		var m *metrics.Metrics
		m.RecordDoHACMEDropped("unknown_path") // must not panic
	})
}
