package config

import (
	"net/netip"
	"strings"
	"testing"
)

// leafOf returns the leaf rule of an element, failing the test if the element is
// not an ElemLeaf.
func leafOf(t *testing.T, el Element) MatchRule {
	t.Helper()
	if el.Kind != ElemLeaf {
		t.Fatalf("expected ElemLeaf, got kind %d (%+v)", el.Kind, el)
	}
	return el.Leaf
}

// TestCountryRuleUppercase verifies that an explicit uppercase country code is parsed correctly.
func TestCountryRuleUppercase(t *testing.T) {
	rules, err := ParseMatchClients([]byte("geoip country TH;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	cr, ok := leafOf(t, rules[0]).(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0].Leaf)
	}
	if cr.Code != "TH" {
		t.Errorf("expected code TH, got %q", cr.Code)
	}
}

// TestCountryRuleLowercaseNormalized verifies that a lowercase country code is uppercased.
func TestCountryRuleLowercaseNormalized(t *testing.T) {
	rules, err := ParseMatchClients([]byte("geoip country th;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	cr, ok := leafOf(t, rules[0]).(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0].Leaf)
	}
	if cr.Code != "TH" {
		t.Errorf("expected code TH, got %q", cr.Code)
	}
}

// TestASNRuleValid verifies that a well-formed asnum rule extracts the numeric AS.
func TestASNRuleValid(t *testing.T) {
	rules, err := ParseMatchClients([]byte(`geoip asnum "AS4134 Chinanet";`), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	ar, ok := leafOf(t, rules[0]).(ASNRule)
	if !ok {
		t.Fatalf("expected ASNRule, got %T", rules[0].Leaf)
	}
	if ar.ASN != 4134 {
		t.Errorf("expected ASN 4134, got %d", ar.ASN)
	}
}

// TestASNRuleUnparseable verifies that a malformed asnum value (no leading AS number) returns an error.
func TestASNRuleUnparseable(t *testing.T) {
	_, err := ParseMatchClients([]byte(`geoip asnum "Chinanet";`), "myfile.conf", 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "myfile.conf") {
		t.Errorf("error should contain path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "5") {
		t.Errorf("error should contain line number 5, got: %v", err)
	}
}

// TestIPRuleAndCIDRRuleDistinguished verifies that a bare IP becomes an IPRule
// and a CIDR prefix becomes a CIDRRule with the correct prefix length.
func TestIPRuleAndCIDRRuleDistinguished(t *testing.T) {
	body := "192.0.2.8;\n198.51.100.0/26;"
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(rules))
	}

	ir, ok := leafOf(t, rules[0]).(IPRule)
	if !ok {
		t.Fatalf("expected IPRule for first element, got %T", rules[0].Leaf)
	}
	wantAddr := netip.MustParseAddr("192.0.2.8")
	if ir.IP != wantAddr {
		t.Errorf("expected IP %v, got %v", wantAddr, ir.IP)
	}

	cr, ok := leafOf(t, rules[1]).(CIDRRule)
	if !ok {
		t.Fatalf("expected CIDRRule for second element, got %T", rules[1].Leaf)
	}
	if cr.Prefix.Bits() != 26 {
		t.Errorf("expected /26, got /%d", cr.Prefix.Bits())
	}
}

// TestMultipleRulesOnSingleLine verifies that multiple semicolon-separated rules
// on the same line are all parsed in order.
func TestMultipleRulesOnSingleLine(t *testing.T) {
	body := "geoip country CN; geoip country HK; geoip country MO;"
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(rules))
	}
	want := []string{"CN", "HK", "MO"}
	for i, el := range rules {
		cr, ok := leafOf(t, el).(CountryRule)
		if !ok {
			t.Fatalf("element[%d]: expected CountryRule, got %T", i, el.Leaf)
		}
		if cr.Code != want[i] {
			t.Errorf("element[%d]: expected %q, got %q", i, want[i], cr.Code)
		}
	}
}

