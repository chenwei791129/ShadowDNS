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
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
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
	got := m.Resolve(netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("192.0.2.2"))
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
	got := m.Resolve(netip.MustParseAddr("10.0.0.99"), netip.MustParseAddr("10.0.0.99"))
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
	got := m.Resolve(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.1"))
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// --- fail-closed for dropped (unevaluable) match-clients rules ----------------
//
// When the config-loader drops a match-clients rule it cannot evaluate (a
// named-acl reference, `!` negation, or nested group), that rule never reaches
// the matcher: the view's Rules slice simply omits it. A view whose entire
// match-clients set was dropped therefore arrives here with empty Rules. These
// tests pin the fail-closed contract: an empty/reduced rule set can only narrow,
// never widen, the clients a view serves — a dropped rule is never promoted to
// `any`.

// droppedOnlyView models a view whose only match-clients rule was dropped: its
// Rules slice is empty.
func droppedOnlyView(name string) NamedRuleSet {
	return NamedRuleSet{Name: name, Rules: nil}
}

// Scenario "View with only a dropped rule matches no client": the view is not
// selected and evaluation proceeds to subsequent views.
func TestMatcher_DroppedOnlyViewMatchesNoClient(t *testing.T) {
	m := &Matcher{
		Views: []NamedRuleSet{
			droppedOnlyView("internal"),     // match-clients { internal-net; } — dropped
			ipView("other", "198.51.100.7"), // a later view that does match
		},
	}
	got := m.Resolve(netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("198.51.100.7"))
	if got != "other" {
		t.Errorf("expected evaluation to fall through the dropped-rule view to 'other', got %q", got)
	}
}

// Scenario "Dropped rule does not widen a view with other rules": a view with a
// surviving CIDR rule (the dropped rule absent) is not selected for a client
// outside the CIDR.
func TestMatcher_DroppedRuleDoesNotWidenView(t *testing.T) {
	// match-clients { internal-net; 192.0.2.0/24; } → internal-net dropped, only
	// the CIDR survives.
	m := &Matcher{
		Views: []NamedRuleSet{
			cidrView("internal", "192.0.2.0/24"),
		},
	}
	// Source IP is outside the surviving CIDR; the dropped rule must not match.
	got := m.Resolve(netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("198.51.100.7"))
	if got != "" {
		t.Errorf("dropped rule must never match; expected no-view result, got %q", got)
	}
}

// Scenario "Dropped rule is never treated as a catch-all": with no later `any`
// view, a client matching no other view gets the explicit no-view result ("",
// which the caller turns into REFUSED), not the dropped-rule view.
func TestMatcher_DroppedRuleNeverCatchAll(t *testing.T) {
	m := &Matcher{
		Views: []NamedRuleSet{
			droppedOnlyView("internal"),
			ipView("other", "192.0.2.1"),
		},
	}
	got := m.Resolve(netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("203.0.113.9"))
	if got != "" {
		t.Errorf("a dropped rule must never act as a catch-all; expected no-view result, got %q", got)
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
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
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
		got := m.Resolve(netip.MustParseAddr("10.1.2.3"), netip.MustParseAddr("10.1.2.3"))
		if got != "internal" {
			t.Errorf("expected internal, got %q", got)
		}
	})

	t.Run("IP outside CIDR", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("11.0.0.1"), netip.MustParseAddr("11.0.0.1"))
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
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
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
	defer func() { _ = db.Close() }()

	m := &Matcher{
		Views: []NamedRuleSet{
			countryView("thai-view", "TH"),
			countryView("japan-view", "JP"),
			anyView("other"),
		},
		Country: db,
	}

	t.Run("TH IP matches thai-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
		if got != "thai-view" {
			t.Errorf("expected thai-view, got %q", got)
		}
	})

	t.Run("JP IP matches japan-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("198.51.100.1"), netip.MustParseAddr("198.51.100.1"))
		if got != "japan-view" {
			t.Errorf("expected japan-view, got %q", got)
		}
	})

	t.Run("unknown country IP falls through to any", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.1"))
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
		got := m2.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
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
	got := m.Resolve(netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("203.0.113.1"))
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
	defer func() { _ = db.Close() }()

	m := &Matcher{
		Views: []NamedRuleSet{
			asnView("asn64500-view", 64500),
			asnView("asn64501-view", 64501),
			anyView("other"),
		},
		ASN: db,
	}

	t.Run("AS64500 IP matches asn64500-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("203.0.113.1"))
		if got != "asn64500-view" {
			t.Errorf("expected asn64500-view, got %q", got)
		}
	})

	t.Run("AS64501 IP matches asn64501-view", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("203.0.113.2"), netip.MustParseAddr("203.0.113.2"))
		if got != "asn64501-view" {
			t.Errorf("expected asn64501-view, got %q", got)
		}
	})

	t.Run("unknown ASN IP falls through to any", func(t *testing.T) {
		got := m.Resolve(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.1"))
		if got != "other" {
			t.Errorf("expected other, got %q", got)
		}
	})
}

