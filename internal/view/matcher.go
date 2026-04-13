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

// Matcher resolves a client IP to a view name.
// GeoIP fields may be nil — in that case country/ASN rules can never match
// (no-match, not error). NetMatch is always usable because it doesn't need
// external data.
type Matcher struct {
	Views   []NamedRuleSet
	Country *CountryDB // can be nil only in tests; production path requires it
	ASN     *ASNDB     // same
}

// Resolve returns the view name whose first matching rule fires for clientIP,
// or "" (empty string) when no view matches. The empty result is the explicit
// "no-view" sentinel — caller is responsible for producing REFUSED.
//
// MUST NOT panic on any input.
func (m *Matcher) Resolve(clientIP netip.Addr) string {
	for _, view := range m.Views {
		for _, rule := range view.Rules {
			if m.ruleMatches(rule, clientIP) {
				return view.Name
			}
		}
	}
	return ""
}

// ruleMatches evaluates a single rule against clientIP.
// Returns false (no-match) rather than panicking on any error condition.
func (m *Matcher) ruleMatches(rule config.MatchRule, clientIP netip.Addr) bool {
	switch r := rule.(type) {
	case config.AnyRule:
		return true

	case config.CountryRule:
		if m.Country == nil {
			return false
		}
		code, ok := m.Country.Lookup(clientIP)
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
		asn, ok := m.ASN.Lookup(clientIP)
		if !ok {
			return false
		}
		return asn == r.ASN

	case config.IPRule:
		return matchIP(r, clientIP)

	case config.CIDRRule:
		return matchCIDR(r, clientIP)

	default:
		// Unknown rule type: no-match (never panic).
		return false
	}
}
