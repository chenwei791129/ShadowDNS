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
type Zone struct {
	Origin  string              // FQDN with trailing dot, lowercased
	Path    string              // absolute path of the source file
	SOA     *dns.SOA            // nil only for backup-override zones without their own SOA
	Records map[string][]dns.RR // owner name (lowercased FQDN) → records; SOA also appears here under Origin
	Role    Role                // set by classify (task 3.3); RoleUnknown until classified
}

// AddRR inserts an RR into the index using the (lowercased) RR.Header().Name as key.
// Used by ParseFile internally; can also be used by tests.
func (z *Zone) AddRR(rr dns.RR) {
	if z.Records == nil {
		z.Records = make(map[string][]dns.RR)
	}
	key := strings.ToLower(rr.Header().Name)
	z.Records[key] = append(z.Records[key], rr)

	// Cache SOA reference for quick access.
	if soa, ok := rr.(*dns.SOA); ok {
		z.SOA = soa
	}
}

// LookupWildcard attempts wildcard matching per RFC 4592 when exact lookup
// fails. Starting from qname it strips the leftmost label and checks for a
// wildcard owner "*.<parent>" at each level. If a wildcard is found, the
// matching records are filtered by qtype and returned with found=true. If an
// existing name (empty non-terminal) is encountered during traversal, the
// search stops and returns found=false. The search stops when parent equals
// the zone origin.
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
		if wRRs, ok := z.Records[wildcard]; ok {
			return filterByQtype(wRRs, qtype), true
		}

		// ENT blocker: if the parent name itself has records, stop.
		if _, exists := z.Records[parent]; exists {
			return nil, false
		}

		name = parent
	}

	// Check wildcard at the zone origin level: "*.<origin>".
	wildcard := "*." + z.Origin
	if wRRs, ok := z.Records[wildcard]; ok {
		return filterByQtype(wRRs, qtype), true
	}

	return nil, false
}

// HasWildcard reports whether any wildcard owner covers qname per RFC 4592,
// regardless of record type. It is a convenience wrapper around LookupWildcard
// for callers that only need to know if the name falls under a wildcard (e.g.,
// to distinguish NODATA from NXDOMAIN).
func (z *Zone) HasWildcard(qname string) bool {
	_, found := z.LookupWildcard(qname, 0)
	return found
}

// filterByQtype returns only the RRs matching the given query type.
func filterByQtype(rrs []dns.RR, qtype uint16) []dns.RR {
	result := make([]dns.RR, 0, len(rrs))
	for _, rr := range rrs {
		if rr.Header().Rrtype == qtype {
			result = append(result, rr)
		}
	}
	return result
}

// Lookup returns all records at owner with the given qtype, or an empty (non-nil) slice.
// owner is expected to be canonicalized (lowercased + trailing dot) by the caller.
func (z *Zone) Lookup(owner string, qtype uint16) []dns.RR {
	rrs, ok := z.Records[owner]
	if !ok {
		return []dns.RR{}
	}
	return filterByQtype(rrs, qtype)
}
