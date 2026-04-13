// Package dnsutil holds DNS-related helpers shared across shadowdns packages.
package dnsutil

import (
	"net"
	"strings"

	"github.com/miekg/dns"
)

// OverridableTypes is the set of RR types that backup zones may override
// independently of their root zone (TXT, MX, SRV).
var OverridableTypes = map[uint16]bool{
	dns.TypeTXT: true,
	dns.TypeMX:  true,
	dns.TypeSRV: true,
}

// Canonicalize returns the lowercased FQDN form of a DNS name (with trailing dot).
// Empty input returns "".
func Canonicalize(name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(name), ".") + "."
}

// IsInZone returns true iff name equals zone or is a subdomain of zone.
// Both arguments MUST already be canonicalized via Canonicalize (lowercased FQDN).
func IsInZone(name, zone string) bool {
	return name == zone || strings.HasSuffix(name, "."+zone)
}

// IsUDP returns true when the writer's local address is a UDP socket.
func IsUDP(w dns.ResponseWriter) bool {
	_, ok := w.LocalAddr().(*net.UDPAddr)
	return ok
}
