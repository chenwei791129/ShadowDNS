package view

import (
	"net/netip"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// --- helpers -----------------------------------------------------------------

func ipView(name string, ips ...string) NamedRuleSet {
	rules := make([]config.MatchRule, len(ips))
	for i, s := range ips {
		rules[i] = config.IPRule{IP: netip.MustParseAddr(s)}
	}
	return NamedRuleSet{Name: name, Rules: rules}
}

func cidrView(name string, cidr string) NamedRuleSet {
	return NamedRuleSet{
		Name:  name,
		Rules: []config.MatchRule{config.CIDRRule{Prefix: netip.MustParsePrefix(cidr)}},
	}
}

func anyView(name string) NamedRuleSet {
	return NamedRuleSet{
		Name:  name,
		Rules: []config.MatchRule{config.AnyRule{}},
	}
}

func countryView(name string, code string) NamedRuleSet {
	return NamedRuleSet{
		Name:  name,
		Rules: []config.MatchRule{config.CountryRule{Code: code}},
	}
}

func asnView(name string, asn uint32) NamedRuleSet {
	return NamedRuleSet{
		Name:  name,
		Rules: []config.MatchRule{config.ASNRule{ASN: asn}},
	}
}

// --- first-match semantics ---------------------------------------------------

func TestMatcher_FirstMatchWins(t *testing.T) {
	// 3 views; first view's IP rule matches clientIP → returns "view1".
	m := &Matcher{
		Views: []NamedRuleSet{
			ipView("view1", "192.0.2.1"),
			ipView("view2", "192.0.2.2"),
			anyView("view3"),
		},
	}
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"))
	if got != "view1" {
		t.Errorf("expected view1, got %q", got)
	}
}

func TestMatcher_SecondViewMatchesWhenFirstDoesNot(t *testing.T) {
	// First view IP doesn't match; second view's IP matches.
	m := &Matcher{
		Views: []NamedRuleSet{
			ipView("view1", "192.0.2.1"),
			ipView("view2", "192.0.2.2"),
			anyView("view3"),
		},
	}
	got := m.Resolve(netip.MustParseAddr("192.0.2.2"))
	if got != "view2" {
		t.Errorf("expected view2, got %q", got)
	}
}

func TestMatcher_FallbackToAnyWhenNothingElseMatches(t *testing.T) {
	// Last view has AnyRule — catches everything that didn't match earlier.
	m := &Matcher{
		Views: []NamedRuleSet{
			ipView("view1", "192.0.2.1"),
			ipView("view2", "192.0.2.2"),
			anyView("catch-all"),
		},
	}
	got := m.Resolve(netip.MustParseAddr("10.0.0.99"))
	if got != "catch-all" {
		t.Errorf("expected catch-all, got %q", got)
	}
}

func TestMatcher_NoMatchReturnsEmptyString(t *testing.T) {
	// No AnyRule anywhere; IP doesn't match any view → "".
	m := &Matcher{
		Views: []NamedRuleSet{
			ipView("view1", "192.0.2.1"),
			ipView("view2", "192.0.2.2"),
		},
	}
	got := m.Resolve(netip.MustParseAddr("10.0.0.1"))
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestMatcher_FirstRuleInViewDecides(t *testing.T) {
	// View "mixed" has two rules: IP 192.0.2.2 first, then AnyRule.
	// Client IP is 192.0.2.1, which matches no IP rule but would match AnyRule
	// inside the same view — however the earlier view "view1" captures it first.
	// This test verifies intra-view first-rule semantics too.
	m := &Matcher{
		Views: []NamedRuleSet{
			{
				Name: "mixed",
				Rules: []config.MatchRule{
					config.IPRule{IP: netip.MustParseAddr("192.0.2.99")}, // doesn't match
					config.AnyRule{}, // matches everything
				},
			},
			ipView("other", "192.0.2.1"),
		},
	}
	// 192.0.2.1 should match "mixed" via its AnyRule (second rule),
	// because that view is evaluated first.
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"))
	if got != "mixed" {
		t.Errorf("expected mixed (AnyRule fires), got %q", got)
	}
}

// --- CIDR rules --------------------------------------------------------------

func TestMatcher_CIDRRule(t *testing.T) {
	m := &Matcher{
		Views: []NamedRuleSet{
			cidrView("internal", "10.0.0.0/8"),
			anyView("external"),
		},
	}

	t.Run("IP inside CIDR", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("10.1.2.3"))
		if got != "internal" {
			t.Errorf("expected internal, got %q", got)
		}
	})

	t.Run("IP outside CIDR", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("11.0.0.1"))
		if got != "external" {
			t.Errorf("expected external, got %q", got)
		}
	})
}

