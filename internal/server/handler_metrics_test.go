package server

import (
	"net"
	"testing"

	"github.com/miekg/dns"
	dto "github.com/prometheus/client_model/go"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// serveWithMetrics runs req through ServeDNS on a metrics-enabled server with
// the given ECS flag state and state snapshot, returning the response and the
// Metrics instance so the caller can assert counter values.
func serveWithMetrics(t *testing.T, ecsEnabled bool, st ServerState, req *dns.Msg) (*dns.Msg, *metrics.Metrics) {
	t.Helper()
	m := metrics.New()
	srv := NewServer(st, nil)
	srv.Metrics = m
	srv.ECSEnabled = ecsEnabled
	w := &recordingWriter{}
	srv.ServeDNS(w, req)
	resp := new(dns.Msg)
	if err := resp.Unpack(w.Packed); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	return resp, m
}

// gatherFamily returns the gathered metric family named `name`, or nil when no
// such family is present. Unlike gatherMetricFamilyWithRetry it does not retry
// or fail on absence: ServeDNS runs synchronously before these assertions, and
// absence is itself a valid outcome (e.g. a counter never incremented).
func gatherFamily(t *testing.T, m *metrics.Metrics, name string) *dto.MetricFamily {
	t.Helper()
	mfs, err := m.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

// metricCounter returns the value of the series in family `name` whose labels
// exactly match `want`, and whether such a series exists. A Vec series only
// appears after it is incremented, so absence means the counter was never
// touched for that label set.
func metricCounter(t *testing.T, m *metrics.Metrics, name string, want map[string]string) (float64, bool) {
	t.Helper()
	mf := gatherFamily(t, m, name)
	if mf == nil {
		return 0, false
	}
	for _, metric := range mf.GetMetric() {
		labels := map[string]string{}
		for _, p := range metric.GetLabel() {
			labels[p.GetName()] = p.GetValue()
		}
		match := len(labels) == len(want)
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

// seriesPresent reports whether any series exists in family `name`.
func seriesPresent(t *testing.T, m *metrics.Metrics, name string) bool {
	t.Helper()
	mf := gatherFamily(t, m, name)
	return mf != nil && len(mf.GetMetric()) > 0
}

// TestServeDNS_ECSClassificationMetrics covers spec "Count ECS option
// classifications": the valid / opt_out / malformed paths each increment
// shadowdns_dns_ecs_queries_total under the matching status (family derived
// from the ECS option's FAMILY field), and malformed still replies FORMERR.
func TestServeDNS_ECSClassificationMetrics(t *testing.T) {
	cases := []struct {
		name       string
		build      func() *dns.Msg
		wantStatus string
		wantFamily string
		wantRcode  int
	}{
		{
			name:       "valid IPv4",
			build:      func() *dns.Msg { return ecsQuery(1, 24, 0, net.ParseIP("203.0.113.0")) },
			wantStatus: "valid",
			wantFamily: "ipv4",
			wantRcode:  dns.RcodeSuccess,
		},
		{
			name:       "opt-out FAMILY 1",
			build:      func() *dns.Msg { return ecsQuery(1, 0, 0, nil) },
			wantStatus: "opt_out",
			wantFamily: "ipv4",
			wantRcode:  dns.RcodeSuccess,
		},
		{
			name:       "malformed (non-zero query scope)",
			build:      func() *dns.Msg { return ecsQuery(1, 24, 24, net.ParseIP("203.0.113.0")) },
			wantStatus: "malformed",
			wantFamily: "ipv4",
			wantRcode:  dns.RcodeFormatError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, m := serveWithMetrics(t, true, optTestState(), tc.build())
			if resp.Rcode != tc.wantRcode {
				t.Fatalf("rcode = %s, want %s",
					dns.RcodeToString[resp.Rcode], dns.RcodeToString[tc.wantRcode])
			}
			val, ok := metricCounter(t, m, "shadowdns_dns_ecs_queries_total",
				map[string]string{"family": tc.wantFamily, "status": tc.wantStatus})
			if !ok {
				t.Fatalf("no ecs_queries_total series with family=%s status=%s", tc.wantFamily, tc.wantStatus)
			}
			if val != 1.0 {
				t.Errorf("ecs_queries_total{family=%s,status=%s} = %f, want 1", tc.wantFamily, tc.wantStatus, val)
			}
		})
	}
}

// TestServeDNS_ECSDisabledNoClassificationMetric covers spec "No increment when
// ECS handling is disabled": with ECS off, an ECS-bearing query produces no
// shadowdns_dns_ecs_queries_total series at all.
func TestServeDNS_ECSDisabledNoClassificationMetric(t *testing.T) {
	_, m := serveWithMetrics(t, false, optTestState(), ecsQuery(1, 24, 0, net.ParseIP("203.0.113.0")))
	if seriesPresent(t, m, "shadowdns_dns_ecs_queries_total") {
		t.Error("ecs_queries_total has series with ECS disabled, want none")
	}
}

// TestServeDNS_ViewSelectedECSGeoTrue covers spec "View resolved using an
// ECS-derived geo address": a valid ECS option makes an ECS-derived geo
// address available to the matcher, so the counter carries ecs_geo="true".
func TestServeDNS_ViewSelectedECSGeoTrue(t *testing.T) {
	_, m := serveWithMetrics(t, true, optTestState(), ecsQuery(1, 24, 0, net.ParseIP("203.0.113.0")))
	val, ok := metricCounter(t, m, "shadowdns_dns_view_selected_total",
		map[string]string{"view": "default", "ecs_geo": "true"})
	if !ok {
		t.Fatal("no view_selected_total series with view=default ecs_geo=true")
	}
	if val != 1.0 {
		t.Errorf("view_selected_total{view=default,ecs_geo=true} = %f, want 1", val)
	}
}

// TestServeDNS_ViewSelectedECSGeoFalse covers spec "View resolved from the
// source IP only": a query without ECS resolves a view with ecs_geo="false".
func TestServeDNS_ViewSelectedECSGeoFalse(t *testing.T) {
	req := ednsQuery("www.root.com.", dns.TypeA, 1232)
	_, m := serveWithMetrics(t, true, optTestState(), req)
	val, ok := metricCounter(t, m, "shadowdns_dns_view_selected_total",
		map[string]string{"view": "default", "ecs_geo": "false"})
	if !ok {
		t.Fatal("no view_selected_total series with view=default ecs_geo=false")
	}
	if val != 1.0 {
		t.Errorf("view_selected_total{view=default,ecs_geo=false} = %f, want 1", val)
	}
}

// TestServeDNS_ViewSelectedNotIncrementedOnRefused covers spec "No view matched
// does not increment": a CIDR-only view the source never matches is REFUSED,
// and shadowdns_dns_view_selected_total stays empty.
func TestServeDNS_ViewSelectedNotIncrementedOnRefused(t *testing.T) {
	// 127.0.0.2 (recordingWriter source) is outside 192.0.2.0/24 → no view → REFUSED.
	st := ServerState{
		Matcher:     makeMatcher("192.0.2.0/24", "view-internal"),
		ZoneOrigins: map[string][]string{"view-internal": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"view-internal": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "192.0.2.1", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}
	resp, m := serveWithMetrics(t, true, st, ednsQuery("www.root.com.", dns.TypeA, 1232))
	if resp.Rcode != dns.RcodeRefused {
		t.Fatalf("rcode = %s, want REFUSED", dns.RcodeToString[resp.Rcode])
	}
	if seriesPresent(t, m, "shadowdns_dns_view_selected_total") {
		t.Error("view_selected_total incremented for a refused (no-view) query, want none")
	}
}
