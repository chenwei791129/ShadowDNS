package alias

import (
	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// Match is the result of resolving a query name to a loaded zone.
type Match struct {
	MatchedZone string // FQDN of the loaded zone whose suffix matches qname
	IsBackup    bool   // true when MatchedZone is a backup in aliasMap
	RootZone    string // when IsBackup, the root zone target; otherwise == MatchedZone
}

// Detect returns the longest-suffix zone among loadedZones whose suffix matches qname.
// Both loadedZones and qname MUST be lowercased FQDNs ending with ".".
//
// Returns Match{} (zero value, MatchedZone=="") when no zone matches.
//
// MUST NOT panic on any input.
func Detect(qname string, loadedZones []string, aliases config.AliasMap) Match {
	if qname == "" {
		return Match{}
	}

	// Whether qname carries an RFC 1035 escape sequence is loop-invariant
	// (qname is fixed across zones), so decide it once outside the loop. The
	// common backslash-free path then calls the inlinable IsInZoneByteSuffix
	// directly — IsInZone is not inlinable (its dns.IsSubDomain branch exceeds
	// the budget), and this loop runs over every loaded zone (potentially tens
	// of thousands) per query, so an un-inlined call here is a measurable
	// throughput regression.
	escaped := dnsutil.HasEscape(qname)

	// Find the longest-suffix match among loaded zones.
	best := ""
	for _, z := range loadedZones {
		if z == "" {
			continue
		}
		// qname matches zone z when qname == z or qname ends with "." + z
		// (e.g., z="root.com.", qname="www.root.com." ends with ".root.com.").
		var match bool
		if escaped {
			match = dnsutil.IsInZone(qname, z)
		} else {
			match = dnsutil.IsInZoneByteSuffix(qname, z)
		}
		if match && len(z) > len(best) {
			best = z
		}
	}

	if best == "" {
		return Match{}
	}

	// Consult alias map to determine role.
	if rootZone, ok := aliases[best]; ok {
		return Match{
			MatchedZone: best,
			IsBackup:    true,
			RootZone:    rootZone,
		}
	}

	return Match{
		MatchedZone: best,
		IsBackup:    false,
		RootZone:    best,
	}
}
