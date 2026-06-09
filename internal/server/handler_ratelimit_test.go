package server

import (
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/ratelimit"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// countingWriter is a dns.ResponseWriter stub that counts delivered responses
// (calls to the underlying WriteMsg) and reports a configurable transport. It
// is reused across ServeDNS calls so the count reflects how many responses
// actually reached the wire after rate limiting.
type countingWriter struct {
	udp        bool
	writeCount int
	last       *dns.Msg
}

func (w *countingWriter) addr() net.Addr {
	ip := net.ParseIP("192.0.2.10")
	if w.udp {
		return &net.UDPAddr{IP: ip, Port: 40000}
	}
	return &net.TCPAddr{IP: ip, Port: 40000}
}

func (w *countingWriter) LocalAddr() net.Addr {
	ip := net.IPv4(127, 0, 0, 1)
	if w.udp {
		return &net.UDPAddr{IP: ip, Port: 53}
	}
	return &net.TCPAddr{IP: ip, Port: 53}
}
func (w *countingWriter) RemoteAddr() net.Addr { return w.addr() }
func (w *countingWriter) WriteMsg(m *dns.Msg) error {
	w.writeCount++
	w.last = m
	return nil
}
func (w *countingWriter) Write(b []byte) (int, error) { w.writeCount++; return len(b), nil }
func (w *countingWriter) Close() error                { return nil }
func (w *countingWriter) TsigStatus() error           { return errors.New("not signed") }
func (w *countingWriter) TsigTimersOnly(bool)         {}
func (w *countingWriter) Hijack()                     {}

// newRateLimitServer builds a single-view server serving www.example.com and
// attaches the given limiter (nil ⇒ unconfigured).
func newRateLimitServer(t *testing.T, l *ratelimit.Limiter) *Server {
	t.Helper()
	rootZ := buildRootZone("example.com.", makeARecord("www.example.com.", "192.0.2.1", 300))
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"example.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"example.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, silentLogger())
	srv.RateLimiter = l
	return srv
}

func mustLimiter(t *testing.T, cfg *config.RateLimitConfig) *ratelimit.Limiter {
	t.Helper()
	l, err := ratelimit.NewLimiter(cfg)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return l
}

func aQuery() *dns.Msg {
	return buildServeDNSRequest("www.example.com.", dns.TypeA, dns.ClassINET, dns.OpcodeQuery, 1)
}

func TestServerRateLimitWiring(t *testing.T) {
	t.Run("UDP flood is rate limited", func(t *testing.T) {
		// cap = 1×1 = 1; slip=0 drops all over-limit responses.
		l := mustLimiter(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, Slip: 0, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		srv := newRateLimitServer(t, l)
		w := &countingWriter{udp: true}
		for i := 0; i < 20; i++ {
			srv.ServeDNS(w, aQuery())
		}
		if w.writeCount != 1 {
			t.Errorf("delivered = %d, want 1 (first allowed, rest dropped over UDP)", w.writeCount)
		}
	})

	t.Run("TCP flood is never rate limited", func(t *testing.T) {
		l := mustLimiter(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, Slip: 0, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		srv := newRateLimitServer(t, l)
		w := &countingWriter{udp: false}
		for i := 0; i < 20; i++ {
			srv.ServeDNS(w, aQuery())
		}
		if w.writeCount != 20 {
			t.Errorf("delivered = %d, want 20 (TCP bypasses limiting)", w.writeCount)
		}
	})

	t.Run("early error responses flow through the limiter without panic", func(t *testing.T) {
		// responses-per-second=5 ⇒ errors inherits 5 (cap 75); a single early
		// error reply must be delivered and must not panic.
		l := mustLimiter(t, &config.RateLimitConfig{
			ResponsesPerSecond: 5, Window: 15, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		srv := newRateLimitServer(t, l)
		// FORMERR: malformed query with zero questions.
		formerr := buildServeDNSRequest("", dns.TypeA, dns.ClassINET, dns.OpcodeQuery, 0)
		wf := &countingWriter{udp: true}
		srv.ServeDNS(wf, formerr)
		if wf.writeCount != 1 || wf.last.Rcode != dns.RcodeFormatError {
			t.Errorf("FORMERR path: writeCount=%d rcode=%d, want 1 / FORMERR", wf.writeCount, rcodeOf(wf.last))
		}
		// NOTIMP: unsupported opcode.
		notimp := buildServeDNSRequest("www.example.com.", dns.TypeA, dns.ClassINET, dns.OpcodeStatus, 1)
		wn := &countingWriter{udp: true}
		srv.ServeDNS(wn, notimp)
		if wn.writeCount != 1 || wn.last.Rcode != dns.RcodeNotImplemented {
			t.Errorf("NOTIMP path: writeCount=%d rcode=%d, want 1 / NOTIMP", wn.writeCount, rcodeOf(wn.last))
		}
	})

	t.Run("unconfigured limiter behaves identically to before", func(t *testing.T) {
		srv := newRateLimitServer(t, nil)
		w := &countingWriter{udp: true}
		for i := 0; i < 20; i++ {
			srv.ServeDNS(w, aQuery())
		}
		if w.writeCount != 20 {
			t.Errorf("delivered = %d, want 20 (no limiting when unconfigured)", w.writeCount)
		}
	})
}

func rcodeOf(m *dns.Msg) int {
	if m == nil {
		return -1
	}
	return m.Rcode
}
