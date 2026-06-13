package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// ---------------------------------------------------------------------------
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

// Test 2.2-5: logging { ... }; block at top level is parsed (not silently ignored).
// The block in this fixture uses a syslog channel with no category queries, so
// query logging is disabled (Config.QueryLog == nil). Views are unaffected.
func TestLoadNamedConf_LoggingBlockParsed(t *testing.T) {
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
	// No category queries in this fixture, so query logging must be disabled.
	if cfg.QueryLog != nil {
		t.Errorf("expected QueryLog to be nil (no category queries), got %+v", cfg.QueryLog)
	}
}

// TestLoadNamedConf_LoggingBlockEnablesQueryLog verifies that a logging block
// with a file channel and category queries produces a non-nil QueryLog.
func TestLoadNamedConf_LoggingBlockEnablesQueryLog(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log" versions 3 size 5000m;
		severity debug;
		print-severity yes;
		print-time yes;
		print-category yes;
	};
	category queries { queries_log; };
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
	if cfg.QueryLog == nil {
		t.Fatal("expected QueryLog to be non-nil, got nil")
	}
	if cfg.QueryLog.FilePath != "/var/log/shadowdns/queries.log" {
		t.Errorf("QueryLog.FilePath: got %q, want %q", cfg.QueryLog.FilePath, "/var/log/shadowdns/queries.log")
	}
	if !cfg.QueryLog.RotationIgnored {
		t.Error("QueryLog.RotationIgnored: got false, want true")
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	cfg, err := LoadNamedConf(confPath, zap.NewNop())
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
// ---------------------------------------------------------------------------

// Test 2.4-1: Middle view with `any` triggers warning mentioning that view and shadowed views.
func TestLoadNamedConf_AnyViewNotLastWarns(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

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
	logger := newTestLogger(&buf)

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
	logger := newTestLogger(&buf)

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

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
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

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("recursion no inside view should be accepted, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Top-level zones + implicit _default view (implicit-default-view)
// ---------------------------------------------------------------------------

// Task 1.1-a: a top-level zone is parsed with the same zone-body rules as an
// in-view zone (name, type, absolute file path).
func TestLoadNamedConf_TopLevelZoneParsed(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
	type master;
	file "/etc/namedb/master/example.com.fwd";
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 synthesized view, got %d", len(cfg.Views))
	}
	zones := cfg.Views[0].Zones
	if len(zones) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(zones))
	}
	if zones[0].Name != "example.com" {
		t.Errorf("zone name: got %q, want example.com", zones[0].Name)
	}
	if zones[0].Type != ZoneTypeMaster {
		t.Errorf("zone type: got %q, want master", zones[0].Type)
	}
	if zones[0].File != "/etc/namedb/master/example.com.fwd" {
		t.Errorf("zone file: got %q", zones[0].File)
	}
}

// Task 1.1-b: a top-level zone with an unsupported type fails with the same
// error as the same type declared inside a view.
func TestLoadNamedConf_TopLevelZoneTypeSlave(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
	type slave;
	file "/etc/namedb/master/example.com.fwd";
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	_, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err == nil {
		t.Fatal("expected error for top-level zone type slave, got nil")
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error should mention zone name 'example.com', got: %v", err)
	}
	if !strings.Contains(err.Error(), "slave") {
		t.Errorf("error should mention type 'slave', got: %v", err)
	}
}

// Task 1.1-c (spec scenario "Top-level zone file path resolves like an in-view
// zone"): a relative file path is resolved against options.directory when the
// options block precedes the zone.
func TestLoadNamedConf_TopLevelZoneRelativePathWithOptions(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
	type master;
	file "master/example.com.fwd";
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/etc/namedb/master/example.com.fwd"
	if got := cfg.Views[0].Zones[0].File; got != want {
		t.Errorf("zone file: got %q, want %q", got, want)
	}
}

