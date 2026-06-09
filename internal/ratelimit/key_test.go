package ratelimit

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

func positiveMsg(qname string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeSuccess
	m.Question = []dns.Question{{Name: qname, Qtype: dns.TypeA, Qclass: dns.ClassINET}}
	m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA}}}
	return m
}

func nxdomainMsg(qname, zone string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeNameError
	m.Question = []dns.Question{{Name: qname, Qtype: dns.TypeA, Qclass: dns.ClassINET}}
	m.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: zone, Rrtype: dns.TypeSOA}}}
	return m
}

func TestImputedName(t *testing.T) {
	t.Run("responses use the exact (folded) query name", func(t *testing.T) {
		m := positiveMsg("WWW.Example.COM.")
		if got := ImputedName(m, CategoryResponses); got != "www.example.com." {
			t.Errorf("ImputedName(responses) = %q, want %q", got, "www.example.com.")
		}
	})
	t.Run("nxdomains use the authority SOA owner (zone origin)", func(t *testing.T) {
		m := nxdomainMsg("random123.example.com.", "example.com.")
		if got := ImputedName(m, CategoryNxdomains); got != "example.com." {
			t.Errorf("ImputedName(nxdomains) = %q, want %q", got, "example.com.")
		}
	})
	t.Run("nodata use the authority SOA owner", func(t *testing.T) {
		m := new(dns.Msg)
		m.Rcode = dns.RcodeSuccess
		m.Question = []dns.Question{{Name: "host.example.com."}}
		m.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA}}}
		if got := ImputedName(m, CategoryNodata); got != "example.com." {
			t.Errorf("ImputedName(nodata) = %q, want %q", got, "example.com.")
		}
	})
	t.Run("errors use an empty name", func(t *testing.T) {
		m := new(dns.Msg)
		m.Rcode = dns.RcodeRefused
		m.Question = []dns.Question{{Name: "whatever.test."}}
		if got := ImputedName(m, CategoryErrors); got != "" {
			t.Errorf("ImputedName(errors) = %q, want empty", got)
		}
	})
}

func TestAccountKeyMasking(t *testing.T) {
	l, _ := limiterWithClock(t, &config.RateLimitConfig{ResponsesPerSecond: 1, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56})
	a := l.maskAddr(netip.MustParseAddr("192.0.2.10"))
	b := l.maskAddr(netip.MustParseAddr("192.0.2.200"))
	if a != b {
		t.Errorf("masking /24: %v and %v differ, want same block", a, b)
	}
	if a != netip.MustParseAddr("192.0.2.0") {
		t.Errorf("masked block = %v, want 192.0.2.0", a)
	}
}

func TestAccountKey(t *testing.T) {
	t.Run("random-subdomain NXDOMAIN flood aggregates per zone across a client block", func(t *testing.T) {
		// nxdomains-per-second=5, window=1 → cap 5. 20 distinct random names
		// under example.com from two addresses in the same /24 must all share
		// one account; responses beyond 5 are over-limit (spec Example).
		l, _ := limiterWithClock(t, &config.RateLimitConfig{
			NxdomainsPerSecond: 5, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		addrs := []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("192.0.2.200")}
		allowed := 0
		for i := 0; i < 20; i++ {
			m := nxdomainMsg(fmt.Sprintf("a%d.example.com.", i), "example.com.")
			cat := ClassifyResponse(m)
			name := ImputedName(m, cat)
			if l.Decide(addrs[i%2], cat, name) == Allow {
				allowed++
			}
		}
		if allowed != 5 {
			t.Errorf("allowed = %d, want 5 (20 random subdomains share one zone-origin account)", allowed)
		}
	})

	t.Run("positive answers to distinct names key independently", func(t *testing.T) {
		// responses-per-second=1, window=1 → cap 1 per account. Distinct query
		// names get distinct accounts, so the first hit to each is allowed.
		l, _ := limiterWithClock(t, &config.RateLimitConfig{
			ResponsesPerSecond: 1, Window: 1, IPv4PrefixLength: 24, IPv6PrefixLength: 56,
		})
		client := netip.MustParseAddr("192.0.2.10")
		mx := positiveMsg("x.example.com.")
		my := positiveMsg("y.example.com.")
		if got := l.Decide(client, ClassifyResponse(mx), ImputedName(mx, CategoryResponses)); got != Allow {
			t.Errorf("first response to x: got %v, want Allow", got)
		}
		if got := l.Decide(client, ClassifyResponse(my), ImputedName(my, CategoryResponses)); got != Allow {
			t.Errorf("first response to y: got %v, want Allow (distinct name ⇒ distinct account)", got)
		}
	})
}