// TestAnyRule verifies that `any;` produces an ElemAny element.
func TestAnyRule(t *testing.T) {
	rules, err := ParseMatchClients([]byte("any;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	if rules[0].Kind != ElemAny {
		t.Fatalf("expected ElemAny, got kind %d", rules[0].Kind)
	}
}

// TestBuiltinACLsRecognized verifies the spec scenario "Built-in acl names are
// recognized": none/localhost/localnets produce their built-in element kinds
// rather than being treated as named-acl references.
func TestBuiltinACLsRecognized(t *testing.T) {
	rules, err := ParseMatchClients([]byte("none; localhost; localnets;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []ElementKind{ElemNone, ElemLocalhost, ElemLocalnets}
	if len(rules) != len(want) {
		t.Fatalf("expected %d elements, got %d", len(want), len(rules))
	}
	for i, k := range want {
		if rules[i].Kind != k {
			t.Errorf("element[%d]: got kind %d, want %d", i, rules[i].Kind, k)
		}
	}
}

// TestNegatedAndNestedElementsParsed verifies the spec scenario "Negated and
// nested elements are parsed": `! 192.0.2.0/24; { 198.51.100.0/24; 203.0.113.0/24; }; any;`
// produces a negated CIDR element, a nested group of two CIDR elements, and an
// `any` element, in that order.
func TestNegatedAndNestedElementsParsed(t *testing.T) {
	body := "! 192.0.2.0/24; { 198.51.100.0/24; 203.0.113.0/24; }; any;"
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 elements, got %d: %+v", len(rules), rules)
	}

	// Element 0: negated CIDR 192.0.2.0/24.
	if !rules[0].Negated {
		t.Errorf("element[0] should be negated")
	}
	if cr, ok := leafOf(t, rules[0]).(CIDRRule); !ok || cr.Prefix.String() != "192.0.2.0/24" {
		t.Errorf("element[0]: got %+v, want negated CIDR 192.0.2.0/24", rules[0])
	}

	// Element 1: nested group of two CIDRs.
	if rules[1].Kind != ElemGroup {
		t.Fatalf("element[1]: got kind %d, want ElemGroup", rules[1].Kind)
	}
	if len(rules[1].Sub) != 2 {
		t.Fatalf("element[1] group: expected 2 sub-elements, got %d", len(rules[1].Sub))
	}
	if cr, ok := leafOf(t, rules[1].Sub[0]).(CIDRRule); !ok || cr.Prefix.String() != "198.51.100.0/24" {
		t.Errorf("group sub[0]: got %+v, want CIDR 198.51.100.0/24", rules[1].Sub[0])
	}
	if cr, ok := leafOf(t, rules[1].Sub[1]).(CIDRRule); !ok || cr.Prefix.String() != "203.0.113.0/24" {
		t.Errorf("group sub[1]: got %+v, want CIDR 203.0.113.0/24", rules[1].Sub[1])
	}

	// Element 2: any.
	if rules[2].Kind != ElemAny {
		t.Errorf("element[2]: got kind %d, want ElemAny", rules[2].Kind)
	}
}

// TestNamedReferenceParsedAsRef verifies the spec scenario "Named reference
// resolves to its acl element list" at the parse layer: a bare word that is no
// recognized keyword/address parses as an ElemRef carrying the name (resolution
// happens later, at build time). It is NOT dropped at parse time.
func TestNamedReferenceParsedAsRef(t *testing.T) {
	rules, err := ParseMatchClients([]byte("internal-net;"), "rules.conf", 7)
	if err != nil {
		t.Fatalf("named-acl reference must parse, not fatal, got: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d: %+v", len(rules), rules)
	}
	if rules[0].Kind != ElemRef {
		t.Fatalf("expected ElemRef, got kind %d", rules[0].Kind)
	}
	if rules[0].RefName != "internal-net" {
		t.Errorf("RefName: got %q, want internal-net", rules[0].RefName)
	}
}

// TestNegatedNamedReferenceParsed verifies `! internal;` parses as a negated
// reference element.
func TestNegatedNamedReferenceParsed(t *testing.T) {
	rules, err := ParseMatchClients([]byte("! internal;"), "rules.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	if rules[0].Kind != ElemRef || rules[0].RefName != "internal" || !rules[0].Negated {
		t.Errorf("got %+v, want negated ElemRef{internal}", rules[0])
	}
}

// TestReferenceAlongsideValidRule verifies that a named reference does not
// suppress a valid sibling: both the reference and the CIDR are parsed, in order.
func TestReferenceAlongsideValidRule(t *testing.T) {
	rules, err := ParseMatchClients([]byte("internal-net; 192.0.2.0/24;"), "rules.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 elements, got %d: %+v", len(rules), rules)
	}
	if rules[0].Kind != ElemRef || rules[0].RefName != "internal-net" {
		t.Errorf("element[0]: got %+v, want ElemRef{internal-net}", rules[0])
	}
	if _, ok := leafOf(t, rules[1]).(CIDRRule); !ok {
		t.Errorf("element[1]: got %T, want CIDRRule", rules[1].Leaf)
	}
}

// TestCommentsAreSkipped verifies that line comments do not produce rules.
func TestCommentsAreSkipped(t *testing.T) {
	body := "// foo\n geoip country US;"
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	cr, ok := leafOf(t, rules[0]).(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0].Leaf)
	}
	if cr.Code != "US" {
		t.Errorf("expected code US, got %q", cr.Code)
	}
}

