package config

import (
	"net/netip"
	"strings"
	"testing"
)

// TestCountryRuleUppercase verifies that an explicit uppercase country code is parsed correctly.
func TestCountryRuleUppercase(t *testing.T) {
	rules, err := ParseMatchClients([]byte("geoip country TH;"), "test.conf", 1)
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
	rules, err := ParseMatchClients([]byte("geoip country th;"), "test.conf", 1)
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
	rules, err := ParseMatchClients([]byte(`geoip asnum "AS4134 Chinanet";`), "test.conf", 1)
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
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
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
	rules, err := ParseMatchClients([]byte("any;"), "test.conf", 1)
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
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
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

// TestUnknownRuleFormReturnsError verifies that an unrecognised rule form
// returns a descriptive error.
func TestUnknownRuleFormReturnsError(t *testing.T) {
	_, err := ParseMatchClients([]byte("geoip whatever;"), "rules.conf", 10)
	if err == nil {
		t.Fatal("expected error for unknown rule, got nil")
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
	rules, err := ParseMatchClients([]byte(body), "test.conf", 1)
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
	rules, err := ParseMatchClients([]byte(""), "test.conf", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
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
	rules, err := ParseMatchClients([]byte(body), "example.conf", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// RU, KR, JP, AS4134, AS17621, CN, HK, MO, 192.0.2.8, 198.51.100.0/26, any = 11 rules.
	if len(rules) != 11 {
		t.Fatalf("expected 11 rules, got %d", len(rules))
	}
}
