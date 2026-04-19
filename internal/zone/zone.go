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
// Records maps owner name (lowercased FQDN) to a qtypeStore that holds the
// owner's RRs indexed by qtype. The store has two states: inline for owners
// with a single qtype (avoids allocating a sub-map), and promoted for owners
// with two or more qtypes. Lookup and LookupWildcard return the stored slice
// directly without copying. Callers MUST NOT mutate the returned slice.
type Zone struct {
	Origin  string
	Path    string
	SOA     *dns.SOA
	Records map[string]*qtypeStore
	Role    Role
}

// qtypeStore holds the RRs at one owner, either inline (single qtype) or
// promoted (multiple qtypes). Inline state: single=true, qtype and rrs hold
// the owner's only qtype and its records, sub is nil. Promoted state:
// single=false, sub holds every qtype's records, qtype/rrs are unused.
// Promotion is one-way (inline→promoted) on the first AddRR of a new qtype;
// there is no demotion path.
type qtypeStore struct {
	single bool
	qtype  uint16
	rrs    []dns.RR
	sub    map[uint16][]dns.RR
}

// Each invokes fn once per (qtype, rrs) pair stored at this owner. Safe to
// call on a nil receiver (no-op).
func (s *qtypeStore) Each(fn func(qtype uint16, rrs []dns.RR)) {
	if s == nil {
		return
	}
	if s.single {
		fn(s.qtype, s.rrs)
		return
	}
	for q, r := range s.sub {
		fn(q, r)
	}
}

// AddRR inserts an RR into the index under (owner, qtype).
// Used by ParseFile internally; can also be used by tests.
func (z *Zone) AddRR(rr dns.RR) {
	if z.Records == nil {
		z.Records = make(map[string]*qtypeStore)
	}
	key := strings.ToLower(rr.Header().Name)
	qtype := rr.Header().Rrtype

	s, ok := z.Records[key]
	switch {
	case !ok:
		z.Records[key] = &qtypeStore{single: true, qtype: qtype, rrs: []dns.RR{rr}}
	case s.single && s.qtype == qtype:
		s.rrs = append(s.rrs, rr)
	case s.single:
		// Promote: re-install the inline slice into the sub-map under its
		// original qtype so that any slice previously returned by Lookup keeps
		// a shared backing array with the promoted storage.
		s.sub = map[uint16][]dns.RR{s.qtype: s.rrs, qtype: {rr}}
		s.single = false
		s.qtype = 0
		s.rrs = nil
	default:
		s.sub[qtype] = append(s.sub[qtype], rr)
	}

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
		if s, ok := z.Records[wildcard]; ok {
			return wildcardHit(s, qtype)
		}

		// ENT blocker: if the parent name itself has records, stop.
		if _, exists := z.Records[parent]; exists {
			return nil, false
		}

		name = parent
	}

	// Check wildcard at the zone origin level: "*.<origin>".
	wildcard := "*." + z.Origin
	if s, ok := z.Records[wildcard]; ok {
		return wildcardHit(s, qtype)
	}

	return nil, false
}

// wildcardHit returns the stored RR list for a matched wildcard owner's store.
// qtype == 0 is the "any qtype / existence check" sentinel used by HasWildcard.
func wildcardHit(s *qtypeStore, qtype uint16) ([]dns.RR, bool) {
	if qtype == 0 {
		if s == nil || (!s.single && len(s.sub) == 0) {
			return nil, false
		}
		return nil, true
	}
	if s == nil {
		return nil, true
	}
	if s.single {
		if s.qtype == qtype {
			return s.rrs, true
		}
		return nil, true
	}
	return s.sub[qtype], true
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
	s, ok := z.Records[owner]
	if !ok {
		return nil
	}
	if s.single {
		if s.qtype == qtype {
			return s.rrs
		}
		return nil
	}
	return s.sub[qtype]
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
