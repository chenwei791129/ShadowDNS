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
// rewriteRDATALabels controls whether RDATA name fields are rewritten with
// the label-anywhere rule (per-alias-group opt-in). It does not affect
// owner-name rewriting.
//
// Case contract (RFC 4343 / preserve-dns-name-case-in-responses):
//   - qname carries the on-wire case from req.Question[0].Name. Each entry
//     point folds it via dnsutil.LookupKey internally for matching and
//     zone Lookup, while preserving the original case for the wildcard owner
//     synthesis prefix.
//   - backupOrigin MUST be the lookup-fold backup FQDN (lowercase, trailing
//     dot) so it matches the lookup-fold qname and the root zone's lookup-
//     fold Origin during RewriteQName.
//   - backupOriginalCase MUST be the operator-authored YAML case for the
//     same backup origin (FQDN with trailing dot); used only for the on-wire
//     rewrite of owner / RDATA names. Pass the empty string only when no
//     case-preserving form is available (callers must source it from
//     ServerState.BackupOriginalCase[match.MatchedZone]).
//
// MUST NOT panic on any input.
func Resolve(qname string, qtype uint16, backupOrigin, backupOriginalCase string, backupZone *zone.Zone, rootZone *zone.Zone, rewriteRDATALabels bool) []dns.RR {
	if rootZone == nil {
		return nil
	}
	if rrs := ResolveExact(qname, qtype, backupOrigin, backupOriginalCase, backupZone, rootZone, rewriteRDATALabels); len(rrs) > 0 {
		return rrs
	}
	if rrs := ResolveWildcard(qname, qtype, backupOrigin, backupOriginalCase, rootZone, rewriteRDATALabels); len(rrs) > 0 {
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
func ResolveExactNoCNAME(qname string, qtype uint16, backupOrigin, backupOriginalCase string, backupZone *zone.Zone, rootZone *zone.Zone, rewriteRDATALabels bool) []dns.RR {
	if rootZone == nil {
		return nil
	}

	qnameFold := dnsutil.LookupKey(qname)

	if dnsutil.OverridableTypes[qtype] && backupZone != nil {
		if overrides := backupZone.Lookup(qnameFold, qtype); len(overrides) > 0 {
			return overrides
		}
	}

	rootQName := RewriteQName(qnameFold, backupOrigin, rootZone.Origin)
	rootRRs := rootZone.Lookup(rootQName, qtype)
	if len(rootRRs) == 0 {
		return nil
	}

	return finalizeBackupRRs(rootRRs, qtype, rootZone, backupOriginalCase, rewriteRDATALabels)
}

// ResolveCNAMEFallback performs only the RFC 1034 §3.6.2 CNAME-fallback
// branch of backup-zone resolution: look up a CNAME at the rewritten root
// qname and finalize (follow in-zone chain + namespace-rewrite). Returns
// nil for CNAME qtype (the fallback is only meaningful for non-CNAME
// qtypes) or when no CNAME exists at the qname.
//
// MUST NOT panic on any input.
func ResolveCNAMEFallback(qname string, qtype uint16, backupOrigin, backupOriginalCase string, rootZone *zone.Zone, rewriteRDATALabels bool) []dns.RR {
	if rootZone == nil || qtype == dns.TypeCNAME {
		return nil
	}

	qnameFold := dnsutil.LookupKey(qname)
	rootQName := RewriteQName(qnameFold, backupOrigin, rootZone.Origin)
	rootRRs := rootZone.Lookup(rootQName, dns.TypeCNAME)
	if len(rootRRs) == 0 {
		return nil
	}

	return finalizeBackupRRs(rootRRs, qtype, rootZone, backupOriginalCase, rewriteRDATALabels)
}

// ResolveExact performs the exact-match portion of backup-zone resolution:
// backup override lookup, root exact lookup with in-bailiwick rewrite, and
// RFC 1034 §3.6.2 CNAME fallback. Wildcard synthesis is NOT consulted.
// Returns nil when no exact match exists.
//
// MUST NOT panic on any input.
func ResolveExact(qname string, qtype uint16, backupOrigin, backupOriginalCase string, backupZone *zone.Zone, rootZone *zone.Zone, rewriteRDATALabels bool) []dns.RR {
	if rrs := ResolveExactNoCNAME(qname, qtype, backupOrigin, backupOriginalCase, backupZone, rootZone, rewriteRDATALabels); len(rrs) > 0 {
		return rrs
	}
	return ResolveCNAMEFallback(qname, qtype, backupOrigin, backupOriginalCase, rootZone, rewriteRDATALabels)
}

// ResolveWildcard performs the wildcard-synthesis portion of backup-zone
// resolution. Returns nil when no wildcard covers the rewritten qname.
//
// MUST NOT panic on any input.
func ResolveWildcard(qname string, qtype uint16, backupOrigin, backupOriginalCase string, rootZone *zone.Zone, rewriteRDATALabels bool) []dns.RR {
	if rootZone == nil {
		return nil
	}

	qnameFold := dnsutil.LookupKey(qname)
	rootQName := RewriteQName(qnameFold, backupOrigin, rootZone.Origin)
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

	return synthesizeWildcardRRs(wRRs, qname, qtype, backupOrigin, backupOriginalCase, rootZone, rewriteRDATALabels)
}

// synthesizeWildcardRRs is the legacy wildcard-emission tail shared by
// ResolveWildcard and ResolveWildcardCollapse: rewrite wildcard owners from
// "*.<zone>" to the rewritten root qname, preserving the on-wire case of the
// original qname's prefix, then finalize. The suffix is the lookup-fold root
// origin; finalizeBackupRRs → RewriteRR will rewrite that suffix to
// backupOriginalCase, yielding a fully case-preserving owner.
func synthesizeWildcardRRs(wRRs []dns.RR, qname string, qtype uint16, backupOrigin, backupOriginalCase string, rootZone *zone.Zone, rewriteRDATALabels bool) []dns.RR {
	wildcardOwner := RewriteName(qname, backupOrigin, rootZone.Origin)
	rootRRs := make([]dns.RR, len(wRRs))
	for i, rr := range wRRs {
		cp := dns.Copy(rr)
		cp.Header().Name = wildcardOwner
		rootRRs[i] = cp
	}

	return finalizeBackupRRs(rootRRs, qtype, rootZone, backupOriginalCase, rewriteRDATALabels)
}

// ResolveExactCollapse mirrors ResolveExactNoCNAME under the unified collapse
// rule. For every qtype except CNAME an exact hit involves no chain, so the
// behavior is identical to ResolveExactNoCNAME (nodata is always false on
// that path). For a direct CNAME-type query the stored CNAME is never
// emitted: the chain is chased and collapsed, yielding either the
// synthesized tail CNAME or NODATA.
//
// The returned nodata=true (with zero records) means the chain ended in-zone
// without an answer; the caller MUST short-circuit to a negative reply
// instead of consulting later stages (design D4).
//
// MUST NOT panic on any input.
func ResolveExactCollapse(qname string, qtype uint16, backupOrigin, backupOriginalCase string, backupZone *zone.Zone, rootZone *zone.Zone, rewriteRDATALabels bool) ([]dns.RR, bool) {
	if rootZone == nil {
		return nil, false
	}
	if qtype != dns.TypeCNAME {
		return ResolveExactNoCNAME(qname, qtype, backupOrigin, backupOriginalCase, backupZone, rootZone, rewriteRDATALabels), false
	}
	return collapseChainAt(qname, qtype, backupOrigin, backupOriginalCase, rootZone, rewriteRDATALabels)
}

// ResolveCNAMEFallbackCollapse mirrors ResolveCNAMEFallback under the unified
// collapse rule: the chain found at the rewritten qname is consumed instead
// of emitted. See ResolveExactCollapse for the nodata contract.
//
// MUST NOT panic on any input.
func ResolveCNAMEFallbackCollapse(qname string, qtype uint16, backupOrigin, backupOriginalCase string, rootZone *zone.Zone, rewriteRDATALabels bool) ([]dns.RR, bool) {
	if rootZone == nil || qtype == dns.TypeCNAME {
		return nil, false
	}
	return collapseChainAt(qname, qtype, backupOrigin, backupOriginalCase, rootZone, rewriteRDATALabels)
}

// collapseChainAt is the chase pipeline shared by ResolveExactCollapse and
// ResolveCNAMEFallbackCollapse: fold the qname, rewrite it into the root
// namespace, look up the CNAME RRset there, and collapse the chain. Returns
// (nil, false) when no CNAME exists at the rewritten name (stage miss).
func collapseChainAt(qname string, qtype uint16, backupOrigin, backupOriginalCase string, rootZone *zone.Zone, rewriteRDATALabels bool) ([]dns.RR, bool) {
	qnameFold := dnsutil.LookupKey(qname)
	rootQName := RewriteQName(qnameFold, backupOrigin, rootZone.Origin)
	initial := rootZone.Lookup(rootQName, dns.TypeCNAME)
	if len(initial) == 0 {
		return nil, false
	}
	return collapseBackupResult(rootZone.CollapseCNAME(initial, qtype), qname, rootZone, backupOriginalCase, rewriteRDATALabels)
}

// ResolveWildcardCollapse mirrors ResolveWildcard under the unified collapse
// rule. A wildcard hit of the requested qtype involves no chain and emits the
// same answer as ResolveWildcard (both call synthesizeWildcardRRs); a
// wildcard CNAME start is chased and collapsed. The stored wildcard slice is
// fed to the chase raw — the collapse only consumes RDATA and TTL, so the
// owner pre-copy of the legacy path would be wasted. See ResolveExactCollapse
// for the nodata contract.
//
// MUST NOT panic on any input.
func ResolveWildcardCollapse(qname string, qtype uint16, backupOrigin, backupOriginalCase string, rootZone *zone.Zone, rewriteRDATALabels bool) ([]dns.RR, bool) {
	if rootZone == nil {
		return nil, false
	}

	qnameFold := dnsutil.LookupKey(qname)
	rootQName := RewriteQName(qnameFold, backupOrigin, rootZone.Origin)

	if qtype != dns.TypeCNAME {
		if wRRs, wFound := rootZone.LookupWildcard(rootQName, qtype); wFound && len(wRRs) > 0 {
			return synthesizeWildcardRRs(wRRs, qname, qtype, backupOrigin, backupOriginalCase, rootZone, rewriteRDATALabels), false
		}
	}

	wCNAMEs, _ := rootZone.LookupWildcard(rootQName, dns.TypeCNAME)
	if len(wCNAMEs) == 0 {
		return nil, false
	}
	return collapseBackupResult(rootZone.CollapseCNAME(wCNAMEs, qtype), qname, rootZone, backupOriginalCase, rewriteRDATALabels)
}

// collapseBackupResult assembles the backup-namespace response for a collapse
// chase outcome (the single three-outcome assembly shared by the collapse
// entry points above):
//
//   - Records: exactly one dns.Copy per terminal record; the owner (the
//     backup-namespace on-wire qname) and chain-minimum TTL are written on
//     the copy, and the RDATA name fields receive the same rewrite RewriteRR
//     applies (rewriteRDATANames primitive).
//   - Tail: one synthesized CNAME whose target gets the stored-CNAME-target
//     treatment — label-anywhere when rewriteRDATALabels, in-bailiwick rule
//     otherwise.
//   - NoData: (nil, true).
func collapseBackupResult(res zone.CollapseResult, qname string, rootZone *zone.Zone, backupOriginalCase string, rewriteRDATALabels bool) ([]dns.RR, bool) {
	switch res.Outcome {
	case zone.CollapseRecords:
		out := make([]dns.RR, len(res.RRs))
		for i, rr := range res.RRs {
			cp := dns.Copy(rr)
			cp.Header().Name = qname
			cp.Header().Ttl = res.MinTTL
			rewriteRDATANames(cp, rootZone.Origin, backupOriginalCase, rewriteRDATALabels)
			out[i] = cp
		}
		return out, false
	case zone.CollapseTail:
		synth := &dns.CNAME{
			Hdr:    dns.RR_Header{Name: qname, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: res.MinTTL},
			Target: res.Target,
		}
		rewriteRDATANames(synth, rootZone.Origin, backupOriginalCase, rewriteRDATALabels)
		return []dns.RR{synth}, false
	default: // zone.CollapseNoData
		return nil, true
	}
}

// finalizeBackupRRs applies in-zone CNAME following (RFC 1034 §3.6.2) and
// then rewrites owners from the root namespace to the backup namespace.
//
// backupOriginalCase MUST be the operator-authored YAML case for the backup
// origin (FQDN with trailing dot); forwarded to RewriteRR as the on-wire
// backup origin.
func finalizeBackupRRs(rootRRs []dns.RR, qtype uint16, rootZone *zone.Zone, backupOriginalCase string, rewriteRDATALabels bool) []dns.RR {
	if len(rootRRs) > 0 && qtype != dns.TypeCNAME {
		if _, ok := rootRRs[len(rootRRs)-1].(*dns.CNAME); ok {
			rootRRs = rootZone.FollowCNAME(nil, rootRRs, qtype)
		}
	}
	result := make([]dns.RR, 0, len(rootRRs))
	for _, rr := range rootRRs {
		result = append(result, RewriteRR(rr, rootZone.Origin, backupOriginalCase, rewriteRDATALabels))
	}
	return result
}
