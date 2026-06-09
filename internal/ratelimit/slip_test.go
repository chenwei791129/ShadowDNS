package ratelimit

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

func TestSlipActionDecision(t *testing.T) {
	t.Run("slip=0 always drops", func(t *testing.T) {
		for c := uint32(1); c <= 5; c++ {
			if got := slipAction(0, c); got != Drop {
				t.Errorf("slipAction(0, %d) = %v, want Drop", c, got)
			}
		}
	})
	t.Run("slip=1 always truncates", func(t *testing.T) {
		for c := uint32(1); c <= 5; c++ {
			if got := slipAction(1, c); got != Slip {
				t.Errorf("slipAction(1, %d) = %v, want Slip", c, got)
			}
		}
	})
	t.Run("slip=2 alternates truncate then drop", func(t *testing.T) {
		// Spec Example: 4 consecutive over-limit → truncate, drop, truncate, drop.
		want := []Action{Slip, Drop, Slip, Drop}
		for i, w := range want {
			if got := slipAction(2, uint32(i+1)); got != w {
				t.Errorf("slipAction(2, %d) = %v, want %v", i+1, got, w)
			}
		}
	})
}

// TestSlipActionSequence drives Decide through a run of over-limit responses on
// one account and asserts the slip=2 cadence holds end-to-end.
func TestSlipActionSequence(t *testing.T) {
	l, _ := limiterWithClock(t, &config.RateLimitConfig{
		ResponsesPerSecond: 1, Window: 1, Slip: 2, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
	})
	client := netip.MustParseAddr("192.0.2.10")
	name := "a.example.com."
	// First response consumes the 1-credit burst (allowed, not over-limit).
	if got := l.Decide(client, CategoryResponses, name); got != Allow {
		t.Fatalf("first response: got %v, want Allow", got)
	}
	// The next four are all over-limit and must follow truncate/drop cadence.
	want := []Action{Slip, Drop, Slip, Drop}
	for i, w := range want {
		if got := l.Decide(client, CategoryResponses, name); got != w {
			t.Errorf("over-limit response %d: got %v, want %v", i+1, got, w)
		}
	}
}

// TestSlipCadenceAggregateAcrossNames is a regression test: when over-limit is
// triggered solely by the all-per-second aggregate gate and each response
// carries a different query name, the slip cadence must follow the shared
// aggregate account — not reset per name (which would make every distinct
// name's first over-limit response a Slip, defeating the cadence).
func TestSlipCadenceAggregateAcrossNames(t *testing.T) {
	// responses-per-second=0 disables the category account; all-per-second=1
	// (cap 1) gates everything; slip=2 → truncate, drop, truncate, drop.
	l, _ := limiterWithClock(t, &config.RateLimitConfig{
		ResponsesPerSecond: 0, AllPerSecond: 1, Window: 1, Slip: 2,
		IPv4PrefixLength: 24, IPv6PrefixLength: 56,
	})
	client := netip.MustParseAddr("192.0.2.10")
	// First response consumes the aggregate burst of 1 (allowed).
	if got := l.Decide(client, CategoryResponses, "n0.example.com."); got != Allow {
		t.Fatalf("first response: got %v, want Allow", got)
	}
	// Each subsequent response uses a DISTINCT name but shares the aggregate
	// account, so the cadence must hold across names.
	want := []Action{Slip, Drop, Slip, Drop}
	for i, w := range want {
		name := fmt.Sprintf("n%d.example.com.", i+1)
		if got := l.Decide(client, CategoryResponses, name); got != w {
			t.Errorf("aggregate over-limit response %d (name %s): got %v, want %v", i+1, name, got, w)
		}
	}
}

func TestTruncateResponseShape(t *testing.T) {
	m := new(dns.Msg)
	m.Question = []dns.Question{{Name: "a.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}}
	m.Rcode = dns.RcodeSuccess
	m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "a.example.com.", Rrtype: dns.TypeA}}}
	m.Ns = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS}}}
	opt := &dns.OPT{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT}}
	m.Extra = []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Name: "glue.example.com.", Rrtype: dns.TypeA}},
		opt,
	}

	truncateResponse(m)

	if !m.Truncated {
		t.Error("TC bit not set")
	}
	if len(m.Answer) != 0 {
		t.Errorf("Answer = %d RRs, want 0", len(m.Answer))
	}
	if len(m.Ns) != 0 {
		t.Errorf("Ns = %d RRs, want 0", len(m.Ns))
	}
	if len(m.Extra) != 1 || m.Extra[0] != opt {
		t.Errorf("Extra = %v, want only the OPT record preserved", m.Extra)
	}
	if m.Rcode != dns.RcodeSuccess {
		t.Errorf("Rcode = %d, want preserved RcodeSuccess", m.Rcode)
	}
	if len(m.Question) != 1 || m.Question[0].Name != "a.example.com." {
		t.Errorf("Question not preserved: %v", m.Question)
	}
}

func TestTruncateResponseNoOPT(t *testing.T) {
	m := new(dns.Msg)
	m.Question = []dns.Question{{Name: "a.example.com."}}
	m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "a.example.com.", Rrtype: dns.TypeA}}}

	truncateResponse(m)

	if !m.Truncated {
		t.Error("TC bit not set")
	}
	if len(m.Extra) != 0 {
		t.Errorf("Extra = %d, want 0 when no OPT present", len(m.Extra))
	}
}
