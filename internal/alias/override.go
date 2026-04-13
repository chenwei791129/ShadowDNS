package alias

import (
	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// Resolve answers a backup-zone query: when qtype is overridable (TXT/MX/SRV)
// and the backup zone supplies records at qname, those override any inherited
// data; otherwise the root zone is consulted with qname rewritten into the
// root namespace, and each resulting record is rewritten back to backupOrigin.
//
//   - qname:        original (backup-namespace) query name, lowercased FQDN
//   - qtype:        DNS query type
//   - backupOrigin: the backup zone's origin (match.MatchedZone)
//   - backupZone:   loaded backup-override zone, or nil if no .fwd exists
//   - rootZone:     the root zone whose data is shared
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

	result := make([]dns.RR, 0, len(rootRRs))
	for _, rr := range rootRRs {
		result = append(result, RewriteRR(rr, rootZone.Origin, backupOrigin))
	}
	return result
}
