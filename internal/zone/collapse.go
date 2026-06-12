package zone

import (
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// CollapseOutcome classifies how an in-zone CNAME chase ended.
type CollapseOutcome int

const (
	// CollapseRecords: the chain resolved in-zone to records of the
	// requested qtype.
	CollapseRecords CollapseOutcome = iota
	// CollapseTail: the chain left the zone or exhausted the depth budget;
	// Target holds the first unresolved name.
	CollapseTail
	// CollapseNoData: the chain ended at an in-zone name with neither the
	// requested qtype nor a further CNAME.
	CollapseNoData
)

// CollapseResult is the typed outcome of CollapseCNAME.
type CollapseResult struct {
	Outcome CollapseOutcome
	// RRs holds the terminal records when Outcome == CollapseRecords. It is
	// the zone's stored slice: callers MUST NOT modify it in place — owner
	// and TTL rewrites happen on dns.Copy copies.
	RRs []dns.RR
	// Target is the synthesized CNAME target when Outcome == CollapseTail,
	// preserving the zone-file original case.
	Target string
	// MinTTL is the minimum TTL across every consumed chain record,
	// including the terminal records for CollapseRecords.
	MinTTL uint32
}

// CollapseCNAME chases an in-zone CNAME chain without accumulating the
// intermediate records, so the caller can hide them from the response. It
// applies the same per-hop rule as FollowCNAME — exact qtype, exact CNAME,
// wildcard qtype, wildcard CNAME — with one exception: when qtype is CNAME,
// the qtype lookup steps are skipped because a CNAME record is always a hop,
// never terminal data (the outcome is then only CollapseTail or
// CollapseNoData).
//
// The depth budget is MaxCNAMEDepth consumed CNAME records including the
// initial RRset; resolving a target into terminal records is free. When the
// budget runs out while the current target still needs another CNAME hop,
// the result is CollapseTail with that unresolved name as Target — the same
// shape as a chain leaving the zone. Note the budget accounting differs from
// FollowCNAME's iteration count for chains of exactly MaxCNAMEDepth records
// (see design D3 of add-cname-chain-collapsing); FollowCNAME itself is
// deliberately untouched.
//
// initial is the CNAME RRset found at the query name. Degenerate inputs
// (empty slice, non-CNAME last element) fail closed to CollapseNoData; the
// function never panics.
func (z *Zone) CollapseCNAME(initial []dns.RR, qtype uint16) CollapseResult {
	if len(initial) == 0 {
		return CollapseResult{Outcome: CollapseNoData}
	}

	minTTL := minRRTTL(initial[0].Header().Ttl, initial[1:])
	cur, ok := initial[len(initial)-1].(*dns.CNAME)
	if !ok {
		return CollapseResult{Outcome: CollapseNoData}
	}
	consumed := len(initial)

	for {
		target := strings.ToLower(cur.Target)

		if !dnsutil.IsInZone(target, z.Origin) {
			return CollapseResult{Outcome: CollapseTail, Target: cur.Target, MinTTL: minTTL}
		}

		if qtype != dns.TypeCNAME {
			if rrs := z.Lookup(target, qtype); len(rrs) > 0 {
				return CollapseResult{Outcome: CollapseRecords, RRs: rrs, MinTTL: minRRTTL(minTTL, rrs)}
			}
		}

		next := z.Lookup(target, dns.TypeCNAME)
		if len(next) == 0 && qtype != dns.TypeCNAME {
			wRRs, wFound := z.LookupWildcard(target, qtype)
			if wFound && len(wRRs) > 0 {
				return CollapseResult{Outcome: CollapseRecords, RRs: wRRs, MinTTL: minRRTTL(minTTL, wRRs)}
			}
		}
		if len(next) == 0 {
			next, _ = z.LookupWildcard(target, dns.TypeCNAME)
		}
		if len(next) == 0 {
			return CollapseResult{Outcome: CollapseNoData, MinTTL: minTTL}
		}

		if consumed >= MaxCNAMEDepth {
			return CollapseResult{Outcome: CollapseTail, Target: cur.Target, MinTTL: minTTL}
		}
		cur, ok = next[len(next)-1].(*dns.CNAME)
		if !ok {
			return CollapseResult{Outcome: CollapseNoData, MinTTL: minTTL}
		}
		consumed += len(next)
		minTTL = minRRTTL(minTTL, next)
	}
}

// minRRTTL folds the TTLs of rrs into the running minimum.
func minRRTTL(current uint32, rrs []dns.RR) uint32 {
	for _, rr := range rrs {
		current = min(current, rr.Header().Ttl)
	}
	return current
}
