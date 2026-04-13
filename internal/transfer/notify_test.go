package transfer

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"

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

func TestNotifyTargets_ExcludesMNAME(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "ns1.example.com.",
		[]string{"ns1.example.com.", "ns2.example.com."})

	targets := NotifyTargets(z)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %v", targets)
	}
	if targets[0] != "ns2.example.com." {
		t.Errorf("expected ns2.example.com., got %s", targets[0])
	}
}

func TestNotifyTargets_UnmatchedMNAME_ReturnsAll(t *testing.T) {
	z := makeZoneWithNSAndMNAME("example.com.", "primary.other.com.",
		[]string{"ns1.example.com.", "ns2.example.com.", "ns3.example.com."})

	targets := NotifyTargets(z)
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %v", targets)
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

	logger := slog.Default()
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
	logger := slog.Default()

	err := sendNotifyWithBackoff(context.Background(), "example.com.", "127.0.0.1:1",
		fastBackoff, logger)
	if err == nil {
		t.Fatal("expected error from unreachable target, got nil")
	}
}

func TestSendNOTIFY_CtxCancel(t *testing.T) {
	// Create a context that we cancel almost immediately.
	fastBackoff := []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.Default()

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
