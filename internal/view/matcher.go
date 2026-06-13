package view

import (
	"net/netip"
	"strings"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// NamedRuleSet binds a view name to its ordered match-clients address-match-list.
// The orchestrator builds these from config.Config.Views.
type NamedRuleSet struct {
	Name  string
	Rules []config.Element
}

// Matcher resolves a client's addresses to a view name.
// GeoIP fields may be nil — in that case country/ASN rules can never match
// (no-match, not error). Net-based rules are always usable because they don't
// need external data.
type Matcher struct {
	Views   []NamedRuleSet
	Country *CountryDB // nil is a legitimate production state when the configuration declares no geo rules
	ASN     *ASNDB     // same

	// LocalhostNets and LocalnetsNets are the server's own addresses (as host
	// prefixes) and the networks attached to its interfaces, enumerated at build
	// time. They back the `localhost` and `localnets` built-in ACLs. Empty sets
	// mean those built-ins match nothing (fail-closed) — e.g. when interface
	// enumeration failed.
	LocalhostNets []netip.Prefix
	LocalnetsNets []netip.Prefix
}

// Resolve returns the name of the first view whose match-clients list accepts
// the client, or "" (empty string) when no view accepts. The empty result is the
// explicit "no-view" sentinel — the caller is responsible for producing REFUSED.
//
// Two addresses drive the evaluation:
//   - srcIP is the transport-level source address; it is the only address
//     evaluated by any/none/localhost/localnets/ip/cidr elements. An ECS-derived
//     address is client-controlled data and MUST NOT satisfy these ACL-style
//     elements.
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
	for _, v := range m.Views {
		if m.listAccepts(v.Rules, srcIP, geoIP, 0) {
			return v.Name
		}
	}
	return ""
}

// listAccepts evaluates an ordered address-match-list with BIND's first-match
// semantics: the first element that fires decides the list outcome — a
// non-negated firing element accepts (returns true), a negated firing element
// rejects (returns false). If no element fires, the list does not accept
// (default deny). MUST NOT panic.
func (m *Matcher) listAccepts(elems []config.Element, srcIP, geoIP netip.Addr, depth int) bool {
	if depth > config.MaxElementDepth {
		return false
	}
	for _, el := range elems {
		if m.elementFires(el, srcIP, geoIP, depth) {
			return !el.Negated
		}
	}
	return false
}

// elementFires reports whether an element's predicate matches the client,
// ignoring negation (the caller applies negation). Country/ASN predicates read
// geoIP; all net-based predicates read srcIP. A reference or nested group fires
// when its own list accepts the client (evaluated recursively). A dropped
// reference (nil Sub) never fires (fail-closed).
func (m *Matcher) elementFires(el config.Element, srcIP, geoIP netip.Addr, depth int) bool {
	switch el.Kind {
	case config.ElemAny:
		return true
	case config.ElemNone:
		return false
	case config.ElemLocalhost:
		return anyPrefixContains(m.LocalhostNets, srcIP)
	case config.ElemLocalnets:
		return anyPrefixContains(m.LocalnetsNets, srcIP)
	case config.ElemLeaf:
		return m.leafMatches(el.Leaf, srcIP, geoIP)
	case config.ElemRef, config.ElemGroup:
		if el.Sub == nil {
			// An undefined/cyclic reference (dropped by the loader) or an empty
			// group never matches.
			return false
		}
		return m.listAccepts(el.Sub, srcIP, geoIP, depth+1)
	default:
		return false
	}
}

// leafMatches evaluates a single leaf predicate. Country/ASN rules look up geoIP;
// ip/cidr rules evaluate srcIP (geoIP must never satisfy them). Returns false
// (no-match) rather than panicking on any error condition.
func (m *Matcher) leafMatches(rule config.MatchRule, srcIP, geoIP netip.Addr) bool {
	switch r := rule.(type) {
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
		// Unknown leaf type: no-match (never panic).
		return false
	}
}
