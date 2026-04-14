package metrics

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	dto "github.com/prometheus/client_model/go"
)

// fakeResponseWriter implements dns.ResponseWriter for testing.
type fakeResponseWriter struct {
	msg    *dns.Msg
	remote net.Addr
	local  net.Addr
}

func (f *fakeResponseWriter) LocalAddr() net.Addr  { return f.local }
func (f *fakeResponseWriter) RemoteAddr() net.Addr { return f.remote }
func (f *fakeResponseWriter) WriteMsg(m *dns.Msg) error {
	f.msg = m
	return nil
}
func (f *fakeResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (f *fakeResponseWriter) Close() error              { return nil }
func (f *fakeResponseWriter) TsigStatus() error         { return nil }
func (f *fakeResponseWriter) TsigTimersOnly(bool)       {}
func (f *fakeResponseWriter) Hijack()                   {}

func TestMetricsResponseWriter_WriteMsg_RecordsRcode(t *testing.T) {
	m := New()
	start := time.Now()

	inner := &fakeResponseWriter{
		remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
	}

	w := NewResponseWriter(inner, m, "view-th", start)

	msg := new(dns.Msg)
	msg.Rcode = dns.RcodeSuccess
	if err := w.WriteMsg(msg); err != nil {
		t.Fatalf("WriteMsg failed: %v", err)
	}

	// Verify the response counter was incremented.
	families := gatherFamilies(t, m)
	mf, ok := families["shadowdns_dns_responses_total"]
	if !ok {
		t.Fatal("metric shadowdns_dns_responses_total not found")
	}

	var found bool
	for _, metric := range mf.GetMetric() {
		labels := labelMapFromPairs(metric.GetLabel())
		if labels["rcode"] == "NOERROR" && labels["view"] == "view-th" {
			val := metric.GetCounter().GetValue()
			if val != 1.0 {
				t.Errorf("expected counter value 1.0, got %f", val)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("no metric found with labels rcode=NOERROR view=view-th")
	}
}

func TestMetricsResponseWriter_WriteMsg_RecordsDuration(t *testing.T) {
	m := New()
	start := time.Now().Add(-10 * time.Millisecond) // simulate 10ms of processing

	inner := &fakeResponseWriter{
		remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
	}

	w := NewResponseWriter(inner, m, "view-th", start)

	msg := new(dns.Msg)
	msg.Rcode = dns.RcodeSuccess
	if err := w.WriteMsg(msg); err != nil {
		t.Fatalf("WriteMsg failed: %v", err)
	}

	// Verify the duration histogram was observed.
	families := gatherFamilies(t, m)
	mf, ok := families["shadowdns_dns_request_duration_seconds"]
	if !ok {
		t.Fatal("metric shadowdns_dns_request_duration_seconds not found")
	}

	var found bool
	for _, metric := range mf.GetMetric() {
		labels := labelMapFromPairs(metric.GetLabel())
		if labels["view"] == "view-th" {
			count := metric.GetHistogram().GetSampleCount()
			if count != 1 {
				t.Errorf("expected sample count 1, got %d", count)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("no histogram found with label view=view-th")
	}
}

func TestMetricsResponseWriter_WriteMsg_DelegatesToInner(t *testing.T) {
	m := New()
	inner := &fakeResponseWriter{
		remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
	}

	w := NewResponseWriter(inner, m, "default", time.Now())

	msg := new(dns.Msg)
	msg.Rcode = dns.RcodeRefused
	if err := w.WriteMsg(msg); err != nil {
		t.Fatalf("WriteMsg failed: %v", err)
	}

	// Verify the inner writer received the message.
	if inner.msg == nil {
		t.Fatal("inner writer did not receive the message")
	}
	if inner.msg.Rcode != dns.RcodeRefused {
		t.Errorf("expected inner msg Rcode %d, got %d", dns.RcodeRefused, inner.msg.Rcode)
	}
}

func TestMetricsResponseWriter_WriteMsg_MapsRcodeNames(t *testing.T) {
	tests := []struct {
		rcode    int
		expected string
	}{
		{dns.RcodeSuccess, "NOERROR"},
		{dns.RcodeNameError, "NXDOMAIN"},
		{dns.RcodeServerFailure, "SERVFAIL"},
		{dns.RcodeRefused, "REFUSED"},
		{dns.RcodeFormatError, "FORMERR"},
		{dns.RcodeNotImplemented, "NOTIMP"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			m := New()
			inner := &fakeResponseWriter{
				remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			}
			w := NewResponseWriter(inner, m, "default", time.Now())

			msg := new(dns.Msg)
			msg.Rcode = tt.rcode
			if err := w.WriteMsg(msg); err != nil {
				t.Fatalf("WriteMsg failed: %v", err)
			}

			families := gatherFamilies(t, m)
			mf := families["shadowdns_dns_responses_total"]
			var found bool
			for _, metric := range mf.GetMetric() {
				labels := labelMapFromPairs(metric.GetLabel())
				if labels["rcode"] == tt.expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected rcode label %q not found", tt.expected)
			}
		})
	}
}

// test helpers

func gatherFamilies(t *testing.T, m *Metrics) map[string]*dto.MetricFamily {
	t.Helper()
	mfs, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	result := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		result[mf.GetName()] = mf
	}
	return result
}

func labelMapFromPairs(pairs []*dto.LabelPair) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p.GetName()] = p.GetValue()
	}
	return m
}