// Task 1.1-d: a top-level zone body that omits type and file is tolerated
// exactly as the same omission is tolerated inside a view.
func TestLoadNamedConf_TopLevelZoneMissingTypeFileTolerated(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("missing type/file should be tolerated, got: %v", err)
	}
	if len(cfg.Views) != 1 || len(cfg.Views[0].Zones) != 1 {
		t.Fatalf("expected 1 view with 1 zone, got %d views", len(cfg.Views))
	}
	z := cfg.Views[0].Zones[0]
	if z.Type != "" || z.File != "" {
		t.Errorf("expected empty type/file, got type=%q file=%q", z.Type, z.File)
	}
}

// Task 1.2-a (spec scenario "Viewless configuration is served via the implicit
// _default view"): two top-level zones synthesize a single _default view whose
// match-clients equals parsing `match-clients { any; };` and whose zones keep
// declaration order.
func TestLoadNamedConf_ViewlessSynthesizesDefaultView(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
	type master;
	file "/etc/namedb/master/example.com.fwd";
};

zone "example.net" {
	type master;
	file "/etc/namedb/master/example.net.fwd";
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(cfg.Views))
	}
	v := cfg.Views[0]
	if v.Name != "_default" {
		t.Errorf("view name: got %q, want _default", v.Name)
	}
	// The synthesized rule set must be value-identical to parsing `any;`.
	wantRules, perr := ParseMatchClients([]byte("any;"), "test", 1)
	if perr != nil {
		t.Fatalf("ParseMatchClients(any): %v", perr)
	}
	if !reflect.DeepEqual(v.MatchClients, wantRules) {
		t.Errorf("match-clients: got %+v, want %+v (== parsing `any;`)", v.MatchClients, wantRules)
	}
	if len(v.Zones) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(v.Zones))
	}
	if v.Zones[0].Name != "example.com" || v.Zones[1].Name != "example.net" {
		t.Errorf("zone order: got %q,%q want example.com,example.net", v.Zones[0].Name, v.Zones[1].Name)
	}
}

// Task 1.2-b (spec scenario "Configuration with no views and no zones stays
// empty"): an options-only config synthesizes nothing.
func TestLoadNamedConf_NoViewsNoZonesStaysEmpty(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 0 {
		t.Fatalf("expected 0 views (no synthesis), got %d", len(cfg.Views))
	}
}

// Task 1.2-c (spec scenario "Explicitly declared view named _default is treated
// as a regular view"): an explicit view "_default" with no top-level zones is
// returned unchanged and nothing extra is synthesized.
func TestLoadNamedConf_ExplicitDefaultViewTreatedAsRegular(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "_default" {
	match-clients { any; };
	zone "example.com" {
		type master;
		file "/etc/namedb/master/example.com.fwd";
	};
};
`
	writeFile(t, filepath.Join(dir, "named.conf"), namedConf)

	cfg, err := LoadNamedConf(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Views) != 1 {
		t.Fatalf("expected exactly 1 view (no extra synthesis), got %d", len(cfg.Views))
	}
	if cfg.Views[0].Name != "_default" {
		t.Errorf("view name: got %q, want _default", cfg.Views[0].Name)
	}
	if len(cfg.Views[0].Zones) != 1 {
		t.Errorf("expected 1 zone in explicit view, got %d", len(cfg.Views[0].Zones))
	}
}

// Task 1.2-d (spec scenario "Duplicate top-level zone names warn once per name
// without failing"): zone "example.com" on lines 5 and 9 succeeds, keeps both
// entries, and logs exactly one warning naming both positions.
func TestLoadNamedConf_DuplicateTopLevelZoneNamesWarnOnce(t *testing.T) {
	dir := t.TempDir()

	core, obs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	// "example.com" name token lands on line 5 and again on line 9.
	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
	type master;
	file "/etc/namedb/master/a.fwd";
};
zone "example.com" {
	type master;
	file "/etc/namedb/master/b.fwd";
};
`
	confPath := filepath.Join(dir, "named.conf")
	writeFile(t, confPath, namedConf)

	cfg, err := LoadNamedConf(confPath, logger)
	if err != nil {
		t.Fatalf("duplicate top-level zone names must not be fatal, got: %v", err)
	}
	if len(cfg.Views) != 1 || len(cfg.Views[0].Zones) != 2 {
		t.Fatalf("expected 1 view with 2 zone entries (both retained), got %d views", len(cfg.Views))
	}
	if cfg.Views[0].Zones[0].File == cfg.Views[0].Zones[1].File {
		t.Error("both duplicate entries should be retained with their own files")
	}

	warns := obs.FilterMessageSnippet("duplicate top-level zone").All()
	if len(warns) != 1 {
		t.Fatalf("expected exactly 1 duplicate warning, got %d: %+v", len(warns), obs.All())
	}
	ctx := warns[0].ContextMap()
	if ctx["zone"] != "example.com" {
		t.Errorf("warning zone field: got %v, want example.com", ctx["zone"])
	}
	decls, _ := ctx["declarations"].(string)
	if !strings.Contains(decls, confPath+":5") || !strings.Contains(decls, confPath+":9") {
		t.Errorf("warning should list both declaration positions (lines 5 and 9), got: %q", decls)
	}
	if !strings.Contains(warns[0].Message, "last declaration takes effect") {
		t.Errorf("warning should state last declaration takes effect at serving time, got: %q", warns[0].Message)
	}
}