// --- Dual-address resolution (srcIP vs geoIP) ---------------------------------

func TestMatcher_DualAddress_GeoAndACLRulesEvaluateDifferentAddresses(t *testing.T) {
	// Spec scenario: CIDR rules evaluate srcIP, country rules evaluate geoIP,
	// both within a single resolution.
	path := buildCountryMMDB(t)
	db, err := OpenCountryDB(path)
	if err != nil {
		t.Fatalf("OpenCountryDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Matcher{
		Views: []NamedRuleSet{
			cidrView("view-internal", "192.0.2.0/24"),
			countryView("view-asia", "TW"),
		},
		Country: db,
	}

	// srcIP is outside the CIDR (and maps to JP in the country db, so a buggy
	// country lookup against srcIP would also fail to match TW); geoIP maps to TW.
	srcIP := netip.MustParseAddr("198.51.100.1")
	geoIP := netip.MustParseAddr("203.0.113.0")
	got := m.Resolve(srcIP, geoIP)
	if got != "view-asia" {
		t.Errorf("expected view-asia (CIDR misses srcIP, country TW hits geoIP), got %q", got)
	}
}

func TestMatcher_DualAddress_GeoIPNeverSatisfiesCIDRRule(t *testing.T) {
	// Spec scenario: the geo lookup address is client-controlled (ECS-derived)
	// and must never satisfy ACL-style CIDR rules.
	m := &Matcher{
		Views: []NamedRuleSet{
			cidrView("view-internal", "192.0.2.0/24"),
			anyView("fallback"),
		},
	}

	// geoIP 192.0.2.5 is inside the CIDR, but only srcIP may satisfy it.
	srcIP := netip.MustParseAddr("203.0.113.7")
	geoIP := netip.MustParseAddr("192.0.2.5")
	got := m.Resolve(srcIP, geoIP)
	if got != "fallback" {
		t.Errorf("expected fallback (geoIP must not satisfy CIDR rule), got %q", got)
	}
}

func TestMatcher_DualAddress_GeoIPNeverSatisfiesIPRule(t *testing.T) {
	// Same anti-spoofing guarantee for exact-IP rules.
	m := &Matcher{
		Views: []NamedRuleSet{
			ipView("view-pinned", "192.0.2.5"),
		},
	}

	srcIP := netip.MustParseAddr("203.0.113.7")
	geoIP := netip.MustParseAddr("192.0.2.5")
	got := m.Resolve(srcIP, geoIP)
	if got != "" {
		t.Errorf("expected no view (geoIP must not satisfy IP rule), got %q", got)
	}
}

func TestMatcher_DualAddress_ASNRuleEvaluatesGeoIP(t *testing.T) {
	// Same geo-address routing guarantee as country rules: ASN rules must
	// read geoIP, never srcIP.
	path := buildASNMMDB(t)
	db, err := OpenASNDB(path)
	if err != nil {
		t.Fatalf("OpenASNDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Matcher{
		Views: []NamedRuleSet{
			asnView("asn64501-view", 64501),
			anyView("other"),
		},
		ASN: db,
	}

	t.Run("geoIP ASN selects the view", func(t *testing.T) {
		// srcIP maps to ASN 64500, geoIP to ASN 64501 — only a geoIP lookup
		// can match the 64501 view.
		got := m.Resolve(netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("203.0.113.2"))
		if got != "asn64501-view" {
			t.Errorf("expected asn64501-view (ASN rule reads geoIP), got %q", got)
		}
	})

	t.Run("srcIP ASN does not leak into the lookup", func(t *testing.T) {
		// srcIP maps to ASN 64501 but geoIP is absent from the db; a buggy
		// srcIP lookup would match, the correct geoIP lookup falls through.
		got := m.Resolve(netip.MustParseAddr("203.0.113.2"), netip.MustParseAddr("10.0.0.1"))
		if got != "other" {
			t.Errorf("expected other (srcIP ASN must not satisfy the rule), got %q", got)
		}
	})
}

func TestMatcher_ZeroGeoIPFallsBackToSrcIP(t *testing.T) {
	// Defense against caller mistakes: a zero-value geoIP (a caller passing
	// netip.Addr{} instead of the source IP) must behave like single-address
	// resolution, not silently disable every geo rule.
	path := buildCountryMMDB(t)
	db, err := OpenCountryDB(path)
	if err != nil {
		t.Fatalf("OpenCountryDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Matcher{
		Views: []NamedRuleSet{
			countryView("th-view", "TH"),
			anyView("other"),
		},
		Country: db,
	}

	got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.Addr{})
	if got != "th-view" {
		t.Errorf("expected th-view (zero geoIP falls back to srcIP), got %q", got)
	}
}

// --- Edge cases --------------------------------------------------------------

func TestMatcher_EmptyViews(t *testing.T) {
	m := &Matcher{Views: []NamedRuleSet{}}
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
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
	got := m.Resolve(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.1"))
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
	got := m.Resolve(zero, zero)
	if got != "catch-all" {
		t.Errorf("expected catch-all, got %q", got)
	}
}
