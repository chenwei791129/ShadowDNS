package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeBuildTestZoneFile writes a minimal valid RFC 1035 zone file to dir and
// returns its path.
func writeBuildTestZoneFile(t *testing.T, dir, filename, origin, serial string) string {
	t.Helper()
	content := "$ORIGIN " + origin + "\n" +
		origin + " 300 IN SOA ns1." + origin + " admin." + origin +
		" " + serial + " 3600 900 604800 300\n" +
		origin + " 300 IN NS ns1." + origin + "\n"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeBuildTestZoneFile %q: %v", path, err)
	}
	return path
}

// singleViewConfig returns a minimal *config.Config with one "default" view
// containing the supplied zones.
func singleViewConfig(zones []config.Zone) *config.Config {
	return &config.Config{
		Views: []config.View{
			{
				Name:         "default",
				MatchClients: []config.Element{{Kind: config.ElemAny}},
				Zones:        zones,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Task 4.1 — Diff-based zone pointer reuse preserves immutability
// ---------------------------------------------------------------------------

// TestBuildState_PointerReuseOnUnchangedZone verifies the core diff-based reload
// invariant: when a zone file is identical between two BuildState calls, the
// returned *zone.Zone pointer in the new state is identical (pointer-equal) to
// the one in the previous state, and the Records map is not mutated.
//
// This test is the RED phase for task 4.1 and fails until BuildState accepts
// prev *ServerState and mode VerifyMode parameters (task 4.4).
func TestBuildState_PointerReuseOnUnchangedZone(t *testing.T) {
	dir := t.TempDir()

	zoneFile := writeBuildTestZoneFile(t, dir, "example.com.zone", "example.com.", "1")

	cfg := singleViewConfig([]config.Zone{
		{Name: "example.com", Type: "master", File: zoneFile},
	})
	aliases := config.AliasMap{}

	// First build: prev == nil → full parse, fingerprints recorded.
	state1, _, err := BuildState(cfg, aliases, nil, nil, nil, nil, VerifyModeHash, nil, nil, nil)
	if err != nil {
		t.Fatalf("first BuildState: %v", err)
	}

	zone1 := state1.RootZones["default"]["example.com."]
	if zone1 == nil {
		t.Fatal("example.com. not found in state1.RootZones[\"default\"]")
	}
	recordsBefore := len(zone1.Records)

	// Second build: prev = &state1, zone file unchanged → pointer must be reused.
	state2, _, err := BuildState(cfg, aliases, nil, nil, nil, &state1, VerifyModeHash, nil, nil, nil)
	if err != nil {
		t.Fatalf("second BuildState: %v", err)
	}

	zone2 := state2.RootZones["default"]["example.com."]
	if zone2 == nil {
		t.Fatal("example.com. not found in state2.RootZones[\"default\"]")
	}

	// Pointer equality: unchanged zone must be the exact same *zone.Zone object.
	if zone1 != zone2 {
		t.Error("expected *zone.Zone to be pointer-equal for unchanged zone file; got distinct pointers (zone was re-parsed)")
	}

	// Immutability: the reused zone's Records map must not have grown or shrunk.
	if len(zone2.Records) != recordsBefore {
		t.Errorf("Records map was mutated: had %d keys before, %d after second BuildState", recordsBefore, len(zone2.Records))
	}
}

// ---------------------------------------------------------------------------
// Task 4.2 — First-reload / startup fallback
// ---------------------------------------------------------------------------

// TestBuildState_StartupFallback verifies that when prev == nil, every zone is
// parsed from disk and the returned ServerState carries a non-nil fingerprint
// for every zone so subsequent reloads can compare against them.
//
// This test is the RED phase for task 4.2 and fails until ServerState gains
// a Fingerprints field and BuildState records fingerprints on the first build.
func TestBuildState_StartupFallback(t *testing.T) {
	dir := t.TempDir()

	file1 := writeBuildTestZoneFile(t, dir, "alpha.com.zone", "alpha.com.", "1")
	file2 := writeBuildTestZoneFile(t, dir, "beta.com.zone", "beta.com.", "1")

	cfg := singleViewConfig([]config.Zone{
		{Name: "alpha.com", Type: "master", File: file1},
		{Name: "beta.com", Type: "master", File: file2},
	})

	state, _, err := BuildState(cfg, config.AliasMap{}, nil, nil, nil, nil, VerifyModeHash, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}

	// Both zones must be parsed and present.
	if state.RootZones["default"]["alpha.com."] == nil {
		t.Error("alpha.com. not found in state.RootZones after startup build")
	}
	if state.RootZones["default"]["beta.com."] == nil {
		t.Error("beta.com. not found in state.RootZones after startup build")
	}

	// The returned state must carry a fingerprint for each zone so the next
	// SIGHUP reload can detect changes.
	if state.Fingerprints == nil {
		t.Fatal("state.Fingerprints is nil; startup build must record fingerprints for all zones")
	}
	if state.Fingerprints["default"] == nil {
		t.Fatal("state.Fingerprints[\"default\"] is nil")
	}
	if _, ok := state.Fingerprints["default"]["alpha.com."]; !ok {
		t.Error("no fingerprint recorded for alpha.com. in startup state")
	}
	if _, ok := state.Fingerprints["default"]["beta.com."]; !ok {
		t.Error("no fingerprint recorded for beta.com. in startup state")
	}
}

// ---------------------------------------------------------------------------
// CNAME chain collapsing — collapse lookup wiring
// ---------------------------------------------------------------------------

// TestBuildState_CollapseFlagsStored verifies that BuildState writes the
// root-keyed collapse lookup into the returned state, so handlers can consult
// it via match.RootZone and SIGHUP reloads swap it atomically with the rest
// of the state snapshot.
func TestBuildState_CollapseFlagsStored(t *testing.T) {
	dir := t.TempDir()
	zoneFile := writeBuildTestZoneFile(t, dir, "example.com.zone", "example.com.", "1")
	cfg := singleViewConfig([]config.Zone{
		{Name: "example.com", Type: "master", File: zoneFile},
	})
	collapse := config.CollapseFlags{"example.com.": true}

	state, _, err := BuildState(cfg, config.AliasMap{}, nil, collapse, nil, nil, VerifyModeHash, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if !state.CollapseFlags["example.com."] {
		t.Errorf("state.CollapseFlags[example.com.] = false, want true")
	}
}

// ---------------------------------------------------------------------------
// Task 4.3 — Rollback semantics on partial failure
// ---------------------------------------------------------------------------

// TestBuildState_RollbackOnPartialFailure verifies that when one zone fails to
// parse during a reload build, BuildState returns a non-nil error and the
// caller's previous state remains completely intact — no zone pointers are
// changed, no maps are mutated.
//
// This test is the RED phase for task 4.3. It will also validate task 4.4's
// implementation once BuildState accepts prev *ServerState.
func TestBuildState_RollbackOnPartialFailure(t *testing.T) {
	dir := t.TempDir()

	goodFile := writeBuildTestZoneFile(t, dir, "good.com.zone", "good.com.", "1")
	badFile := filepath.Join(dir, "bad.com.zone")
	if err := os.WriteFile(badFile, []byte(
		"$ORIGIN bad.com.\n"+
			"bad.com. 300 IN SOA ns1.bad.com. admin.bad.com. 1 3600 900 604800 300\n"+
			"bad.com. 300 IN NS ns1.bad.com.\n",
	), 0o644); err != nil {
		t.Fatalf("WriteFile bad.com zone: %v", err)
	}

	cfg := singleViewConfig([]config.Zone{
		{Name: "good.com", Type: "master", File: goodFile},
		{Name: "bad.com", Type: "master", File: badFile},
	})

	// First build succeeds: establishes a known good prev state.
	prevState, _, err := BuildState(cfg, config.AliasMap{}, nil, nil, nil, nil, VerifyModeHash, nil, nil, nil)
	if err != nil {
		t.Fatalf("initial BuildState: %v", err)
	}

	prevGoodZone := prevState.RootZones["default"]["good.com."]
	prevBadZone := prevState.RootZones["default"]["bad.com."]
	if prevGoodZone == nil || prevBadZone == nil {
		t.Fatal("zones missing from initial state; test setup error")
	}

	// Corrupt bad.com.zone so the second build fails mid-run.
	if err := os.WriteFile(badFile, []byte("INVALID ZONE CONTENT %%%"), 0o644); err != nil {
		t.Fatalf("corrupt zone file: %v", err)
	}

	// Second build must fail because bad.com.zone is now unparseable.
	_, _, err = BuildState(cfg, config.AliasMap{}, nil, nil, nil, &prevState, VerifyModeHash, nil, nil, nil)
	if err == nil {
		t.Fatal("expected BuildState to return an error when a zone file is unparseable; got nil")
	}

	// Previous state must be completely intact after the failed rebuild.
	// Neither the zone pointers nor the map structure may have been mutated.
	if prevState.RootZones["default"]["good.com."] != prevGoodZone {
		t.Error("good.com. zone pointer was modified in previous state by a failed rebuild (rollback violated)")
	}
	if prevState.RootZones["default"]["bad.com."] != prevBadZone {
		t.Error("bad.com. zone pointer was modified in previous state by a failed rebuild (rollback violated)")
	}
}
