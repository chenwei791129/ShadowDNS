package transfer

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// --------------------------------------------------------------------------
// NotifyTargets tests
// --------------------------------------------------------------------------

func makeZoneWithNSAndMNAME(origin, mname string, nsTargets []string) *zone.Zone {
	z := &zone.Zone{Origin: origin}
	soa := &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   origin,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Ns:     mname,
		Mbox:   "hostmaster." + origin,
		Serial: 1,
	}
	z.AddRR(soa)
	for _, ns := range nsTargets {
		z.AddRR(&dns.NS{
			Hdr: dns.RR_Header{
				Name:   origin,
				Rrtype: dns.TypeNS,
				Class:  dns.ClassINET,
				Ttl:    3600,
			},
			Ns: ns,
		})
	}
	return z
}

// addGlueA inserts an A RR for `host` (FQDN, case as supplied) with the given
// dotted-quad address into z.Records, simulating an in-zone glue record.
func addGlueA(z *zone.Zone, host, ipv4 string) {
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{
			Name:   host,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		A: net.ParseIP(ipv4).To4(),
	})
}

// addGlueAAAA inserts an AAAA RR for `host` with the given IPv6 address.
func addGlueAAAA(z *zone.Zone, host, ipv6 string) {
	z.AddRR(&dns.AAAA{
		Hdr: dns.RR_Header{
			Name:   host,
			Rrtype: dns.TypeAAAA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		AAAA: net.ParseIP(ipv6),
	})
}

// findTarget returns the NotifyTarget whose Host equals the FQDN of `host`,
// or nil if no such target exists in the slice. Lookup is case-insensitive on
// the hostname per RFC 4343.
func findTarget(targets []NotifyTarget, host string) *NotifyTarget {
	want := dns.Fqdn(host)
	for i := range targets {
		if dns.Fqdn(targets[i].Host) == want {
			return &targets[i]
		}
	}
	return nil
}

// sortAddrs returns a sorted copy of addrs for stable comparison in assertions.
func sortAddrs(addrs []netip.Addr) []netip.Addr {
	out := make([]netip.Addr, len(addrs))
	copy(out, addrs)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// In-zone A glue is populated on the returned NotifyTarget. Spec scenario:
// "NOTIFY sent to each in-zone glue IP of an NS target".
func TestNotifyTargets_InZoneAGlue_Populated(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "ns1.example.com.",
		[]string{"ns2.example.com."})
	addGlueA(z, "ns2.example.com.", "10.0.0.2")

	targets := NotifyTargets(z)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d: %+v", len(targets), targets)
	}
	got := targets[0]
	if got.Host != "ns2.example.com." {
		t.Errorf("Host: want ns2.example.com., got %q", got.Host)
	}
	if len(got.IPs) != 1 {
		t.Fatalf("IPs: want 1, got %d (%v)", len(got.IPs), got.IPs)
	}
	if got.IPs[0] != netip.MustParseAddr("10.0.0.2") {
		t.Errorf("IPs[0]: want 10.0.0.2, got %v", got.IPs[0])
	}
}

// Multi-IP glue — both an A+AAAA pair and multi-A return every IP. Spec
// scenario: "NOTIFY sent to every glue IP when multiple exist".
func TestNotifyTargets_MultiIPGlue_AllReturned(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "ns1.example.com.",
		[]string{"ns21.example.com.", "ns22.example.com."})
	// ns21: dual-stack (A + AAAA).
	addGlueA(z, "ns21.example.com.", "10.0.0.21")
	addGlueAAAA(z, "ns21.example.com.", "2001:db8::21")
	// ns22: multi-A (anycast / multi-slave sharing a hostname).
	addGlueA(z, "ns22.example.com.", "10.0.0.221")
	addGlueA(z, "ns22.example.com.", "10.0.0.222")

	targets := NotifyTargets(z)

	ns21 := findTarget(targets, "ns21.example.com.")
	if ns21 == nil {
		t.Fatalf("ns21.example.com. missing from %+v", targets)
	}
	want21 := sortAddrs([]netip.Addr{
		netip.MustParseAddr("10.0.0.21"),
		netip.MustParseAddr("2001:db8::21"),
	})
	got21 := sortAddrs(ns21.IPs)
	if len(got21) != len(want21) {
		t.Fatalf("ns21 IPs: want %v, got %v", want21, got21)
	}
	for i := range want21 {
		if got21[i] != want21[i] {
			t.Errorf("ns21 IPs[%d]: want %v, got %v", i, want21[i], got21[i])
		}
	}

	ns22 := findTarget(targets, "ns22.example.com.")
	if ns22 == nil {
		t.Fatalf("ns22.example.com. missing from %+v", targets)
	}
	want22 := sortAddrs([]netip.Addr{
		netip.MustParseAddr("10.0.0.221"),
		netip.MustParseAddr("10.0.0.222"),
	})
	got22 := sortAddrs(ns22.IPs)
	if len(got22) != len(want22) {
		t.Fatalf("ns22 IPs: want %v, got %v", want22, got22)
	}
	for i := range want22 {
		if got22[i] != want22[i] {
			t.Errorf("ns22 IPs[%d]: want %v, got %v", i, want22[i], got22[i])
		}
	}
}

