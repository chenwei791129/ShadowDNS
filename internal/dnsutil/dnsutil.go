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

// Canonicalize returns the FQDN form of a DNS name (with trailing dot), preserving
// the original case of every label byte-for-byte. It only normalizes the trailing
// dot. Empty input returns "".
//
// Per RFC 4343, DNS name comparisons are case-insensitive, but on-wire names should
// be transmitted with their original case preserved (BIND9, Knot, NSD, PowerDNS all
// behave this way). Use LookupKey for case-folded comparisons / map keys.
func Canonicalize(name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSuffix(name, ".") + "."
}

// LookupKey returns the lowercase-folded FQDN form of a DNS name (with trailing
// dot), suitable as a case-insensitive comparison key per RFC 4343. Empty input
// returns "". Use this for map keys and equality checks; use Canonicalize for
// stored / output names where case must be preserved.
func LookupKey(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}

// IsInZone returns true iff name equals zone or is a subdomain of zone.
// Both arguments MUST already be lowercase-folded via LookupKey for correct
// case-insensitive matching.
func IsInZone(name, zone string) bool {
	return name == zone || strings.HasSuffix(name, "."+zone)
}

// IsUDP returns true when the writer's local address is a UDP socket.
func IsUDP(w dns.ResponseWriter) bool {
	_, ok := w.LocalAddr().(*net.UDPAddr)
	return ok
}