// Task 1.3-a (spec scenario "Top-level zone declared before a view fails"): a
// top-level zone followed by a view is a fatal mixing error naming the zone and
// its source:line.
func TestLoadNamedConf_MixZoneBeforeViewFails(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

zone "example.com" {
	type master;
	file "/etc/namedb/master/example.com.fwd";
};

view "view-other" {
	match-clients { any; };
	zone "example.net" {
		type master;
		file "/etc/namedb/master/example.net.fwd";
	};
};
`
	confPath := filepath.Join(dir, "named.conf")
	writeFile(t, confPath, namedConf)

	_, err := LoadNamedConf(confPath, zap.NewNop())
	if err == nil {
		t.Fatal("expected fatal error for mixing top-level zone with a view, got nil")
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error should name the top-level zone example.com, got: %v", err)
	}
	if !strings.Contains(err.Error(), confPath+":5") {
		t.Errorf("error should include source:line (%s:5), got: %v", confPath, err)
	}
}

// Task 1.3-b: a view followed by a top-level zone is a fatal mixing error
// regardless of declaration order (view first here).
func TestLoadNamedConf_MixViewBeforeZoneFails(t *testing.T) {
	dir := t.TempDir()

	namedConf := `options {
	directory "/etc/namedb";
};

view "view-other" {
	match-clients { any; };
	zone "example.net" {
		type master;
		file "/etc/namedb/master/example.net.fwd";
	};
};

zone "example.com" {
	type master;
	file "/etc/namedb/master/example.com.fwd";
};
`
	confPath := filepath.Join(dir, "named.conf")
	writeFile(t, confPath, namedConf)

	_, err := LoadNamedConf(confPath, zap.NewNop())
	if err == nil {
		t.Fatal("expected fatal error for view-then-top-level-zone, got nil")
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error should name the top-level zone example.com, got: %v", err)
	}
	if !strings.Contains(err.Error(), confPath+":13") {
		t.Errorf("error should include source:line (%s:13), got: %v", confPath, err)
	}
}

// Task 1.3-c (spec scenario "Mixing across included files fails regardless of
// order"): a view in an included file plus a top-level zone in the root file is
// fatal even though the zone is parsed after all views.
func TestLoadNamedConf_MixAcrossIncludeFilesFails(t *testing.T) {
	dir := t.TempDir()

	masterZones := `view "view-other" {
	match-clients { any; };
	zone "example.net" {
		type master;
		file "/etc/namedb/master/example.net.fwd";
	};
};
`
	namedConf := `options {
	directory "/etc/namedb";
};

include "master.zones";

zone "example.com" {
	type master;
	file "/etc/namedb/master/example.com.fwd";
};
`
	writeFile(t, filepath.Join(dir, "master.zones"), masterZones)
	confPath := filepath.Join(dir, "named.conf")
	writeFile(t, confPath, namedConf)

	_, err := LoadNamedConf(confPath, zap.NewNop())
	if err == nil {
		t.Fatal("expected fatal error mixing across files, got nil")
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error should name top-level zone example.com, got: %v", err)
	}
	if !strings.Contains(err.Error(), confPath) {
		t.Errorf("error should reference the named.conf source path, got: %v", err)
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
