package ratelimit

import (
	"net/netip"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// spyRecorder captures RecordRateLimit calls as "category/action" strings.
type spyRecorder struct{ calls []string }

func (s *spyRecorder) RecordRateLimit(category, action string) {
	s.calls = append(s.calls, category+"/"+action)
}

func (s *spyRecorder) count(want string) int {
	n := 0
	for _, c := range s.calls {
		if c == want {
			n++
		}
	}
	return n
}

func TestLimiterDecide(t *testing.T) {
	name := "a.example.com."

	t.Run("exempt client is never limited", func(t *testing.T) {
		spy := &spyRecorder{}
		l, err := NewLimiter(&config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
			ExemptClients: []string{"192.0.2.0/24"},
		}, WithRecorder(spy))
		if err != nil {
			t.Fatalf("NewLimiter: %v", err)
		}
		l.now = newTestClock().now
		exempt := netip.MustParseAddr("192.0.2.5")
		for i := 0; i < 100; i++ {
			if got := l.Decide(exempt, CategoryResponses, name); got != Allow {
				t.Fatalf("exempt flood response %d: got %v, want Allow", i, got)
			}
		}
		if spy.count("responses/exempted") == 0 {
			t.Errorf("expected exempted to be recorded, calls=%v", spy.calls)
		}
		// A non-exempt client is still subject to limiting (exemption is scoped).
		other := netip.MustParseAddr("198.51.100.5")
		if l.Decide(other, CategoryResponses, name) != Allow {
			t.Errorf("non-exempt first response: want Allow")
		}
		if l.Decide(other, CategoryResponses, name) == Allow {
			t.Errorf("non-exempt second response: want over-limit (cap 1)")
		}
	})

	t.Run("log-only records would-drop but delivers", func(t *testing.T) {
		spy := &spyRecorder{}
		l, err := NewLimiter(&config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
			LogOnly: true,
		}, WithRecorder(spy))
		if err != nil {
			t.Fatalf("NewLimiter: %v", err)
		}
		l.now = newTestClock().now
		client := netip.MustParseAddr("203.0.113.7")
		if got := l.Decide(client, CategoryResponses, name); got != Allow {
			t.Fatalf("first response: got %v, want Allow", got)
		}
		// Second response is over-limit, but log-only delivers it unchanged.
		if got := l.Decide(client, CategoryResponses, name); got != Allow {
			t.Errorf("over-limit response under log-only: got %v, want Allow", got)
		}
		if spy.count("responses/logonly_would_drop") != 1 {
			t.Errorf("logonly_would_drop count = %d, want 1; calls=%v", spy.count("responses/logonly_would_drop"), spy.calls)
		}
		if spy.count("responses/dropped") != 0 {
			t.Errorf("dropped count = %d, want 0 under log-only", spy.count("responses/dropped"))
		}
	})

	t.Run("enforcing mode does not deliver over-limit responses", func(t *testing.T) {
		spy := &spyRecorder{}
		l, err := NewLimiter(&config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		}, WithRecorder(spy))
		if err != nil {
			t.Fatalf("NewLimiter: %v", err)
		}
		l.now = newTestClock().now
		client := netip.MustParseAddr("203.0.113.8")
		if got := l.Decide(client, CategoryResponses, name); got != Allow {
			t.Fatalf("first response: got %v, want Allow", got)
		}
		if got := l.Decide(client, CategoryResponses, name); got == Allow {
			t.Errorf("over-limit response while enforcing: got Allow, want Drop/Slip")
		}
	})

	t.Run("v4-mapped-v6 exempt CIDR matches the unmapped client", func(t *testing.T) {
		// Regression: "::ffff:192.0.2.0/120" must exempt 192.0.2.5 after the
		// prefix is unmapped and its length adjusted to /24, not silently
		// swallowed as an invalid >/32 IPv4 prefix.
		l, err := NewLimiter(&config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
			ExemptClients: []string{"::ffff:192.0.2.0/120"},
		})
		if err != nil {
			t.Fatalf("NewLimiter: %v", err)
		}
		l.now = newTestClock().now
		client := netip.MustParseAddr("192.0.2.5")
		for i := 0; i < 50; i++ {
			if got := l.Decide(client, CategoryResponses, name); got != Allow {
				t.Fatalf("v4-mapped exempt flood response %d: got %v, want Allow", i, got)
			}
		}
	})

	t.Run("invalid exempt-clients entry is a construction error", func(t *testing.T) {
		_, err := NewLimiter(&config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1,
			ExemptClients: []string{"not-an-ip"},
		})
		if err == nil {
			t.Error("expected error for invalid exempt-clients entry, got nil")
		}
	})
}
