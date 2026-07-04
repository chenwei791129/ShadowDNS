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
	if isAlreadyLookupKey(name) {
		return name
	}
	return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}

// isAlreadyLookupKey reports whether s is already in lookup-fold form.
// Production zone data hits this branch nearly 100% of the time; a non-ASCII
// byte or uppercase letter forces the allocation path in LookupKey.
func isAlreadyLookupKey(s string) bool {
	n := len(s)
	if n == 0 || s[n-1] != '.' {
		return false
	}
	for i := 0; i < n-1; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			return false
		}
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// HasEscape reports whether name contains an RFC 1035 escape sequence (a
// backslash). A name with no backslash provably contains no escaped dot, so
// byte-level label logic — suffix comparison, "."-scan splitting — is exactly
// equivalent to a full label walk for it. Callers use this to gate an
// allocation-free fast path and fall back to escaping-aware label walks only
// when an escape may be present. Such names are rare in real traffic.
func HasEscape(name string) bool {
	return strings.IndexByte(name, '\\') >= 0
}

// IsInZone returns true iff name equals zone or is a subdomain of zone.
// Both arguments MUST already be lowercase-folded via LookupKey for correct
// case-insensitive matching.
//
// The common backslash-free case uses the allocation-free byte-suffix
// comparison IsInZoneByteSuffix. When name carries an escape sequence (see
// HasEscape), an escaped dot could sit inside a label, so membership is decided
// with dns.IsSubDomain, whose label walk honors RFC 1035 escaping and reports
// true when name equals zone as well.
//
// IsInZone itself is not inlinable (the dns.IsSubDomain call exceeds the
// budget) — hot loops that test one backslash-free name against many zones
// should hoist the HasEscape check and call IsInZoneByteSuffix directly (see
// alias.Detect).
//
// Root-origin zones (".") are routed through the byte path for BOTH escaped and
// non-escaped names: an escaped dot never changes whether a name sits under
// root, and the byte contract (root membership is false unless name == ".") is
// the established behavior every caller relies on. Routing root through
// dns.IsSubDomain (which reports every name as a subdomain of root) would flip
// that to true and silently change record loading and zone attribution for
// every caller — out of scope for an escaped-dot correctness fix.
func IsInZone(name, zone string) bool {
	if zone != "." && HasEscape(name) {
		return dns.IsSubDomain(zone, name)
	}
	return IsInZoneByteSuffix(name, zone)
}

// IsInZoneByteSuffix is the allocation-free, inline-friendly byte-suffix zone
// membership test for names KNOWN to be free of RFC 1035 escape sequences
// (HasEscape(name) == false). It returns true iff name equals zone or is a
// byte-suffix subdomain at a "." boundary. It is exported so hot multi-zone
// loops (alias.Detect scans every loaded zone per query) can call it directly
// and keep the comparison inlined — IsInZone cannot inline because of its
// dns.IsSubDomain branch. Results match IsInZone for backslash-free names and
// are unspecified for names containing a backslash (the caller must gate on
// HasEscape first).
func IsInZoneByteSuffix(name, zone string) bool {
	if name == zone {
		return true
	}
	offset := len(name) - len(zone)
	return offset > 0 && name[offset-1] == '.' && name[offset:] == zone
}

// IsUDP returns true when the writer's local address is a UDP socket.
func IsUDP(w dns.ResponseWriter) bool {
	_, ok := w.LocalAddr().(*net.UDPAddr)
	return ok
}
