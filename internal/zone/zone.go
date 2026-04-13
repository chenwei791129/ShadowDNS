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

// Lookup returns all records at owner with the given qtype, or an empty (non-nil) slice.
// owner is expected to be canonicalized (lowercased + trailing dot) by the caller.
func (z *Zone) Lookup(owner string, qtype uint16) []dns.RR {
	rrs, ok := z.Records[owner]
	if !ok {
		return []dns.RR{}
	}
	result := make([]dns.RR, 0, len(rrs))
	for _, rr := range rrs {
		if rr.Header().Rrtype == qtype {
			result = append(result, rr)
		}
	}
	return result
}
