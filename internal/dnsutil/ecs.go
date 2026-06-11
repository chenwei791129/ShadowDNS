package dnsutil

import (
	"net"
	"net/netip"

	"github.com/miekg/dns"
)

// ECSClass is the handler-layer classification of an EDNS Client Subnet
// option found in a query (RFC 7871). A distinct type (rather than bools)
// keeps call sites explicit and prevents silent misclassification.
type ECSClass int

const (
	// ECSMalformed is the zero value on purpose: anything the classifier
	// does not positively recognize is denied by default.
	ECSMalformed ECSClass = iota
	// ECSValid means the option carries a usable client subnet; the geo
	// lookup address returned alongside is meaningful only for this class.
	ECSValid
	// ECSOptOut means the client sent SOURCE PREFIX-LENGTH 0 to opt out of
	// subnet-based answers (the `dig +subnet=0` form).
	ECSOptOut
)

// familyBits returns the address width in bits for an ECS FAMILY, or 0 for
// families outside the RFC 7871 address registry. Single source of
// family-shape truth for this file.
func familyBits(family uint16) int {
	switch family {
	case 1:
		return 32 // IPv4
	case 2:
		return 128 // IPv6
	}
	return 0
}

// ClassifyECS classifies a query's ECS option per RFC 7871 section 7.1.1 and
// returns the geo-lookup address for ECSValid options (zero netip.Addr
// otherwise). It is a total function: every representable input — including
// directly-constructed values that dns msg unpacking would reject — yields a
// classification without panicking. Malformed checks take precedence over
// the opt-out classification.
func ClassifyECS(e *dns.EDNS0_SUBNET) (ECSClass, netip.Addr) {
	if e == nil {
		return ECSMalformed, netip.Addr{}
	}
	// RFC 7871 mandates SCOPE PREFIX-LENGTH 0 in queries.
	if e.SourceScope != 0 {
		return ECSMalformed, netip.Addr{}
	}

	// FAMILY 0 is only well-formed as the opt-out shape: prefix 0 and an
	// all-zero (or absent) address. IsUnspecified, not a raw byte scan: the
	// library's unpack delivers FAMILY 0 addresses as the 16-byte v4-mapped
	// net.IPv4zero, whose mapping bytes are non-zero.
	if e.Family == 0 {
		if e.SourceNetmask == 0 && (len(e.Address) == 0 || e.Address.IsUnspecified()) {
			return ECSOptOut, netip.Addr{}
		}
		return ECSMalformed, netip.Addr{}
	}

	bits := familyBits(e.Family)
	if bits == 0 || int(e.SourceNetmask) > bits {
		return ECSMalformed, netip.Addr{}
	}

	// An absent address carries no non-zero bits: opt-out for prefix 0,
	// malformed for any longer prefix (no bits to cover it).
	if len(e.Address) == 0 {
		if e.SourceNetmask == 0 {
			return ECSOptOut, netip.Addr{}
		}
		return ECSMalformed, netip.Addr{}
	}

	// The address is a raw net.IP that may have any length when directly
	// constructed, so normalize defensively rather than trusting unpack
	// invariants.
	addr, ok := familyAddr(e.Family, e.Address)
	if !ok {
		return ECSMalformed, netip.Addr{}
	}
	// RFC 7871: address bits beyond SOURCE PREFIX-LENGTH must be zero.
	// Masked zeroes them, so any difference means a stray bit was set. With
	// prefix 0 every bit is beyond the prefix, making a non-zero address
	// malformed rather than opt-out.
	if netip.PrefixFrom(addr, int(e.SourceNetmask)).Masked().Addr() != addr {
		return ECSMalformed, netip.Addr{}
	}
	if e.SourceNetmask == 0 {
		return ECSOptOut, netip.Addr{}
	}
	// Unmap canonicalizes a v4-mapped FAMILY 2 address into plain IPv4,
	// keeping byte-equivalence with the source-IP path (addrFromRemote also
	// unmaps) so geo lookups see one canonical form per subnet.
	return ECSValid, addr.Unmap()
}

// EchoECS builds the response ECS option for a query option q: FAMILY,
// SOURCE PREFIX-LENGTH and ADDRESS are echoed verbatim and SCOPE
// PREFIX-LENGTH is set to scope (RFC 7871 section 7.1.3). The returned
// option aliases q.Address rather than copying it — the request is owned by
// the handler goroutine and is not read after response assembly (the same
// invariant attachOPT relies on to reuse the request's OPT record), so the
// alias saves a per-query allocation.
func EchoECS(q *dns.EDNS0_SUBNET, scope uint8) *dns.EDNS0_SUBNET {
	resp := &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		Family:        q.Family,
		SourceNetmask: q.SourceNetmask,
		SourceScope:   scope,
		Address:       q.Address,
	}
	if len(q.Address) == 0 {
		// A directly-constructed opt-out option may carry a nil address, but
		// the dns library refuses to pack FAMILY 1/2 options without one. The
		// zero sentinel packs to the same empty wire form (the address field
		// is truncated to ceil(prefix/8) bytes).
		switch q.Family {
		case 1:
			resp.Address = net.IPv4zero
		case 2:
			resp.Address = net.IPv6zero
		}
	}
	return resp
}

// familyAddr converts a raw ECS address to a netip.Addr of the width the
// family dictates. Length mismatches (e.g. a 4-byte address with FAMILY 2,
// or a non-IPv4-mappable 16-byte address with FAMILY 1) report !ok.
func familyAddr(family uint16, ip net.IP) (netip.Addr, bool) {
	switch family {
	case 1:
		ip4 := ip.To4()
		if ip4 == nil {
			return netip.Addr{}, false
		}
		return netip.AddrFromSlice(ip4)
	case 2:
		if len(ip) != net.IPv6len {
			return netip.Addr{}, false
		}
		return netip.AddrFromSlice(ip)
	default:
		return netip.Addr{}, false
	}
}
