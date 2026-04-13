package alias

import (
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// Resolve performs the full backup-zone query: it first checks the backup zone
// for an override at (qname, qtype) when qtype is TXT/MX/SRV; if no override
// exists OR qtype is not in that set, it falls back to looking up in the root
// zone using the rewritten qname and applies in-bailiwick rewrite to each
// record before returning.
//
//   - qname:      the original (backup) query name, lowercased FQDN
//   - qtype:      the query type (e.g. dns.TypeA, dns.TypeTXT)
//   - backupZone: the loaded backup-override zone (may be nil if backup has no own .fwd)
//   - rootZone:   the root zone whose data is shared
//
// Returns the answer records ready to send (already rewritten).
//
// MUST NOT panic on any input.
func Resolve(qname string, qtype uint16, backupZone *zone.Zone, rootZone *zone.Zone) []dns.RR {
	if rootZone == nil {
		return []dns.RR{}
	}

	// Check for a backup override when qtype is overridable and a backup zone is loaded.
	if dnsutil.OverridableTypes[qtype] && backupZone != nil {
		overrides := backupZone.Lookup(qname, qtype)
		if len(overrides) > 0 {
			return overrides
		}
	}

	// Fall back to root zone: rewrite qname from backup namespace to root namespace,
	// then look up in the root zone and rewrite each resulting record back to backup.
	rootQName := rewriteToRoot(qname, backupZone, rootZone)
	rootRRs := rootZone.Lookup(rootQName, qtype)

	// Determine backup and root origins for the in-bailiwick rewrite.
	backupOrigin := inferBackupOrigin(qname, backupZone, rootZone)

	result := make([]dns.RR, 0, len(rootRRs))
	for _, rr := range rootRRs {
		result = append(result, RewriteRR(rr, rootZone.Origin, backupOrigin))
	}
	return result
}

// rewriteToRoot maps a qname from the backup namespace to the root namespace.
// It infers the backup origin from backupZone if available, otherwise from qname itself.
func rewriteToRoot(qname string, backupZone *zone.Zone, rootZone *zone.Zone) string {
	backupOrigin := inferBackupOrigin(qname, backupZone, rootZone)
	return RewriteQName(qname, backupOrigin, rootZone.Origin)
}

// inferBackupOrigin determines the backup zone origin to use for suffix rewriting.
// When backupZone is non-nil its Origin is used; otherwise the longest suffix of
// qname that ends with "." + rootZone.Origin (or equals rootZone.Origin) is used.
// As a final fallback the rootZone.Origin is returned, which makes RewriteQName a no-op.
func inferBackupOrigin(qname string, backupZone *zone.Zone, rootZone *zone.Zone) string {
	if backupZone != nil {
		return backupZone.Origin
	}
	// No explicit backup zone: derive backup origin from qname by stripping the
	// root suffix.  The caller is expected to pass a qname that already has the
	// backup suffix, so we do a best-effort suffix scan.
	//
	// E.g. qname="www.backup.com.", rootZone.Origin="root.com."
	// We cannot know "backup.com." without a backupZone, so we must reconstruct
	// it from the qname difference.
	//
	// Strategy: strip the deepest label(s) that do NOT appear in rootZone.Origin.
	// Simplest safe approach: strip the root suffix from qname to get the host part,
	// then remove the host part to get the backup origin.
	//
	// But without a backupZone we can only infer the backup origin if qname happens
	// to end with something we can map.  The real information is in the caller (the
	// Match result). Since Resolve's signature does not carry Match, we approximate:
	//
	//   Given qname = "www.backup.com." and rootZone.Origin = "root.com.",
	//   the host part is "www" (common to both), so backup origin ≈ qname without
	//   the host part.
	//
	// A robust approach: find the label count difference between qname and root origin,
	// then the backup origin = the last (count-of-root-labels) + 1 labels of qname.
	rootLabels := strings.Split(strings.TrimSuffix(rootZone.Origin, "."), ".")
	qLabels := strings.Split(strings.TrimSuffix(qname, "."), ".")

	// The backup zone covers exactly the same label depth as root.
	backupDepth := len(rootLabels)
	if len(qLabels) < backupDepth {
		return rootZone.Origin
	}
	backupParts := qLabels[len(qLabels)-backupDepth:]
	return strings.Join(backupParts, ".") + "."
}