// NS pointing out-of-bailiwick returns a target with empty IPs. The
// function takes only a *zone.Zone, so the absence of network access is
// structural; this test asserts the empty-IPs contract that callers rely
// on for the skip decision. Spec scenario: "NS target without in-zone
// glue is skipped".
func TestNotifyTargets_OutOfBailiwickNS_EmptyIPs(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "ns1.example.com.",
		[]string{"ns.elsewhere.test."})

	targets := NotifyTargets(z)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d: %+v", len(targets), targets)
	}
	got := targets[0]
	if dns.Fqdn(got.Host) != "ns.elsewhere.test." {
		t.Errorf("Host: want ns.elsewhere.test., got %q", got.Host)
	}
	if len(got.IPs) != 0 {
		t.Errorf("IPs: want empty for out-of-bailiwick NS, got %v", got.IPs)
	}
}

// Glue lookup MUST consult only the passed-in zone's records, even when
// another zone in memory happens to hold an A record for the same name.
func TestNotifyTargets_GlueLookupScopedToOwnZone(t *testing.T) {
	// Zone A: example.com. — its NS target has no glue in *this* zone.
	zA := makeZoneWithNSAndMNAME("example.com.", "ns1.example.com.",
		[]string{"ns.elsewhere.test."})

	// Zone B: elsewhere.test. — owns the A record for ns.elsewhere.test.
	// (separate *zone.Zone instance; the helper must not see it)
	zB := &zone.Zone{Origin: "elsewhere.test."}
	zB.AddRR(&dns.SOA{
		Hdr: dns.RR_Header{Name: "elsewhere.test.", Rrtype: dns.TypeSOA, Class: dns.ClassINET},
		Ns:  "ns1.elsewhere.test.",
	})
	addGlueA(zB, "ns.elsewhere.test.", "10.99.99.99")

	targets := NotifyTargets(zA)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if len(targets[0].IPs) != 0 {
		t.Errorf("cross-zone glue leaked: IPs=%v (must be empty when "+
			"the NS target's A record lives in a different *zone.Zone)", targets[0].IPs)
	}
}

// MNAME-exclusion holds even when the MNAME itself has in-zone glue. Spec
// scenario: "NOTIFY not sent to SOA MNAME".
func TestNotifyTargets_ExcludesMNAME_EvenWithGlue(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "ns1.example.com.",
		[]string{"ns1.example.com.", "ns2.example.com."})
	// MNAME has glue, but it must still be excluded.
	addGlueA(z, "ns1.example.com.", "10.0.0.1")
	addGlueA(z, "ns2.example.com.", "10.0.0.2")

	targets := NotifyTargets(z)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target (MNAME excluded), got %d: %+v", len(targets), targets)
	}
	if dns.Fqdn(targets[0].Host) != "ns2.example.com." {
		t.Errorf("Host: want ns2.example.com., got %q", targets[0].Host)
	}
	if findTarget(targets, "ns1.example.com.") != nil {
		t.Error("ns1.example.com. (MNAME) must not appear in the target list")
	}
}

func TestNotifyTargets_UnmatchedMNAME_ReturnsAll(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "primary.elsewhere.test.",
		[]string{"ns1.example.com.", "ns2.example.com.", "ns3.example.com."})

	targets := NotifyTargets(z)
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}
}

func TestNotifyTargets_NoNS(t *testing.T) {
	z := &zone.Zone{Origin: "example.com."}
	soa := &dns.SOA{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET},
		Ns:  "ns1.example.com.",
	}
	z.AddRR(soa)

	targets := NotifyTargets(z)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets, got %v", targets)
	}
}

// --------------------------------------------------------------------------
// SendNOTIFY / sendNotifyWithBackoff tests
// --------------------------------------------------------------------------

