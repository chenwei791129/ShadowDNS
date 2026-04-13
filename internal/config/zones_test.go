package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Task 2.2 tests
// ---------------------------------------------------------------------------

// Test 2.2-1: Multiple views with ordered rules across named.conf + master.zones.
func TestLoadNamedConf_ViewsAndRulesOrder(t *testing.T) {
	dir := t.TempDir()

	masterZones := `
view "view-th" {
	match-clients {
		geoip country TH;
		geoip country SG;
	};
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_view-th.fwd";
	};
};

view "view-other" {
	match-clients {
		any;
	};
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_view-other.fwd";
	};
};
`

	namedConf := `options {
	directory "/etc/namedb";
};

include "master.zones";
`
	writeFile(t, filepath.Join(dir, "master.zones"), masterZones)
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(cfg.Views))
	}
	if cfg.Views[0].Name != "view-th" {
		t.Errorf("views[0] name: got %q, want %q", cfg.Views[0].Name, "view-th")
	}
	if cfg.Views[1].Name != "view-other" {
		t.Errorf("views[1] name: got %q, want %q", cfg.Views[1].Name, "view-other")
	}
	// Check rule order in view-th
	rules0 := cfg.Views[0].MatchClients
	if len(rules0) != 2 {
		t.Fatalf("view-th: expected 2 rules, got %d", len(rules0))
	}
	r0, ok := rules0[0].(CountryRule)
	if !ok || r0.Code != "TH" {
		t.Errorf("view-th rule[0]: got %T %+v, want CountryRule{TH}", rules0[0], rules0[0])
	}
	r1, ok := rules0[1].(CountryRule)
	if !ok || r1.Code != "SG" {
		t.Errorf("view-th rule[1]: got %T %+v, want CountryRule{SG}", rules0[1], rules0[1])
	}
	// Check view-other has AnyRule
	rules1 := cfg.Views[1].MatchClients
	if len(rules1) != 1 {
		t.Fatalf("view-other: expected 1 rule, got %d", len(rules1))
	}
	if _, ok := rules1[0].(AnyRule); !ok {
		t.Errorf("view-other rule[0]: got %T, want AnyRule", rules1[0])
	}
}

// Test 2.2-2: Zone file path resolution — relative path + non-empty Directory → absolute.
func TestLoadNamedConf_ZoneFilePathResolution(t *testing.T) {
	dir := t.TempDir()

	masterZones := `
view "view-th" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "master/group-a/example.com_view-th.fwd";
	};
};
`
	namedConf := `options {
	directory "/etc/namedb";
};
include "master.zones";
`
	writeFile(t, filepath.Join(dir, "master.zones"), masterZones)
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
	zones := cfg.Views[0].Zones
	if len(zones) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(zones))
	}
	want := "/etc/namedb/master/group-a/example.com_view-th.fwd"
	if zones[0].File != want {
		t.Errorf("zone file: got %q, want %q", zones[0].File, want)
	}
}

// Test 2.2-3: Same zone name across two views produces two separate entries.
func TestLoadNamedConf_SameZoneNameAcrossViews(t *testing.T) {
	dir := t.TempDir()

	masterZones := `
view "view-a" {
	match-clients { geoip country US; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_view-a.fwd";
	};
};

view "view-b" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_view-b.fwd";
	};
};
`
	namedConf := `options {
	directory "/etc/namedb";
};
include "master.zones";
`
	writeFile(t, filepath.Join(dir, "master.zones"), masterZones)
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(cfg.Views))
	}
	z0 := cfg.Views[0].Zones[0]
	z1 := cfg.Views[1].Zones[0]
	if z0.Name != "example.com" || z1.Name != "example.com" {
		t.Errorf("zone names: got %q and %q, both want %q", z0.Name, z1.Name, "example.com")
	}
	if z0.File == z1.File {
		t.Errorf("zone files should differ across views, both got %q", z0.File)
	}
	if z0.File != "/etc/namedb/master/example.com_view-a.fwd" {
		t.Errorf("view-a zone file: got %q", z0.File)
	}
	if z1.File != "/etc/namedb/master/example.com_view-b.fwd" {
		t.Errorf("view-b zone file: got %q", z1.File)
	}
}

