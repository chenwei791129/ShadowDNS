package view

import (
	"net/netip"
	"strings"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// NamedRuleSet binds a view name to its ordered match-clients rules.
// The orchestrator builds these from config.Config.Views.
type NamedRuleSet struct {
	Name  string
	Rules []config.MatchRule
}

// Matcher resolves a client's addresses to a view name.
// GeoIP fields may be nil — in that case country/ASN rules can never match
// (no-match, not error). NetMatch is always usable because it doesn't need
// external data.
type Matcher struct {
	Views   []NamedRuleSet
	Country *CountryDB // can be nil only in tests; production path requires it
	ASN     *ASNDB     // same
}

// Resolve returns the view name whose first matching rule fires, or ""
// (empty string) when no view matches. The empty result is the explicit
// "no-view" sentinel — caller is responsible for producing REFUSED.
//
// Two addresses drive the evaluation:
//   - srcIP is the transport-level source address; it is the only address
//     evaluated by any/ip/cidr rules. An ECS-derived address is
//     client-controlled data and MUST NOT satisfy these ACL-style rules.
//   - geoIP is the address used for country/ASN mmdb lookups (e.g. an
//     ECS-derived address). Callers without an ECS-derived address pass the
//     source IP as geoIP, making resolution identical to single-address
//     behavior.
//
// MUST NOT panic on any input.
func (m *Matcher) Resolve(srcIP, geoIP netip.Addr) string {
	// Defense against caller mistakes: a zero-value geoIP would silently
	// disable every geo rule; treat it as "no ECS-derived address" instead.
	if !geoIP.IsValid() {
		geoIP = srcIP
	}
	for _, view := range m.Views {
		for _, rule := range view.Rules {
			if m.ruleMatches(rule, srcIP, geoIP) {
				return view.Name
			}
		}
	}
	return ""
}

// ruleMatches evaluates a single rule: country/ASN rules look up geoIP,
// any/ip/cidr rules evaluate srcIP (geoIP must never satisfy them).
// Returns false (no-match) rather than panicking on any error condition.
func (m *Matcher) ruleMatches(rule config.MatchRule, srcIP, geoIP netip.Addr) bool {
	switch r := rule.(type) {
	case config.AnyRule:
		return true

	case config.CountryRule:
		if m.Country == nil {
			return false
		}
		code, ok := m.Country.Lookup(geoIP)
		if !ok {
			return false
		}
		// Case-insensitive: mmdb returns uppercase ISO codes, but CountryRule.Code
		// may be lowercase when constructed directly in tests.
		return strings.EqualFold(code, r.Code)

	case config.ASNRule:
		if m.ASN == nil {
			return false
		}
		asn, ok := m.ASN.Lookup(geoIP)
		if !ok {
			return false
		}
		return asn == r.ASN

	case config.IPRule:
		return matchIP(r, srcIP)

	case config.CIDRRule:
		return matchCIDR(r, srcIP)

	default:
		// Unknown rule type: no-match (never panic).
		return false
	}
}
