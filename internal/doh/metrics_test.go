package doh

import (
	"net/http"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
)

// TestHandler_DoHQueryLabeledDistinctly asserts a DoH-served query increments
// shadowdns_dns_requests_total under proto="doh", distinct from udp/tcp.
func TestHandler_DoHQueryLabeledDistinctly(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	srv.Metrics = metrics.New()
	h := newDoHHandler(t, srv)

	rec := doPOST(t, h, queryMsg("www.example.com."), "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	got := protoCounts(t, srv.Metrics)
	if got["doh"] < 1 {
		t.Errorf("proto=doh count = %v, want >= 1", got["doh"])
	}
	if got["udp"] != 0 || got["tcp"] != 0 {
		t.Errorf("DoH query must not increment udp/tcp: udp=%v tcp=%v", got["udp"], got["tcp"])
	}
}

// protoCounts returns shadowdns_dns_requests_total summed by proto label.
func protoCounts(t *testing.T, m *metrics.Metrics) map[string]float64 {
	t.Helper()
	mfs, err := m.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() != "shadowdns_dns_requests_total" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			for _, l := range metric.GetLabel() {
				if l.GetName() == "proto" {
					out[l.GetValue()] += metric.GetCounter().GetValue()
				}
			}
		}
	}
	return out
}
