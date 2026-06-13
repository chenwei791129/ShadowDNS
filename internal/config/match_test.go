package config

import (
	"net/netip"
	"strings"
	"testing"
)

// TestCountryRuleUppercase verifies that an explicit uppercase country code is parsed correctly.
func TestCountryRuleUppercase(t *testing.T) {
	rules, _, err := ParseMatchClients([]byte("geoip country TH;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	cr, ok := rules[0].(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0])
	}
	if cr.Code != "TH" {
		t.Errorf("expected code TH, got %q", cr.Code)
	}
}

// TestCountryRuleLowercaseNormalized verifies that a lowercase country code is uppercased.
func TestCountryRuleLowercaseNormalized(t *testing.T) {
	rules, _, err := ParseMatchClients([]byte("geoip country th;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	cr, ok := rules[0].(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0])
	}
	if cr.Code != "TH" {
		t.Errorf("expected code TH, got %q", cr.Code)
	}
}

// TestASNRuleValid verifies that a well-formed asnum rule extracts the numeric AS.
func TestASNRuleValid(t *testing.T) {
	rules, _, err := ParseMatchClients([]byte(`geoip asnum "AS4134 Chinanet";`), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	ar, ok := rules[0].(ASNRule)
	if !ok {
		t.Fatalf("expected ASNRule, got %T", rules[0])
	}
	if ar.ASN != 4134 {
		t.Errorf("expected ASN 4134, got %d", ar.ASN)
	}
}

// TestASNRuleUnparseable verifies that a malformed asnum value (no leading AS number) returns an error.
func TestASNRuleUnparseable(t *testing.T) {
	_, _, err := ParseMatchClients([]byte(`geoip asnum "Chinanet";`), "myfile.conf", 5)
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
	rules, _, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	ir, ok := rules[0].(IPRule)
	if !ok {
		t.Fatalf("expected IPRule for first rule, got %T", rules[0])
	}
	wantAddr := netip.MustParseAddr("192.0.2.8")
	if ir.IP != wantAddr {
		t.Errorf("expected IP %v, got %v", wantAddr, ir.IP)
	}

	cr, ok := rules[1].(CIDRRule)
	if !ok {
		t.Fatalf("expected CIDRRule for second rule, got %T", rules[1])
	}
	if cr.Prefix.Bits() != 26 {
		t.Errorf("expected /26, got /%d", cr.Prefix.Bits())
	}
}

// TestMultipleRulesOnSingleLine verifies that multiple semicolon-separated rules
// on the same line are all parsed in order.
func TestMultipleRulesOnSingleLine(t *testing.T) {
	body := "geoip country CN; geoip country HK; geoip country MO;"
	rules, _, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	want := []string{"CN", "HK", "MO"}
	for i, r := range rules {
		cr, ok := r.(CountryRule)
		if !ok {
			t.Fatalf("rule[%d]: expected CountryRule, got %T", i, r)
		}
		if cr.Code != want[i] {
			t.Errorf("rule[%d]: expected %q, got %q", i, want[i], cr.Code)
		}
	}
}

// TestAnyRule verifies that `any;` produces an AnyRule.
func TestAnyRule(t *testing.T) {
	rules, _, err := ParseMatchClients([]byte("any;"), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if _, ok := rules[0].(AnyRule); !ok {
		t.Fatalf("expected AnyRule, got %T", rules[0])
	}
}

// TestCommentsAreSkipped verifies that line comments do not produce rules.
func TestCommentsAreSkipped(t *testing.T) {
	body := "// foo\n geoip country US;"
	rules, _, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	cr, ok := rules[0].(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0])
	}
	if cr.Code != "US" {
		t.Errorf("expected code US, got %q", cr.Code)
	}
}

// TestMalformedGeoIPFormReturnsError verifies that a malformed instance of a
// recognized form (a `geoip` sub-command written incorrectly) stays fatal — it
// is a recognized form written wrong, not an unsupported construct.
func TestMalformedGeoIPFormReturnsError(t *testing.T) {
	_, _, err := ParseMatchClients([]byte("geoip whatever;"), "rules.conf", 10)
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

// TestUnrecognizedTokenIsDropped verifies that a token matching no recognized
// rule form (a named-acl reference) is dropped — returned in dropped with its
// token and line, not appended to rules, and not fatal.
func TestUnrecognizedTokenIsDropped(t *testing.T) {
	rules, dropped, err := ParseMatchClients([]byte("internal-net;"), "rules.conf", 7)
	if err != nil {
		t.Fatalf("named-acl reference must be dropped, not fatal, got: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("dropped token must not produce a rule, got %+v", rules)
	}
	if len(dropped) != 1 {
		t.Fatalf("expected exactly 1 dropped rule, got %d: %+v", len(dropped), dropped)
	}
	if dropped[0].Token != "internal-net" {
		t.Errorf("dropped token: got %q, want internal-net", dropped[0].Token)
	}
	if dropped[0].Line != 7 {
		t.Errorf("dropped line: got %d, want 7", dropped[0].Line)
	}
}

// TestDroppedRuleAlongsideValidRule verifies that a dropped rule does not
// suppress a valid sibling: the CIDR survives while the named-acl is dropped.
func TestDroppedRuleAlongsideValidRule(t *testing.T) {
	rules, dropped, err := ParseMatchClients([]byte("internal-net; 192.0.2.0/24;"), "rules.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected the CIDR rule to survive, got %d rules: %+v", len(rules), rules)
	}
	if _, ok := rules[0].(CIDRRule); !ok {
		t.Errorf("surviving rule: got %T, want CIDRRule", rules[0])
	}
	if len(dropped) != 1 || dropped[0].Token != "internal-net" {
		t.Errorf("expected internal-net dropped, got %+v", dropped)
	}
}

// TestHashCommentIsSkipped verifies that # comments are also stripped.
func TestHashCommentIsSkipped(t *testing.T) {
	body := "# hash comment\ngeoip country JP;"
	rules, _, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	cr, ok := rules[0].(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0])
	}
	if cr.Code != "JP" {
		t.Errorf("expected JP, got %q", cr.Code)
	}
}

// TestBlockCommentIsSkipped verifies that /* ... */ comments are stripped.
func TestBlockCommentIsSkipped(t *testing.T) {
	body := "/* block comment */ geoip country DE;"
	rules, _, err := ParseMatchClients([]byte(body), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	cr, ok := rules[0].(CountryRule)
	if !ok {
		t.Fatalf("expected CountryRule, got %T", rules[0])
	}
	if cr.Code != "DE" {
		t.Errorf("expected DE, got %q", cr.Code)
	}
}

// TestEmptyBodyProducesNoRules verifies that an empty body is accepted without error.
func TestEmptyBodyProducesNoRules(t *testing.T) {
	rules, _, err := ParseMatchClients([]byte(""), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

// TestFirstGeoRuleViewNoGeoRules verifies that views containing only
// non-geo rules (any/IP/CIDR) report found=false.
func TestFirstGeoRuleViewNoGeoRules(t *testing.T) {
	views := []View{
		{
			Name:         "internal",
			MatchClients: []MatchRule{AnyRule{}, IPRule{IP: netip.MustParseAddr("192.0.2.8")}},
			Line:         3,
			Source:       "named.conf",
		},
		{
			Name:         "external",
			MatchClients: []MatchRule{CIDRRule{Prefix: netip.MustParsePrefix("198.51.100.0/26")}},
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
			MatchClients: []MatchRule{AnyRule{}},
			Line:         1,
			Source:       "named.conf",
		},
		{
			Name:         "geo-view",
			MatchClients: []MatchRule{CountryRule{Code: "JP"}},
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
			MatchClients: []MatchRule{ASNRule{ASN: 64500}},
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
			MatchClients: []MatchRule{IPRule{IP: netip.MustParseAddr("203.0.113.1")}},
			Line:         2,
			Source:       "named.conf",
		},
		{
			Name: "first-geo",
			// Geo rule appears after a non-geo rule within the same view.
			MatchClients: []MatchRule{AnyRule{}, CountryRule{Code: "KR"}},
			Line:         10,
			Source:       "first.conf",
		},
		{
			Name:         "second-geo",
			MatchClients: []MatchRule{ASNRule{ASN: 64501}},
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

// TestFirstGeoRuleViewEmptyViews verifies that nil and empty view slices
// report found=false.
func TestFirstGeoRuleViewEmptyViews(t *testing.T) {
	for _, views := range [][]View{nil, {}} {
		name, source, line, found := FirstGeoRuleView(views)
		if found {
			t.Errorf("views=%#v: expected found=false, got true (view=%q source=%q line=%d)", views, name, source, line)
		}
	}
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
	rules, _, err := ParseMatchClients([]byte(body), "example.conf", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// RU, KR, JP, AS4134, AS17621, CN, HK, MO, 192.0.2.8, 198.51.100.0/26, any = 11 rules.
	if len(rules) != 11 {
		t.Fatalf("expected 11 rules, got %d", len(rules))
	}
}
