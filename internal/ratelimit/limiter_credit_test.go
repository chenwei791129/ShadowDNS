package ratelimit

import (
	"net/netip"
	"testing"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// fakeClock is an injectable monotonic clock for deterministic credit tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestClock() *fakeClock {
	// Non-zero base so creditAccount.lastRefill.IsZero() correctly flags a
	// fresh (never-charged) account on first use.
	return &fakeClock{t: time.Unix(1_000_000, 0)}
}

// limiterWithClock builds a Limiter from cfg and swaps in a controllable clock.
func limiterWithClock(t *testing.T, cfg *config.RateLimitConfig) (*Limiter, *fakeClock) {
	t.Helper()
	l, err := NewLimiter(cfg)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	clk := newTestClock()
	l.now = clk.now
	return l, clk
}

func TestCreditAccounting(t *testing.T) {
	client := netip.MustParseAddr("192.0.2.10")
	name := "a.example.com."

	t.Run("fresh account allows a burst up to window*rate then over-limits", func(t *testing.T) {
		// responses-per-second=5, window=15 → cap = 75 credits.
		l, _ := limiterWithClock(t, &config.RateLimitConfig{ResponsesPerSecond: 5, Window: 15})
		for i := 1; i <= 75; i++ {
			if got := l.Decide(client, CategoryResponses, name); got != Allow {
				t.Fatalf("response %d: got %v, want Allow (within burst)", i, got)
			}
		}
		if got := l.Decide(client, CategoryResponses, name); got == Allow {
			t.Fatalf("response 76: got Allow, want over-limit (burst exhausted)")
		}
	})

	t.Run("credit regenerates at rate per second after idle", func(t *testing.T) {
		l, clk := limiterWithClock(t, &config.RateLimitConfig{ResponsesPerSecond: 5, Window: 15})
		// Deplete the 75-credit burst.
		for i := 0; i < 76; i++ {
			l.Decide(client, CategoryResponses, name)
		}
		// 76th drove balance negative; idle 2s regenerates 5/s = ~10 credits.
		clk.advance(2 * time.Second)
		if got := l.Decide(client, CategoryResponses, name); got != Allow {
			t.Errorf("after 2s idle: got %v, want Allow (credit regenerated)", got)
		}
	})

	t.Run("regenerated credit caps at window*rate", func(t *testing.T) {
		l, clk := limiterWithClock(t, &config.RateLimitConfig{ResponsesPerSecond: 5, Window: 15})
		// Deplete, then idle far longer than the window (100s would regen 500
		// uncapped, but must cap at 75).
		for i := 0; i < 76; i++ {
			l.Decide(client, CategoryResponses, name)
		}
		clk.advance(100 * time.Second)
		// Cap is 75: exactly 75 more allowed, the 76th over-limits again.
		for i := 1; i <= 75; i++ {
			if got := l.Decide(client, CategoryResponses, name); got != Allow {
				t.Fatalf("post-idle response %d: got %v, want Allow (capped burst)", i, got)
			}
		}
		if got := l.Decide(client, CategoryResponses, name); got == Allow {
			t.Fatalf("post-idle response 76: got Allow, want over-limit (cap is 75, not 500)")
		}
	})

	t.Run("zero rate disables a category", func(t *testing.T) {
		// nxdomains-per-second omitted → inherits responses-per-second=0 → disabled.
		l, _ := limiterWithClock(t, &config.RateLimitConfig{ResponsesPerSecond: 0, Window: 15})
		for i := 0; i < 1000; i++ {
			if got := l.Decide(client, CategoryNxdomains, "example.com."); got != Allow {
				t.Fatalf("nxdomain %d with zero rate: got %v, want Allow (disabled)", i, got)
			}
		}
	})

	t.Run("all-per-second aggregate gate over-limits across categories", func(t *testing.T) {
		// Category rate effectively unlimited (huge), but all-per-second=5
		// (cap 75) gates every response regardless of category.
		l, _ := limiterWithClock(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1_000_000, AllPerSecond: 5, Window: 15,
		})
		for i := 1; i <= 75; i++ {
			if got := l.Decide(client, CategoryResponses, name); got != Allow {
				t.Fatalf("response %d: got %v, want Allow (under aggregate cap)", i, got)
			}
		}
		if got := l.Decide(client, CategoryResponses, name); got == Allow {
			t.Fatalf("response 76: got Allow, want over-limit (aggregate gate exhausted)")
		}
	})
}
