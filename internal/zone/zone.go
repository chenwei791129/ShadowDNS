package zone

import (
	"strings"

	"github.com/miekg/dns"
)

// Role classifies a zone's purpose within the ShadowDNS system.
type Role int

const (
	// RoleUnknown is the initial state before Classify is called.
	RoleUnknown Role = iota
	// RoleRoot is a normal zone with complete data.
	RoleRoot
	// RoleBackupOverride is a backup zone holding only TXT/MX/SRV overrides.
	RoleBackupOverride
)

// Zone is the in-memory representation of a parsed DNS zone file.
//
// Records is a two-level index: owner name (lowercased FQDN) → qtype → RR list.
// The inner slice is stored by reference; Lookup and LookupWildcard return it
// directly without copying. Callers MUST NOT mutate the returned slice.
type Zone struct {
	Origin  string
	Path    string
	SOA     *dns.SOA
	Records map[string]map[uint16][]dns.RR
	Role    Role
}

// AddRR inserts an RR into the index under (owner, qtype).
// Used by ParseFile internally; can also be used by tests.
func (z *Zone) AddRR(rr dns.RR) {
	if z.Records == nil {
		z.Records = make(map[string]map[uint16][]dns.RR)
	}
	key := strings.ToLower(rr.Header().Name)
	qtype := rr.Header().Rrtype

	sub, ok := z.Records[key]
	if !ok {
		sub = make(map[uint16][]dns.RR)
		z.Records[key] = sub
	}
	sub[qtype] = append(sub[qtype], rr)

	// Cache SOA reference for quick access.
	if soa, ok := rr.(*dns.SOA); ok {
		z.SOA = soa
	}
}

// LookupWildcard attempts wildcard matching per RFC 4592 when exact lookup
// fails. Starting from qname it strips the leftmost label and checks for a
// wildcard owner "*.<parent>" at each level. If a wildcard owner exists, the
// RRs stored at (wildcard, qtype) are returned with found=true (the slice may
// be empty when the wildcard has no records of that qtype — a NODATA signal).
// If an existing name (empty non-terminal) is encountered during traversal,
// the search stops and returns found=false. The search stops when parent
// equals the zone origin.
//
// When qtype == 0, the return value is non-nil iff the wildcard owner exists
// with at least one record of any qtype — used by HasWildcard as an existence
// probe.
//
// Returns the stored slice as a direct reference. Callers MUST NOT mutate the
// returned slice (no element assignment, no append that shares its capacity,
// no sort) — doing so corrupts the zone's internal state.
//
// The returned RRs still carry the wildcard owner name ("*." prefix); the
// caller is responsible for rewriting the owner to the original qname.
func (z *Zone) LookupWildcard(qname string, qtype uint16) ([]dns.RR, bool) {
	if qname == z.Origin {
		return nil, false
	}

	originSuffix := "." + z.Origin

	// Walk up the label tree from qname toward the zone origin.
	name := qname
	for {
		idx := strings.Index(name, ".")
		if idx < 0 {
			break
		}
		parent := name[idx+1:]

		if parent == z.Origin {
			break
		}

		// Guard: stop if we've traversed past the zone origin.
		if !strings.HasSuffix(parent, originSuffix) {
			return nil, false
		}

		wildcard := "*." + parent
		if sub, ok := z.Records[wildcard]; ok {
			return wildcardHit(sub, qtype)
		}

		// ENT blocker: if the parent name itself has records, stop.
		if _, exists := z.Records[parent]; exists {
			return nil, false
		}

		name = parent
	}

	// Check wildcard at the zone origin level: "*.<origin>".
	wildcard := "*." + z.Origin
	if sub, ok := z.Records[wildcard]; ok {
		return wildcardHit(sub, qtype)
	}

	return nil, false
}

// wildcardHit returns the stored RR list for a matched wildcard owner's sub-map.
// qtype == 0 is the "any qtype / existence check" sentinel used by HasWildcard.
func wildcardHit(sub map[uint16][]dns.RR, qtype uint16) ([]dns.RR, bool) {
	if qtype == 0 {
		if len(sub) == 0 {
			return nil, false
		}
		return nil, true
	}
	return sub[qtype], true
}

// HasWildcard reports whether any wildcard owner covers qname per RFC 4592,
// regardless of record type. It is a convenience wrapper around LookupWildcard
// for callers that only need to know if the name falls under a wildcard (e.g.,
// to distinguish NODATA from NXDOMAIN).
func (z *Zone) HasWildcard(qname string) bool {
	_, found := z.LookupWildcard(qname, 0)
	return found
}

// Lookup returns all records at owner with the given qtype.
//
// Returns the stored slice as a direct reference. Callers MUST NOT mutate the
// returned slice (no element assignment, no append that shares its capacity,
// no sort) — doing so corrupts the zone's internal state. When the owner or
// qtype has no records, the return value is nil; callers SHOULD check len()
// rather than !=nil.
//
// owner is expected to be canonicalized (lowercased + trailing dot) by the caller.
func (z *Zone) Lookup(owner string, qtype uint16) []dns.RR {
	sub, ok := z.Records[owner]
	if !ok {
		return nil
	}
	return sub[qtype]
}

// MaxCNAMEDepth limits CNAME chain following to prevent infinite loops
// from circular zone configurations.
const MaxCNAMEDepth = 8

// FollowCNAME follows in-zone CNAME targets per RFC 1034 §3.6.2.
// Starting from the initial CNAME record(s), it checks whether each target
// is within the same zone (in-bailiwick). If so, it looks up the target for
// qtype and appends the results. If the target is itself a CNAME, the process
// repeats up to MaxCNAMEDepth total records.
//
// dst is an optional caller-provided buffer to append results into; pass nil
// to allocate a fresh slice with capacity MaxCNAMEDepth+1. When dst is
// non-nil, the caller must use only the returned slice afterwards; dst and
// the result share the underlying array.
func (z *Zone) FollowCNAME(dst []dns.RR, initial []dns.RR, qtype uint16) []dns.RR {
	answer := dst
	if answer == nil {
		answer = make([]dns.RR, 0, MaxCNAMEDepth+1)
	}
	answer = append(answer, initial...)

	originSuffix := "." + z.Origin

	for range MaxCNAMEDepth - len(initial) {
		last, ok := answer[len(answer)-1].(*dns.CNAME)
		if !ok {
			break
		}
		target := strings.ToLower(last.Target)

		if target != z.Origin && !strings.HasSuffix(target, originSuffix) {
			break
		}

		if rrs := z.Lookup(target, qtype); len(rrs) > 0 {
			answer = append(answer, rrs...)
			break
		}

		if cnames := z.Lookup(target, dns.TypeCNAME); len(cnames) > 0 {
			answer = append(answer, cnames...)
			continue
		}

		wRRs, wFound := z.LookupWildcard(target, qtype)
		if wFound && len(wRRs) > 0 {
			answer = append(answer, copyWithOwner(wRRs, target)...)
			break
		}

		if wCNAMEs, _ := z.LookupWildcard(target, dns.TypeCNAME); len(wCNAMEs) > 0 {
			answer = append(answer, copyWithOwner(wCNAMEs, target)...)
			continue
		}

		break
	}

	return answer
}

// copyWithOwner returns deep copies of rrs with owner name set to name.
func copyWithOwner(rrs []dns.RR, name string) []dns.RR {
	result := make([]dns.RR, len(rrs))
	for i, rr := range rrs {
		cp := dns.Copy(rr)
		cp.Header().Name = name
		result[i] = cp
	}
	return result
}
