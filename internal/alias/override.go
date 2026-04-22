// Package alias implements backup-zone resolution: exact-match, RFC 1034
// §3.6.2 CNAME fallback, and wildcard synthesis, with owner-name rewrites
// between the backup and root namespaces.
//
// The ephemeral TXT overlay is intentionally handled in the server layer
// (see internal/server/handler.go); this package does not import or depend
// on internal/ephemeral.
package alias

import (
	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// Resolve answers a backup-zone query. It performs an exact-match pass
// (backup override + root exact + root CNAME fallback) and then a wildcard
// pass. Kept as a thin wrapper over ResolveExact/ResolveWildcard so callers
// that don't need to interleave other lookups (e.g. ephemeral store) keep
// working unchanged.
//
// MUST NOT panic on any input.
func Resolve(qname string, qtype uint16, backupOrigin string, backupZone *zone.Zone, rootZone *zone.Zone) []dns.RR {
	if rootZone == nil {
		return nil
	}
	if rrs := ResolveExact(qname, qtype, backupOrigin, backupZone, rootZone); len(rrs) > 0 {
		return rrs
	}
	if rrs := ResolveWildcard(qname, qtype, backupOrigin, rootZone); len(rrs) > 0 {
		return rrs
	}
	return nil
}

// ResolveExactNoCNAME performs the strict exact-match portion of backup-zone
// resolution: backup override lookup for overridable types and root exact
// lookup with in-bailiwick rewrite. No CNAME fallback; no wildcard. Returns
// nil when no exact match exists. Split out so callers can interleave
// another exact source (e.g. the ephemeral TXT store) between exact-match
// and CNAME fallback.
//
// MUST NOT panic on any input.
func ResolveExactNoCNAME(qname string, qtype uint16, backupOrigin string, backupZone *zone.Zone, rootZone *zone.Zone) []dns.RR {
	if rootZone == nil {
		return nil
	}

	if dnsutil.OverridableTypes[qtype] && backupZone != nil {
		if overrides := backupZone.Lookup(qname, qtype); len(overrides) > 0 {
			return overrides
		}
	}

	rootQName := RewriteQName(qname, backupOrigin, rootZone.Origin)
	rootRRs := rootZone.Lookup(rootQName, qtype)
	if len(rootRRs) == 0 {
		return nil
	}

	return finalizeBackupRRs(rootRRs, qtype, rootZone, backupOrigin)
}

// ResolveCNAMEFallback performs only the RFC 1034 §3.6.2 CNAME-fallback
// branch of backup-zone resolution: look up a CNAME at the rewritten root
// qname and finalize (follow in-zone chain + namespace-rewrite). Returns
// nil for CNAME qtype (the fallback is only meaningful for non-CNAME
// qtypes) or when no CNAME exists at the qname.
//
// MUST NOT panic on any input.
func ResolveCNAMEFallback(qname string, qtype uint16, backupOrigin string, rootZone *zone.Zone) []dns.RR {
	if rootZone == nil || qtype == dns.TypeCNAME {
		return nil
	}

	rootQName := RewriteQName(qname, backupOrigin, rootZone.Origin)
	rootRRs := rootZone.Lookup(rootQName, dns.TypeCNAME)
	if len(rootRRs) == 0 {
		return nil
	}

	return finalizeBackupRRs(rootRRs, qtype, rootZone, backupOrigin)
}

// ResolveExact performs the exact-match portion of backup-zone resolution:
// backup override lookup, root exact lookup with in-bailiwick rewrite, and
// RFC 1034 §3.6.2 CNAME fallback. Wildcard synthesis is NOT consulted.
// Returns nil when no exact match exists.
//
// MUST NOT panic on any input.
func ResolveExact(qname string, qtype uint16, backupOrigin string, backupZone *zone.Zone, rootZone *zone.Zone) []dns.RR {
	if rrs := ResolveExactNoCNAME(qname, qtype, backupOrigin, backupZone, rootZone); len(rrs) > 0 {
		return rrs
	}
	return ResolveCNAMEFallback(qname, qtype, backupOrigin, rootZone)
}

// ResolveWildcard performs the wildcard-synthesis portion of backup-zone
// resolution. Returns nil when no wildcard covers the rewritten qname.
//
// MUST NOT panic on any input.
func ResolveWildcard(qname string, qtype uint16, backupOrigin string, rootZone *zone.Zone) []dns.RR {
	if rootZone == nil {
		return nil
	}

	rootQName := RewriteQName(qname, backupOrigin, rootZone.Origin)
	wRRs, wFound := rootZone.LookupWildcard(rootQName, qtype)
	if wFound && len(wRRs) == 0 && qtype != dns.TypeCNAME {
		wRRs, _ = rootZone.LookupWildcard(rootQName, dns.TypeCNAME)
	}
	if len(wRRs) == 0 {
		// wFound=true but no records of the requested type is a NODATA case.
		// We return nil here because the caller (handleBackupQuery) falls
		// through to negativeReply, which re-derives wildcard presence via
		// backupZoneHasName → rootZone.HasWildcard and sets RCODE=NOERROR
		// (NODATA) instead of NXDOMAIN. If that call chain is ever short-
		// circuited, this coupling must be revisited.
		return nil
	}

	// Rewrite wildcard owners from "*.<zone>" to rootQName so subsequent
	// CNAME following and final rewrite work uniformly.
	rootRRs := make([]dns.RR, len(wRRs))
	for i, rr := range wRRs {
		cp := dns.Copy(rr)
		cp.Header().Name = rootQName
		rootRRs[i] = cp
	}

	return finalizeBackupRRs(rootRRs, qtype, rootZone, backupOrigin)
}

// finalizeBackupRRs applies in-zone CNAME following (RFC 1034 §3.6.2) and
// then rewrites owners from the root namespace to the backup namespace.
func finalizeBackupRRs(rootRRs []dns.RR, qtype uint16, rootZone *zone.Zone, backupOrigin string) []dns.RR {
	if len(rootRRs) > 0 && qtype != dns.TypeCNAME {
		if _, ok := rootRRs[len(rootRRs)-1].(*dns.CNAME); ok {
			rootRRs = rootZone.FollowCNAME(nil, rootRRs, qtype)
		}
	}
	result := make([]dns.RR, 0, len(rootRRs))
	for _, rr := range rootRRs {
		result = append(result, RewriteRR(rr, rootZone.Origin, backupOrigin))
	}
	return result
}