// TestMalformedGeoIPFormReturnsError verifies that a malformed instance of a
// recognized form (a `geoip` sub-command written incorrectly) stays fatal — it
// is a recognized form written wrong, not an unsupported construct.
func TestMalformedGeoIPFormReturnsError(t *testing.T) {
	_, err := ParseMatchClients([]byte("geoip whatever;"), "rules.conf", 10)
	if err == nil {
		t.Fatal("expected error for malformed geoip rule, got nil")
	}
	if !strings.Contains(err.Error(), "rules.conf") {
		t.Errorf("error should contain path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "10") {
		t.Errorf("error should contain line number 10, got: %v", err)
	}
}

// TestHashCommentIsSkipped verifies that # comments are also stripped.
func TestHashCommentIsSkipped(t *testing.T) {
	body := "# hash comment\ngeoip country JP;"
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	cr, ok := leafOf(t, rules[0]).(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0].Leaf)
	}
	if cr.Code != "JP" {
		t.Errorf("expected JP, got %q", cr.Code)
	}
}

// TestBlockCommentIsSkipped verifies that /* ... */ comments are stripped.
func TestBlockCommentIsSkipped(t *testing.T) {
	body := "/* block comment */ geoip country DE;"
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 element, got %d", len(rules))
	}
	cr, ok := leafOf(t, rules[0]).(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0].Leaf)
	}
	if cr.Code != "DE" {
		t.Errorf("expected DE, got %q", cr.Code)
	}
}

// TestEmptyBodyProducesNoRules verifies that an empty body is accepted without error.
func TestEmptyBodyProducesNoRules(t *testing.T) {
	rules, err := ParseMatchClients([]byte(""), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 elements, got %d", len(rules))
	}
}

// anyElem and leafElem build elements for the FirstGeoRuleView tests below.
func anyElem() Element                 { return Element{Kind: ElemAny} }
func leafElem(r MatchRule) Element     { return Element{Kind: ElemLeaf, Leaf: r} }
func groupElem(sub ...Element) Element { return Element{Kind: ElemGroup, Sub: sub} }
func refElem(name string, sub ...Element) Element {
	return Element{Kind: ElemRef, RefName: name, Sub: sub}
}

// TestFirstGeoRuleViewNoGeoRules verifies that views containing only
// non-geo rules (any/IP/CIDR) report found=false.
func TestFirstGeoRuleViewNoGeoRules(t *testing.T) {
	views := []View{
		{
			Name:         "internal",
			MatchClients: []Element{anyElem(), leafElem(IPRule{IP: netip.MustParseAddr("192.0.2.8")})},
			Line:         3,
			Source:       "named.conf",
		},
		{
			Name:         "external",
			MatchClients: []Element{leafElem(CIDRRule{Prefix: netip.MustParsePrefix("198.51.100.0/26")})},
			Line:         12,
			Source:       "named.conf",
		},
	}
	name, source, line, found := FirstGeoRuleView(views)
	if found {
		t.Fatalf("expected found=false, got true (view=%q source=%q line=%d)", name, source, line)
	}
}

// TestFirstGeoRuleViewCountryRule verifies that a view with a CountryRule is
// reported with its name, source, and line.
func TestFirstGeoRuleViewCountryRule(t *testing.T) {
	views := []View{
		{
			Name:         "plain",
			MatchClients: []Element{anyElem()},
			Line:         1,
			Source:       "named.conf",
		},
		{
			Name:         "geo-view",
			MatchClients: []Element{leafElem(CountryRule{Code: "JP"})},
			Line:         20,
			Source:       "views.conf",
		},
	}
	name, source, line, found := FirstGeoRuleView(views)
	if !found {
		t.Fatal("expected found=true, got false")
	}
	if name != "geo-view" {
		t.Errorf("expected view name %q, got %q", "geo-view", name)
	}
	if source != "views.conf" {
		t.Errorf("expected source %q, got %q", "views.conf", source)
	}
	if line != 20 {
		t.Errorf("expected line 20, got %d", line)
	}
}

