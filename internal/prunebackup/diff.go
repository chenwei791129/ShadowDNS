package prunebackup

import (
	"slices"
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// overridableTypes mirrors dnsutil.OverridableTypes. The copy is intentional:
// if the runtime overridable set ever widens, the prune rules need manual
// review (deleting what is now an overlay intent would be silently wrong),
// so we prefer a visible local constant over a transparent import. A test
// in diff_test.go pins the two sets together.
var overridableTypes = map[uint16]bool{
	dns.TypeTXT: true,
	dns.TypeMX:  true,
	dns.TypeSRV: true,
}

// rrsetKey identifies one RRSet by canonical owner + rtype. Class is always
// IN across this codebase, so it is omitted from the key.
type rrsetKey struct {
	Owner string
	Rtype uint16
}

// rrsetIndex groups RRs by (owner, rtype) with canonicalized owner names.
type rrsetIndex map[rrsetKey][]dns.RR

// buildRRSetIndex bins RRs into an rrsetIndex. Owner names are folded to the
// lookup key form (lowercased FQDN with trailing dot) so lookups are
// case-insensitive per RFC 4343.
func buildRRSetIndex(rrs []dns.RR) rrsetIndex {
	idx := make(rrsetIndex)
	for _, rr := range rrs {
		key := rrsetKey{
			Owner: dnsutil.LookupKey(rr.Header().Name),
			Rtype: rr.Header().Rrtype,
		}
		idx[key] = append(idx[key], rr)
	}
	return idx
}

// decision is the outcome of classify for one RRSet.
type decision int

const (
	decisionRetain decision = iota
	decisionDelete
)

// classifyWithoutRoot is the type-only prune decision. SOA and apex NS
// retain because they are RFC 1035 mandated zone-file infrastructure;
// overridable types (TXT/MX/SRV) retain because byte-equality against
// root cannot be evaluated without a root zone. Everything else deletes.
// owner and origin MUST be lookup-folded.
func classifyWithoutRoot(owner string, rtype uint16, origin string) decision {
	if rtype == dns.TypeSOA {
		return decisionRetain
	}
	if rtype == dns.TypeNS && owner == origin {
		return decisionRetain
	}
	if !overridableTypes[rtype] {
		return decisionDelete
	}
	return decisionRetain
}

// classify applies the prune rules to one (owner, rtype) RRSet. owner and
// origin MUST already be lookup-folded (lowercased FQDN with trailing dot
// via dnsutil.LookupKey); callers are expected to normalise before invoking.
func classify(backupRRSet, rootRRSet []dns.RR, owner string, rtype uint16, origin string) decision {
	if d := classifyWithoutRoot(owner, rtype, origin); d == decisionDelete {
		return decisionDelete
	}
	// d == decisionRetain. For overridable types the byte-equality check
	// against root may flip retain to delete. SOA and apex NS are not
	// overridable, so they fall straight through to retain.
	if overridableTypes[rtype] && rrsetEqual(backupRRSet, rootRRSet) {
		return decisionDelete
	}
	return decisionRetain
}

// rrsetEqual returns true when a and b are equal as sets of canonical
// (class, rdata) tuples — ignoring TTL, ignoring order, ignoring owner
// name. Class is implied IN.
func rrsetEqual(a, b []dns.RR) bool {
	if len(a) != len(b) {
		return false
	}
	ka := canonicalRDataList(a)
	kb := canonicalRDataList(b)
	slices.Sort(ka)
	slices.Sort(kb)
	return slices.Equal(ka, kb)
}

// canonicalRDataList reduces each RR to its rdata form by stripping the
// header prefix. Type is implicit in the grouping (rrsetEqual is only
// called on two RRSets sharing the same (owner, type)), so dropping type
// alongside owner/TTL/class is safe. miekg's RR_Header.String() is the
// exact header prefix of rr.String(), giving a reliable strip with no
// fragile field counting.
func canonicalRDataList(rrs []dns.RR) []string {
	out := make([]string, 0, len(rrs))
	for _, rr := range rrs {
		out = append(out, strings.TrimPrefix(rr.String(), rr.Header().String()))
	}
	return out
}
