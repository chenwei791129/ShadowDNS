package alias

import (
	"reflect"

	"github.com/miekg/dns"
)

// rrClass is the three-outcome classification of a record with respect to
// backup-answer emission (fail-closed withholding, design Decision 2).
type rrClass int

const (
	// classCovered: rrtype handled by rewriteRDATANames; rewrite and emit.
	classCovered rrClass = iota
	// classNoName: RDATA carries no domain name (e.g. A/AAAA/TXT and
	// unknown RFC 3597 types); emit unchanged.
	classNoName
	// classUncoveredName: RDATA carries a domain name but the type is not
	// covered by the rewrite (e.g. RRSIG, NSEC, MINFO); withhold from backup
	// answers so a half-rewritten record never leaks the root origin.
	classUncoveredName
)

// coveredRRTypes is the explicit set of rrtypes handled by the
// rewriteRDATANames type switch. The switch cannot be introspected, so this
// set is maintained alongside it; TestCoveredSetMatchesRewriteSwitch and
// TestCoveredTypesAreNameBearing guard that the two stay in sync in both
// directions.
var coveredRRTypes = map[uint16]bool{
	dns.TypeCNAME: true,
	dns.TypeNS:    true,
	dns.TypeMX:    true,
	dns.TypePTR:   true,
	dns.TypeSRV:   true,
	dns.TypeSOA:   true,
	dns.TypeHTTPS: true,
	dns.TypeSVCB:  true,
	dns.TypeDNAME: true,
	dns.TypeNAPTR: true,
	dns.TypeRP:    true,
	dns.TypeKX:    true,
	dns.TypeAFSDB: true,
	dns.TypePX:    true,
	dns.TypeRT:    true,
}

// rrHeaderType is skipped by the reflection walk: RR_Header.Name carries a
// cdomain-name tag but is the owner name, not RDATA.
var rrHeaderType = reflect.TypeOf(dns.RR_Header{})

// hasNameBearingRDATA reports whether rr's RDATA contains at least one
// domain-name field, derived authoritatively from the record type's
// name-carrying struct tags rather than a hand-maintained list, so any
// name-bearing type absent from coveredRRTypes is withheld by default
// (fail closed). Uncached: the per-query hot path is answered by the
// init-time classByRRType table; this walk runs only for the init build
// itself and the rare unknown/private-type fallback, where a tag walk over
// a small struct is trivial.
func hasNameBearingRDATA(rr dns.RR) bool {
	st := reflect.TypeOf(rr)
	for st.Kind() == reflect.Pointer {
		st = st.Elem()
	}
	return st.Kind() == reflect.Struct && structHasNameTag(st)
}

// structHasNameTag walks st's fields looking for a domain-name struct tag,
// recursing into anonymous embedded structs: dns.HTTPS is `struct { SVCB }`
// and its Target tag lives on the embedded SVCB, so a top-level NumField
// scan alone would misclassify HTTPS as having no name-bearing RDATA.
func structHasNameTag(st reflect.Type) bool {
	for i := range st.NumField() {
		f := st.Field(i)
		if f.Type == rrHeaderType {
			// Owner name, not RDATA.
			continue
		}
		if f.Anonymous {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && structHasNameTag(ft) {
				return true
			}
			continue
		}
		switch f.Tag.Get("dns") {
		case "domain-name", "cdomain-name",
			// IPSECKEY.GatewayHost and AMTRELAY.GatewayHost carry a domain
			// name (when GatewayType is 3) under dedicated wire-format tags;
			// missing them would emit the root origin unrewritten. Treating
			// every IPSECKEY/AMTRELAY as name-bearing also withholds the
			// IP-gateway variants — fail closed in the safe direction.
			"ipsechost", "amtrelayhost":
			return true
		}
	}
	return false
}

// classByRRType precomputes the classification of every rrtype registered in
// miekg/dns at package-init time, derived from the same struct-tag walk the
// runtime fallback uses (still not a hand-maintained list). The per-query hot
// path then pays one plain map read for all well-known types; only records
// whose rrtype is absent here (dns.PrivateHandle registrations after init,
// unknown RFC 3597 types) fall back to the direct reflection walk.
var classByRRType = func() map[uint16]rrClass {
	m := make(map[uint16]rrClass, len(dns.TypeToRR))
	for rrtype, newRR := range dns.TypeToRR {
		switch {
		case coveredRRTypes[rrtype]:
			m[rrtype] = classCovered
		case hasNameBearingRDATA(newRR()):
			m[rrtype] = classUncoveredName
		default:
			m[rrtype] = classNoName
		}
	}
	return m
}()

// classifyRR maps rr to its backup-answer emission class. Never panics: an
// unknown or private type has no typed name-bearing field and classifies as
// classNoName.
func classifyRR(rr dns.RR) rrClass {
	if c, ok := classByRRType[rr.Header().Rrtype]; ok {
		return c
	}
	// Rrtype absent from the init-time table (private registration or
	// unknown RFC 3597 type) — such a type is never in coveredRRTypes, so
	// only the name-bearing check matters.
	if hasNameBearingRDATA(rr) {
		return classUncoveredName
	}
	return classNoName
}

// WithheldRecord identifies a record excluded from a backup answer because
// its type carries a domain name in its RDATA that the rewrite does not
// cover. Callers (which own a logger; this package stays logger-free) log
// one warning per entry.
type WithheldRecord struct {
	Rrtype uint16
	Owner  string
}

// LogArgs returns the structured-log key/value pairs identifying wr for the
// per-record withheld warning, so the query path and the AXFR path emit the
// same field schema (rrtype as mnemonic, owner in lookup-fold form) without
// this package depending on a logging library. ownerFold MUST be the
// lookup-fold form of wr.Owner (dnsutil.LookupKey); it is a parameter so a
// caller that already folded the owner (e.g. for a dedup key) does not fold
// it twice.
func (wr WithheldRecord) LogArgs(backup, ownerFold string) []any {
	return []any{
		"backup", backup,
		"rrtype", dns.TypeToString[wr.Rrtype],
		"owner", ownerFold,
	}
}

// FilterBackupRRs splits rrs into records safe to emit from a backup zone
// (covered types, to be rewritten, and no-name types, emitted unchanged) and
// withheld records (uncovered name-bearing types, per the fail-closed rule).
// Order is preserved. A non-empty withheld with an empty kept signals that
// the name existed in the root zone but every record was withheld — an
// existing-name NODATA, not a stage miss.
//
// The common case (nothing withheld) returns the input slice unchanged with
// no allocation.
func FilterBackupRRs(rrs []dns.RR) (kept []dns.RR, withheld []WithheldRecord) {
	for i, rr := range rrs {
		if classifyRR(rr) != classUncoveredName {
			continue
		}
		// Slow path: at least one record is withheld.
		kept = append(make([]dns.RR, 0, len(rrs)-1), rrs[:i]...)
		withheld = append(withheld, WithheldRecord{Rrtype: rr.Header().Rrtype, Owner: rr.Header().Name})
		for _, r := range rrs[i+1:] {
			if classifyRR(r) == classUncoveredName {
				withheld = append(withheld, WithheldRecord{Rrtype: r.Header().Rrtype, Owner: r.Header().Name})
			} else {
				kept = append(kept, r)
			}
		}
		return kept, withheld
	}
	return rrs, nil
}