// TestFirstGeoRuleViewASNRule verifies that a view with an ASNRule counts as
// a geo rule.
func TestFirstGeoRuleViewASNRule(t *testing.T) {
	views := []View{
		{
			Name:         "asn-view",
			MatchClients: []Element{leafElem(ASNRule{ASN: 64500})},
			Line:         7,
			Source:       "asn.conf",
		},
	}
	name, source, line, found := FirstGeoRuleView(views)
	if !found {
		t.Fatal("expected found=true, got false")
	}
	if name != "asn-view" {
		t.Errorf("expected view name %q, got %q", "asn-view", name)
	}
	if source != "asn.conf" {
		t.Errorf("expected source %q, got %q", "asn.conf", source)
	}
	if line != 7 {
		t.Errorf("expected line 7, got %d", line)
	}
}

// TestFirstGeoRuleViewDeclarationOrder verifies that when multiple views
// contain geo rules, the first view in declaration order wins — including a
// geo rule that appears after non-geo rules within the same view's
// MatchClients.
func TestFirstGeoRuleViewDeclarationOrder(t *testing.T) {
	views := []View{
		{
			Name:         "no-geo",
			MatchClients: []Element{leafElem(IPRule{IP: netip.MustParseAddr("203.0.113.1")})},
			Line:         2,
			Source:       "named.conf",
		},
		{
			Name: "first-geo",
			// Geo rule appears after a non-geo rule within the same view.
			MatchClients: []Element{anyElem(), leafElem(CountryRule{Code: "KR"})},
			Line:         10,
			Source:       "first.conf",
		},
		{
			Name:         "second-geo",
			MatchClients: []Element{leafElem(ASNRule{ASN: 64501})},
			Line:         30,
			Source:       "second.conf",
		},
	}
	name, source, line, found := FirstGeoRuleView(views)
	if !found {
		t.Fatal("expected found=true, got false")
	}
	if name != "first-geo" {
		t.Errorf("expected first view in declaration order %q, got %q", "first-geo", name)
	}
	if source != "first.conf" {
		t.Errorf("expected source %q, got %q", "first.conf", source)
	}
	if line != 10 {
		t.Errorf("expected line 10, got %d", line)
	}
}

// TestFirstGeoRuleViewNestedAndReferenced verifies that a geo leaf reachable only
// through a nested group or a resolved named reference is still detected (the
// GeoIP database must be required in those cases too).
func TestFirstGeoRuleViewNestedAndReferenced(t *testing.T) {
	t.Run("geo inside nested group", func(t *testing.T) {
		views := []View{{
			Name:         "grouped",
			MatchClients: []Element{groupElem(leafElem(CountryRule{Code: "TW"}))},
			Line:         4,
			Source:       "g.conf",
		}}
		if _, _, _, found := FirstGeoRuleView(views); !found {
			t.Error("expected geo leaf inside a nested group to be found")
		}
	})

	t.Run("geo behind resolved reference", func(t *testing.T) {
		views := []View{{
			Name:         "ref",
			MatchClients: []Element{refElem("geo-acl", leafElem(ASNRule{ASN: 64500}))},
			Line:         9,
			Source:       "r.conf",
		}}
		if _, _, _, found := FirstGeoRuleView(views); !found {
			t.Error("expected geo leaf behind a resolved reference to be found")
		}
	})
}

// TestFullBlockExample exercises the reference example from the spec.
func TestFullBlockExample(t *testing.T) {
	body := `
    geoip country RU;
    geoip country KR;
    geoip country JP; geoip asnum "AS4134 Chinanet"; geoip asnum "AS17621 China Unicom Shanghai network"; geoip country CN; geoip country HK; geoip country MO;
    192.0.2.8;
    198.51.100.0/26;
    any;
`
	rules, err := ParseMatchClients([]byte(body), "example.conf", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// RU, KR, JP, AS4134, AS17621, CN, HK, MO, 192.0.2.8, 198.51.100.0/26, any = 11 elements.
	if len(rules) != 11 {
		t.Fatalf("expected 11 elements, got %d", len(rules))
	}
}
