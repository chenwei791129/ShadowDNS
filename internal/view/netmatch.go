package view

import (
	"net/netip"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// matchIP returns true iff rule.IP equals clientIP.
func matchIP(rule config.IPRule, clientIP netip.Addr) bool {
	return rule.IP == clientIP
}

// matchCIDR returns true iff rule.Prefix contains clientIP.
func matchCIDR(rule config.CIDRRule, clientIP netip.Addr) bool {
	return rule.Prefix.Contains(clientIP)
}