// --- Country rules with nil GeoIP -------------------------------------------

func TestMatcher_CountryRule_NilCountryDB_NoMatch(t *testing.T) {
	// When Country is nil, country rules must never match (not panic).
	m := &Matcher{
		Views: []NamedRuleSet{
			countryView("geo-view", "TH"),
			anyView("fallback"),
		},
		Country: nil,
	}
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"))
	if got != "fallback" {
		t.Errorf("expected fallback (country nil → no-match), got %q", got)
	}
}

// --- Country rules with real mmdb -------------------------------------------

func TestMatcher_CountryRule_WithRealDB(t *testing.T) {
	path := buildCountryMMDB(t)
	db, err := OpenCountryDB(path)
	if err != nil {
		t.Fatalf("OpenCountryDB: %v", err)
	}
	defer db.Close()

	m := &Matcher{
		Views: []NamedRuleSet{
			countryView("thai-view", "TH"),
			countryView("japan-view", "JP"),
			anyView("other"),
		},
		Country: db,
	}

	t.Run("TH IP matches thai-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("192.0.2.1"))
		if got != "thai-view" {
			t.Errorf("expected thai-view, got %q", got)
		}
	})

	t.Run("JP IP matches japan-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("198.51.100.1"))
		if got != "japan-view" {
			t.Errorf("expected japan-view, got %q", got)
		}
	})

	t.Run("unknown country IP falls through to any", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("10.0.0.1"))
		if got != "other" {
			t.Errorf("expected other, got %q", got)
		}
	})

	t.Run("country rule is case-insensitive at compare", func(t *testing.T) {
		// Rule has lowercase "th" — should still match TH from mmdb.
		m2 := &Matcher{
			Views: []NamedRuleSet{
				countryView("lower-th", "th"),
				anyView("other"),
			},
			Country: db,
		}
		got := m2.Resolve(netip.MustParseAddr("192.0.2.1"))
		if got != "lower-th" {
			t.Errorf("expected lower-th (case-insensitive), got %q", got)
		}
	})
}

// --- ASN rules with nil GeoIP ------------------------------------------------

func TestMatcher_ASNRule_NilASNDB_NoMatch(t *testing.T) {
	m := &Matcher{
		Views: []NamedRuleSet{
			asnView("asn-view", 64500),
			anyView("fallback"),
		},
		ASN: nil,
	}
	got := m.Resolve(netip.MustParseAddr("203.0.113.1"))
	if got != "fallback" {
		t.Errorf("expected fallback (ASN nil → no-match), got %q", got)
	}
}

// --- ASN rules with real mmdb ------------------------------------------------

func TestMatcher_ASNRule_WithRealDB(t *testing.T) {
	path := buildASNMMDB(t)
	db, err := OpenASNDB(path)
	if err != nil {
		t.Fatalf("OpenASNDB: %v", err)
	}
	defer db.Close()

	m := &Matcher{
		Views: []NamedRuleSet{
			asnView("asn64500-view", 64500),
			asnView("asn64501-view", 64501),
			anyView("other"),
		},
		ASN: db,
	}

	t.Run("AS64500 IP matches asn64500-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("203.0.113.1"))
		if got != "asn64500-view" {
			t.Errorf("expected asn64500-view, got %q", got)
		}
	})

	t.Run("AS64501 IP matches asn64501-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("203.0.113.2"))
		if got != "asn64501-view" {
			t.Errorf("expected asn64501-view, got %q", got)
		}
	})

	t.Run("unknown ASN IP falls through to any", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("10.0.0.1"))
		if got != "other" {
			t.Errorf("expected other, got %q", got)
		}
	})
}

// --- Edge cases --------------------------------------------------------------

func TestMatcher_EmptyViews(t *testing.T) {
	m := &Matcher{Views: []NamedRuleSet{}}
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"))
	if got != "" {
		t.Errorf("expected empty string for empty views, got %q", got)
	}
}

func TestMatcher_ViewWithNoRules(t *testing.T) {
	m := &Matcher{
		Views: []NamedRuleSet{
			{Name: "no-rules", Rules: nil},
			anyView("fallback"),
		},
	}
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"))
	// "no-rules" has no rules, so it never matches; fallback catches.
	if got != "fallback" {
		t.Errorf("expected fallback, got %q", got)
	}
}

func TestMatcher_ZeroClientIP_DoesNotPanic(t *testing.T) {
	m := &Matcher{
		Views: []NamedRuleSet{
			anyView("catch-all"),
		},
	}
	var zero netip.Addr
	// Must not panic; AnyRule always returns true regardless of IP.
	got := m.Resolve(zero)
	if got != "catch-all" {
		t.Errorf("expected catch-all, got %q", got)
	}
}