// Test 2.2-4: include directive is followed; views from included file appear after parent.
func TestLoadNamedConf_IncludeFollowed(t *testing.T) {
	dir := t.TempDir()

	// named.conf has one view, includes extra.zones which has another
	extraZones := `
view "view-extra" {
	match-clients { any; };
	zone "extra.com" {
		type master;
		file "/etc/namedb/master/extra.com.fwd";
	};
};
`
	namedConf := `options {
	directory "/etc/namedb";
};

view "view-main" {
	match-clients { geoip country TH; };
	zone "main.com" {
		type master;
		file "/etc/namedb/master/main.com.fwd";
	};
};

include "extra.zones";
`
	writeFile(t, filepath.Join(dir, "extra.zones"), extraZones)
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(cfg.Views))
	}
	if cfg.Views[0].Name != "view-main" {
		t.Errorf("views[0]: got %q, want %q", cfg.Views[0].Name, "view-main")
	}
	if cfg.Views[1].Name != "view-extra" {
		t.Errorf("views[1]: got %q, want %q", cfg.Views[1].Name, "view-extra")
	}
}

// Test 2.2-5: logging { ... }; block at top level is silently ignored.
func TestLoadNamedConf_LoggingBlockIgnored(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

logging {
	channel default_syslog {
		syslog daemon;
		severity info;
	};
	category default { default_syslog; };
};

view "view-a" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
}