// startMockNotifyServer starts a UDP DNS server on a random port.
// When a NOTIFY is received, it increments notifyCount and sends NOERROR.
// Returns the server address and a cleanup function.
func startMockNotifyServer(t *testing.T, notifyCount *atomic.Int32) (addr string, cleanup func()) {
	t.Helper()

	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot bind UDP: %v", err)
	}

	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		if r.Opcode == dns.OpcodeNotify {
			notifyCount.Add(1)
			m := new(dns.Msg)
			m.SetReply(r)
			m.Opcode = dns.OpcodeNotify
			_ = w.WriteMsg(m)
		}
	})

	srv := &dns.Server{
		PacketConn: pc,
		Net:        "udp",
		Handler:    mux,
	}

	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }

	go func() { _ = srv.ActivateAndServe() }()
	<-started

	return pc.LocalAddr().String(), func() { _ = srv.Shutdown() }
}

func TestSendNOTIFY_Success(t *testing.T) {
	var count atomic.Int32
	addr, cleanup := startMockNotifyServer(t, &count)
	defer cleanup()

	logger := zap.NewNop()
	fastBackoff := []time.Duration{1 * time.Millisecond}

	err := sendNotifyWithBackoff(context.Background(), "example.com.", addr, fastBackoff, logger)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// Give the mock a moment to process; then check it received exactly one NOTIFY.
	time.Sleep(10 * time.Millisecond)
	if n := count.Load(); n != 1 {
		t.Errorf("expected 1 NOTIFY, mock saw %d", n)
	}
}

func TestSendNOTIFY_RetriesOnFailure(t *testing.T) {
	// Point at an address where nothing is listening — every exchange will time out fast.
	// Use a fast backoff so the test doesn't take real wall time.
	fastBackoff := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond}

	// Use a nonexistent port.  We need to override the DNS exchange timeout.
	// In practice dns.Exchange with a no-listener returns quickly (connection refused or timeout).
	// We use 127.0.0.1:1 which is almost always closed.
	logger := zap.NewNop()

	err := sendNotifyWithBackoff(context.Background(), "example.com.", "127.0.0.1:1",
		fastBackoff, logger)
	if err == nil {
		t.Fatal("expected error from unreachable target, got nil")
	}
}

// Per-attempt Warnw inside sendNotifyWithBackoff carries the inherited
// `target`, `ip`, and `source` fields when the caller pre-enriches the
// logger via With() — the pattern dispatchNotifies relies on so retries
// remain log-correlatable to their NS hostname and source path.
func TestSendNOTIFY_PerAttemptWarn_InheritsLoggerFields(t *testing.T) {
	core, obs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core).With(
		zap.String("target", "ns2.example.com."),
		zap.String("ip", "127.0.0.1"),
		zap.String("source", "glue"),
	)

	fastBackoff := []time.Duration{1 * time.Millisecond}
	err := sendNotifyWithBackoff(context.Background(), "example.com.", "127.0.0.1:1",
		fastBackoff, logger)
	if err == nil {
		t.Fatal("expected error from unreachable target, got nil")
	}

	fails := obs.FilterMessage("NOTIFY failed").All()
	if len(fails) < 1 {
		t.Fatalf("expected at least 1 NOTIFY failed log, got 0 (all: %+v)", obs.All())
	}
	got := fails[0]
	want := map[string]string{
		"zone":   "example.com.",
		"addr":   "127.0.0.1:1",
		"target": "ns2.example.com.",
		"ip":     "127.0.0.1",
		"source": "glue",
	}
	for k, v := range want {
		var found string
		for _, f := range got.Context {
			if f.Key == k {
				found = f.String
				break
			}
		}
		if found != v {
			t.Errorf("log field %q: want %q, got %q (full fields: %+v)", k, v, found, got.Context)
		}
	}
	// `attempt` is logged as int — assert presence and value via Integer.
	var sawAttempt bool
	for _, f := range got.Context {
		if f.Key == "attempt" {
			sawAttempt = true
			if f.Integer != 1 {
				t.Errorf("attempt: want 1, got %d", f.Integer)
			}
		}
	}
	if !sawAttempt {
		t.Errorf("attempt field missing from log: %+v", got.Context)
	}
}

func TestSendNOTIFY_CtxCancel(t *testing.T) {
	// Create a context that we cancel almost immediately.
	fastBackoff := []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	logger := zap.NewNop()

	// Cancel the context after a tiny delay (after the first attempt fails).
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := sendNotifyWithBackoff(ctx, "example.com.", "127.0.0.1:1", fastBackoff, logger)
	if err == nil {
		t.Fatal("expected ctx cancellation error")
	}
	// Should be context.Canceled or context.DeadlineExceeded.
	if ctx.Err() == nil {
		t.Errorf("expected context to be cancelled; err=%v", err)
	}
}
