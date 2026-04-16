package alias

import (
	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// Resolve answers a backup-zone query. Overridable types (TXT/MX/SRV) are
// served from the backup zone when present; everything else falls through to
// the root zone with in-bailiwick rewrite applied.
//
// MUST NOT panic on any input.
func Resolve(qname string, qtype uint16, backupOrigin string, backupZone *zone.Zone, rootZone *zone.Zone) []dns.RR {
	if rootZone == nil {
		return []dns.RR{}
	}

	if dnsutil.OverridableTypes[qtype] && backupZone != nil {
		if overrides := backupZone.Lookup(qname, qtype); len(overrides) > 0 {
			return overrides
		}
	}

	rootQName := RewriteQName(qname, backupOrigin, rootZone.Origin)
	rootRRs := rootZone.Lookup(rootQName, qtype)

	// CNAME fallback per RFC 1034 §3.6.2 (root-zone path only;
	// backup overridable-type hits are returned early above).
	if len(rootRRs) == 0 && qtype != dns.TypeCNAME {
		rootRRs = rootZone.Lookup(rootQName, dns.TypeCNAME)
	}

	result := make([]dns.RR, 0, len(rootRRs))
	for _, rr := range rootRRs {
		result = append(result, RewriteRR(rr, rootZone.Origin, backupOrigin))
	}
	return result
}