// Test 2.2-6: recursion no; inside a view does not cause an error.
func TestLoadNamedConf_RecursionInsideView(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	recursion no;
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("recursion inside view should not cause error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
}

// Test 2.2-7: Comments (// # /* */) are stripped.
func TestLoadNamedConf_CommentsStripped(t *testing.T) {
	dir := t.TempDir()

	namedConf := `// top-level comment
options {
	directory "/etc/namedb"; // inline comment
	# hash comment
	/* block
	   comment */
};

/* another block comment */
view "view-a" { // comment
	match-clients { any; }; # hash
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
	if cfg.Options.Directory != "/etc/namedb" {
		t.Errorf("Directory: got %q, want %q", cfg.Options.Directory, "/etc/namedb")
	}
}

// Test 2.2-8: Multiple zones in one view are returned in declaration order.
func TestLoadNamedConf_MultipleZonesOrdered(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	zone "alpha.com" {
		type master;
		file "/etc/namedb/master/alpha.com.fwd";
	};
	zone "beta.com" {
		type master;
		file "/etc/namedb/master/beta.com.fwd";
	};
	zone "gamma.com" {
		type master;
		file "/etc/namedb/master/gamma.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	zones := cfg.Views[0].Zones
	if len(zones) != 3 {
		t.Fatalf("expected 3 zones, got %d", len(zones))
	}
	wantNames := []string{"alpha.com", "beta.com", "gamma.com"}
	for i, wn := range wantNames {
		if zones[i].Name != wn {
			t.Errorf("zone[%d]: got %q, want %q", i, zones[i].Name, wn)
		}
	}
}

// Test 2.2-9: Domain names are lowercased and trailing dot stripped.
func TestLoadNamedConf_DomainNameNormalized(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	zone "EXAMPLE.COM." {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Views[0].Zones[0].Name != "example.com" {
		t.Errorf("zone name: got %q, want %q", cfg.Views[0].Zones[0].Name, "example.com")
	}
}

// Test 2.2-10: Zone Source and Line fields are populated.
func TestLoadNamedConf_ZoneSourceAndLine(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	confPath := filepath.Join(dir, "named.conf")
	writeFile(t, confPath, namedConf)

	cfg, err := LoadNamedConf(confPath, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	z := cfg.Views[0].Zones[0]
	if z.Source != confPath {
		t.Errorf("zone source: got %q, want %q", z.Source, confPath)
	}
	if z.Line <= 0 {
		t.Errorf("zone line: got %d, want >0", z.Line)
	}
}

// ---------------------------------------------------------------------------
// Task 2.4 tests
// ---------------------------------------------------------------------------

// Test 2.4-1: Middle view with `any` triggers warning mentioning that view and shadowed views.
func TestLoadNamedConf_AnyViewNotLastWarns(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-th" {
	match-clients { geoip country TH; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_th.fwd";
	};
};

view "view-other" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_other.fwd";
	};
};

view "view-eu" {
	match-clients { geoip country DE; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_eu.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 3 {
		t.Fatalf("expected 3 views, got %d", len(cfg.Views))
	}

	logOutput := buf.String()
	// Should warn about view-other shadowing view-eu
	if !strings.Contains(logOutput, "view-other") {
		t.Errorf("warning should mention the shadowing view 'view-other', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "view-eu") {
		t.Errorf("warning should mention the shadowed view 'view-eu', got: %s", logOutput)
	}
}

// Test 2.4-2: Last view with `any` does not trigger warning.
func TestLoadNamedConf_AnyViewLastNoWarning(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-th" {
	match-clients { geoip country TH; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_th.fwd";
	};
};

view "view-other" {
	match-clients { geoip country SG; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_other.fwd";
	};
};

view "view-any" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_any.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	// Should NOT warn — any is last
	if strings.Contains(logOutput, "shadow") {
		t.Errorf("should not warn when any is last, got log: %s", logOutput)
	}
}

// Test 2.4-3: View with `any` mixed with other rules still triggers warning when non-last.
func TestLoadNamedConf_AnyMixedWithOtherRulesWarns(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-mixed" {
	match-clients {
		geoip country TH;
		any;
	};
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_mixed.fwd";
	};
};

view "view-last" {
	match-clients { geoip country SG; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com_last.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "view-mixed") {
		t.Errorf("warning should mention 'view-mixed', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "view-last") {
		t.Errorf("warning should mention the shadowed 'view-last', got: %s", logOutput)
	}
}

// ---------------------------------------------------------------------------
// Task 2.6 tests
// ---------------------------------------------------------------------------

// Test 2.6-1: zone type slave returns error mentioning zone name and type.
func TestLoadNamedConf_ZoneTypeSlave(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	zone "x.com" {
		type slave;
		file "/etc/namedb/master/x.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err == nil {
		t.Fatal("expected error for zone type slave, got nil")
	}
	if !strings.Contains(err.Error(), "x.com") {
		t.Errorf("error should mention zone name 'x.com', got: %v", err)
	}
	if !strings.Contains(err.Error(), "slave") {
		t.Errorf("error should mention type 'slave', got: %v", err)
	}
}

// Test 2.6-2: zone type forward returns error mentioning zone name and type.
func TestLoadNamedConf_ZoneTypeForward(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	zone "x.com" {
		type forward;
		forwarders { 8.8.8.8; };
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err == nil {
		t.Fatal("expected error for zone type forward, got nil")
	}
	if !strings.Contains(err.Error(), "x.com") {
		t.Errorf("error should mention zone name 'x.com', got: %v", err)
	}
	if !strings.Contains(err.Error(), "forward") {
		t.Errorf("error should mention type 'forward', got: %v", err)
	}
}

// Test 2.6-3: Top-level dnssec-enable returns error.
func TestLoadNamedConf_TopLevelDnssecEnable(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

dnssec-enable yes;

view "view-a" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err == nil {
		t.Fatal("expected error for dnssec-enable, got nil")
	}
	if !strings.Contains(err.Error(), "dnssec-enable") {
		t.Errorf("error should mention 'dnssec-enable', got: %v", err)
	}
}

// Test 2.6-4: Top-level allow-update returns error.
func TestLoadNamedConf_TopLevelAllowUpdate(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

allow-update { any; };

view "view-a" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err == nil {
		t.Fatal("expected error for allow-update, got nil")
	}
	if !strings.Contains(err.Error(), "allow-update") {
		t.Errorf("error should mention 'allow-update', got: %v", err)
	}
}

// Test 2.6-5: Inside view, unknown directive returns error.
func TestLoadNamedConf_UnknownDirectiveInsideView(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	unknown-directive "foo";
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err == nil {
		t.Fatal("expected error for unknown directive inside view, got nil")
	}
	if !strings.Contains(err.Error(), "unknown-directive") {
		t.Errorf("error should mention directive name 'unknown-directive', got: %v", err)
	}
}

// Test 2.6-6: recursion no; inside a view is accepted (sanity check after 2.6 rules).
func TestLoadNamedConf_RecursionInsideViewAccepted(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-a" {
	match-clients { any; };
	recursion no;
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), slog.Default())
	if err != nil {
		t.Fatalf("recursion no inside view should be accepted, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
