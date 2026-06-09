package ratelimit

import (
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// fakeRW is a dns.ResponseWriter stub that records WriteMsg calls and reports a
// configurable transport via LocalAddr.
type fakeRW struct {
	local      net.Addr
	remote     net.Addr
	written    []*dns.Msg
	writeCount int
}

func (f *fakeRW) LocalAddr() net.Addr       { return f.local }
func (f *fakeRW) RemoteAddr() net.Addr      { return f.remote }
func (f *fakeRW) Write([]byte) (int, error) { return 0, nil }
func (f *fakeRW) Close() error              { return nil }
func (f *fakeRW) TsigStatus() error         { return nil }
func (f *fakeRW) TsigTimersOnly(bool)       {}
func (f *fakeRW) Hijack()                   {}
func (f *fakeRW) WriteMsg(m *dns.Msg) error {
	f.written = append(f.written, m)
	f.writeCount++
	return nil
}

func udpRW(ip string) *fakeRW {
	addr := &net.UDPAddr{IP: net.ParseIP(ip), Port: 5353}
	return &fakeRW{local: &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 53}, remote: addr}
}

func tcpRW(ip string) *fakeRW {
	addr := &net.TCPAddr{IP: net.ParseIP(ip), Port: 5353}
	return &fakeRW{local: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 53}, remote: addr}
}

func TestRateLimitWriter(t *testing.T) {
	t.Run("TCP responses bypass the limiter entirely", func(t *testing.T) {
		l, _ := limiterWithClock(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, Slip: 0, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		inner := tcpRW("192.0.2.5")
		w := NewResponseWriter(inner, l)
		// Far more than the cap of 1; all must be delivered over TCP.
		for i := 0; i < 50; i++ {
			if err := w.WriteMsg(positiveMsg("a.example.com.")); err != nil {
				t.Fatalf("WriteMsg: %v", err)
			}
		}
		if inner.writeCount != 50 {
			t.Errorf("TCP writeCount = %d, want 50 (no limiting)", inner.writeCount)
		}
	})

	t.Run("UDP over-limit with slip=0 is dropped (not written)", func(t *testing.T) {
		l, _ := limiterWithClock(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, Slip: 0, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		inner := udpRW("192.0.2.5")
		w := NewResponseWriter(inner, l)
		// First is allowed (burst of 1); second is over-limit and dropped.
		_ = w.WriteMsg(positiveMsg("a.example.com."))
		_ = w.WriteMsg(positiveMsg("a.example.com."))
		if inner.writeCount != 1 {
			t.Errorf("UDP writeCount = %d, want 1 (second response dropped)", inner.writeCount)
		}
	})

	t.Run("UDP over-limit with slip=1 writes a truncated response", func(t *testing.T) {
		l, _ := limiterWithClock(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, Slip: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		inner := udpRW("192.0.2.5")
		w := NewResponseWriter(inner, l)
		_ = w.WriteMsg(positiveMsg("a.example.com."))
		_ = w.WriteMsg(positiveMsg("a.example.com."))
		if inner.writeCount != 2 {
			t.Fatalf("UDP writeCount = %d, want 2 (slip truncates and writes)", inner.writeCount)
		}
		slipped := inner.written[1]
		if !slipped.Truncated {
			t.Error("slipped response missing TC bit")
		}
		if len(slipped.Answer) != 0 {
			t.Errorf("slipped response Answer = %d, want 0 (cleared)", len(slipped.Answer))
		}
	})

	t.Run("nil limiter delivers unchanged", func(t *testing.T) {
		inner := udpRW("192.0.2.5")
		w := NewResponseWriter(inner, nil)
		for i := 0; i < 10; i++ {
			_ = w.WriteMsg(positiveMsg("a.example.com."))
		}
		if inner.writeCount != 10 {
			t.Errorf("nil-limiter writeCount = %d, want 10", inner.writeCount)
		}
	})
}
