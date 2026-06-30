package transfer

import (
	"errors"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"
)

// abortingResponseWriter is a fake dns.ResponseWriter that fails its WriteMsg
// after a configurable number of successful writes, simulating a transfer peer
// that aborts the connection mid-stream. It exercises only our streamAXFR; it
// does not stand in for any real network behavior of the DNS library.
type abortingResponseWriter struct {
	// failAfter is the number of WriteMsg calls that succeed before the next
	// one (and every one thereafter) returns an error. failAfter == 1 means the
	// first write succeeds and the second fails.
	failAfter int
	writes    atomic.Int32
}

func (w *abortingResponseWriter) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}
func (w *abortingResponseWriter) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

func (w *abortingResponseWriter) WriteMsg(*dns.Msg) error {
	n := int(w.writes.Add(1))
	if n > w.failAfter {
		return errors.New("simulated peer abort: write failed")
	}
	return nil
}

func (w *abortingResponseWriter) Write([]byte) (int, error) {
	n := int(w.writes.Add(1))
	if n > w.failAfter {
		return 0, errors.New("simulated peer abort: write failed")
	}
	return 0, nil
}

func (w *abortingResponseWriter) Close() error        { return nil }
func (w *abortingResponseWriter) TsigStatus() error   { return nil }
func (w *abortingResponseWriter) TsigTimersOnly(bool) {}
func (w *abortingResponseWriter) Hijack()             {}

// runStreamAXFRWithTimeout drives streamAXFR in a goroutine and reports whether
// it returned before the timeout. A false result means streamAXFR is hung
// (e.g. a producer send blocked because the consumer goroutine already exited).
func runStreamAXFRWithTimeout(t *testing.T, w dns.ResponseWriter, soa *dns.SOA, records []dns.RR, timeout time.Duration) bool {
	t.Helper()

	req := new(dns.Msg)
	req.SetAxfr(soa.Hdr.Name)

	done := make(chan struct{})
	go func() {
		streamAXFR(w, req, soa, records, zap.NewNop())
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// TestStreamAXFR_PeerAbortMidStreamDoesNotHang covers the requirement "AXFR
// streaming survives a mid-stream peer abort without leaking": when the peer's
// second WriteMsg errors, streamAXFR must return promptly rather than block
// forever on a send to a channel whose consumer goroutine has already exited.
func TestStreamAXFR_PeerAbortMidStreamDoesNotHang(t *testing.T) {
	soa := &dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:     "ns1.example.com.",
		Mbox:   "admin.example.com.",
		Serial: 2024010101,
	}
	records := []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.1"),
		},
	}

	// The peer accepts the first envelope (SOA) then aborts: the second
	// WriteMsg (records) fails, so the transfer-out routine returns before the
	// producer has sent the closing SOA.
	w := &abortingResponseWriter{failAfter: 1}

	before := runtime.NumGoroutine()

	if !runStreamAXFRWithTimeout(t, w, soa, records, 2*time.Second) {
		t.Fatal("streamAXFR did not return within 2s after a mid-stream peer abort (producer goroutine leaked)")
	}

	// The producer goroutine must have unwound. Allow brief scheduler settling.
	assertNoGoroutineGrowth(t, before)
}

// TestStreamAXFR_RepeatedAbortsDoNotLeakGoroutines covers "Repeated mid-stream
// aborts do not accumulate resources": many aborted transfers in a row must not
// grow the live goroutine count without bound.
func TestStreamAXFR_RepeatedAbortsDoNotLeakGoroutines(t *testing.T) {
	soa := &dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:     "ns1.example.com.",
		Mbox:   "admin.example.com.",
		Serial: 2024010101,
	}
	records := []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.1"),
		},
	}

	before := runtime.NumGoroutine()

	const iterations = 50
	for i := 0; i < iterations; i++ {
		w := &abortingResponseWriter{failAfter: 1}
		if !runStreamAXFRWithTimeout(t, w, soa, records, 2*time.Second) {
			t.Fatalf("streamAXFR hung on aborted transfer iteration %d", i)
		}
	}

	assertNoGoroutineGrowth(t, before)
}

// TestStreamAXFR_PanicDuringTransferIsRecovered covers "A panic while packing
// an envelope does not crash the process": a panic raised inside the transfer
// goroutine must be recovered so the call returns and the process keeps running.
func TestStreamAXFR_PanicDuringTransferIsRecovered(t *testing.T) {
	soa := &dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:     "ns1.example.com.",
		Mbox:   "admin.example.com.",
		Serial: 2024010101,
	}
	records := []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.1"),
		},
	}

	// A writer that panics inside the transfer goroutine (during the first
	// envelope write) stands in for a packing failure. Without recover(), the
	// panic would unwind through the goroutine and crash the test process.
	w := &panickingResponseWriter{}

	if !runStreamAXFRWithTimeout(t, w, soa, records, 2*time.Second) {
		t.Fatal("streamAXFR did not return after a panic in the transfer goroutine")
	}
}

// panickingResponseWriter panics on the first WriteMsg, simulating a panic
// raised inside the transfer goroutine while packing/writing an envelope.
type panickingResponseWriter struct{}

func (w *panickingResponseWriter) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}
func (w *panickingResponseWriter) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}
func (w *panickingResponseWriter) WriteMsg(*dns.Msg) error { panic("simulated packing panic") }
func (w *panickingResponseWriter) Write([]byte) (int, error) {
	panic("simulated packing panic")
}
func (w *panickingResponseWriter) Close() error        { return nil }
func (w *panickingResponseWriter) TsigStatus() error   { return nil }
func (w *panickingResponseWriter) TsigTimersOnly(bool) {}
func (w *panickingResponseWriter) Hijack()             {}

// assertNoGoroutineGrowth fails if the live goroutine count has grown
// meaningfully above baseline, allowing a small slack for scheduler settling.
func assertNoGoroutineGrowth(t *testing.T, before int) {
	t.Helper()

	const slack = 2
	// Goroutine teardown is asynchronous; poll briefly before asserting.
	deadline := time.Now().Add(2 * time.Second)
	for {
		after := runtime.NumGoroutine()
		if after <= before+slack {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count grew from %d to %d (leak suspected)", before, after)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
