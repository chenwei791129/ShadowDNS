package transfer

import (
	"net"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// fakeTCPResponseWriter is a TCP-style dns.ResponseWriter that captures the
// reply message instead of writing it to a socket. LocalAddr returns a
// *net.TCPAddr so dnsutil.IsUDP reports false (i.e. the request looks like it
// arrived over TCP, the only transport AXFR is served on).
type fakeTCPResponseWriter struct {
	msg     *dns.Msg
	written bool
}

func (w *fakeTCPResponseWriter) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 53}
}

func (w *fakeTCPResponseWriter) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.2"), Port: 5353}
}

func (w *fakeTCPResponseWriter) WriteMsg(m *dns.Msg) error {
	w.msg = m
	w.written = true
	return nil
}

func (w *fakeTCPResponseWriter) Write(b []byte) (int, error) {
	w.written = true
	return len(b), nil
}

func (w *fakeTCPResponseWriter) Close() error        { return nil }
func (w *fakeTCPResponseWriter) TsigStatus() error   { return nil }
func (w *fakeTCPResponseWriter) TsigTimersOnly(bool) {}
func (w *fakeTCPResponseWriter) Hijack()             {}

// TestHandleAXFR_NilSOA_Refused drives HandleAXFR with a zone whose apex SOA is
// nil (records present, no SOA) over a TCP-style writer and asserts a REFUSED
// reply with no panic / process abort.
func TestHandleAXFR_NilSOA_Refused(t *testing.T) {
	origin := "example.com."

	// Zone with a record but no SOA — z.SOA stays nil.
	z := &zone.Zone{Origin: origin}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "www." + origin, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("192.0.2.10"),
	})
	if z.SOA != nil {
		t.Fatalf("test setup: expected nil SOA, got %v", z.SOA)
	}

	req := new(dns.Msg)
	req.SetAxfr(origin)

	w := &fakeTCPResponseWriter{}
	HandleAXFR(w, req, z, zap.NewNop())

	if !w.written {
		t.Fatal("HandleAXFR wrote no reply for a SOA-less zone")
	}
	if w.msg == nil {
		t.Fatal("HandleAXFR produced no message")
	}
	if w.msg.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for SOA-less zone, got %s", dns.RcodeToString[w.msg.Rcode])
	}
}

// TestHandleAliasAXFR_NilRootSOA_Refused drives HandleAliasAXFR with a backing
// root zone whose SOA is nil and asserts a REFUSED reply with no panic.
func TestHandleAliasAXFR_NilRootSOA_Refused(t *testing.T) {
	rootOrigin := "root.example.org."
	backupOrigin := "backup.example.org."

	// Root zone with a record but no SOA — rootZone.SOA stays nil.
	rootZone := &zone.Zone{Origin: rootOrigin}
	rootZone.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "www." + rootOrigin, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("192.0.2.20"),
	})
	if rootZone.SOA != nil {
		t.Fatalf("test setup: expected nil root SOA, got %v", rootZone.SOA)
	}

	req := new(dns.Msg)
	req.SetAxfr(backupOrigin)

	w := &fakeTCPResponseWriter{}
	HandleAliasAXFR(w, req, backupOrigin, backupOrigin, rootZone, nil, false, zap.NewNop())

	if !w.written {
		t.Fatal("HandleAliasAXFR wrote no reply for a SOA-less backing root zone")
	}
	if w.msg == nil {
		t.Fatal("HandleAliasAXFR produced no message")
	}
	if w.msg.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for SOA-less backing root zone, got %s", dns.RcodeToString[w.msg.Rcode])
	}
}
