package view

import (
	"net/netip"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

func TestMatchIP(t *testing.T) {
	t.Run("exact IP match positive", func(t *testing.T) {
		ip := netip.MustParseAddr("192.0.2.1")
		rule := config.IPRule{IP: ip}
		if !matchIP(rule, ip) {
			t.Error("expected match, got no-match")
		}
	})

	t.Run("exact IP match negative", func(t *testing.T) {
		rule := config.IPRule{IP: netip.MustParseAddr("192.0.2.1")}
		client := netip.MustParseAddr("192.0.2.2")
		if matchIP(rule, client) {
			t.Error("expected no-match, got match")
		}
	})

	t.Run("zero client IP does not match a real rule IP", func(t *testing.T) {
		rule := config.IPRule{IP: netip.MustParseAddr("192.0.2.1")}
		var zero netip.Addr
		if matchIP(rule, zero) {
			t.Error("expected no-match for zero IP, got match")
		}
	})

	t.Run("zero rule IP does not match a real client IP", func(t *testing.T) {
		var zero netip.Addr
		rule := config.IPRule{IP: zero}
		client := netip.MustParseAddr("192.0.2.1")
		if matchIP(rule, client) {
			t.Error("expected no-match, got match")
		}
	})
}

func TestMatchCIDR(t *testing.T) {
	t.Run("CIDR /26 match positive", func(t *testing.T) {
		rule := config.CIDRRule{Prefix: netip.MustParsePrefix("192.0.2.0/26")}
		client := netip.MustParseAddr("192.0.2.63") // last host in /26
		if !matchCIDR(rule, client) {
			t.Error("expected CIDR match, got no-match")
		}
	})

	t.Run("CIDR /26 match negative — address outside prefix", func(t *testing.T) {
		rule := config.CIDRRule{Prefix: netip.MustParsePrefix("192.0.2.0/26")}
		client := netip.MustParseAddr("192.0.2.64") // first host outside /26
		if matchCIDR(rule, client) {
			t.Error("expected no-match, got CIDR match")
		}
	})

	t.Run("zero client IP does not panic", func(t *testing.T) {
		rule := config.CIDRRule{Prefix: netip.MustParsePrefix("192.0.2.0/24")}
		var zero netip.Addr
		// Must not panic; result is don't-care as long as it returns false for zero.
		_ = matchCIDR(rule, zero)
	})
}
