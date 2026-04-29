// Integration tests for diff-based reload behaviour (change: diff-based-reload).
//
// These tests start a real server, trigger reloads via server.BuildState +
// server.Server.SwapState (the same path that reload() in cmd/shadowdns takes),
// and verify pointer-identity, change-detection accuracy, and full-rebuild
// semantics for --reload-verify=none.
package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// collectZonePointers returns a flat map of "viewName/origin" → *zone.Zone for
// all root and backup zones in the given state.
func collectZonePointers(state *server.ServerState) map[string]any {
	ptrs := make(map[string]any)
	for viewName, zones := range state.RootZones {
		for origin, z := range zones {
			ptrs[viewName+"/root/"+origin] = z
		}
	}
	for viewName, zones := range state.BackupZones {
		for origin, z := range zones {
			ptrs[viewName+"/backup/"+origin] = z
		}
	}
	return ptrs
}

// ---------------------------------------------------------------------------
// Task 7.1 — pointer identity after no-change reload
// ---------------------------------------------------------------------------

// TestReloadDiff_NoChange_PointersIdentical starts a server with two zones,
// triggers a reload with no zone file modifications, and asserts that every
// *zone.Zone pointer in the new state is pointer-identical to the corresponding
// pointer in the old state.  This exercises the core diff-based reload
// invariant: unchanged zones are not re-parsed.
func TestReloadDiff_NoChange_PointersIdentical(t *testing.T) {
	tmpDir := t.TempDir()
	copyFixtures(t, tmpDir)

	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)
	patchNamedConf(t, tmpDir)

	namedConf := filepath.Join(tmpDir, "named.conf")
	logger := zap.NewNop()

	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("LoadNamedConf: %v", err)
	}
	sdCfg, err := shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}
	aliases := sdCfg.Aliases
	country, asn, err := view.LoadGeoIP(geoIPDir, logger)
	if err != nil {
		t.Fatalf("LoadGeoIP: %v", err)
	}
	defer func() { _ = country.Close(); _ = asn.Close() }()

	// Initial build — fingerprints recorded.
	state1, _, err := server.BuildState(cfg, aliases, nil, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		t.Fatalf("initial BuildState: %v", err)
	}
	srv := server.NewServer(state1, logger)

	before := collectZonePointers(srv.CurrentState())

	// Reload with no file changes: every zone must be reused.
	prev := srv.CurrentState()
	state2, summary, err := server.BuildState(cfg, aliases, nil, prev, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		t.Fatalf("reload BuildState: %v", err)
	}
	srv.SwapState(state2)

	after := collectZonePointers(srv.CurrentState())

	// Every pointer must be identical.
	for key, ptrBefore := range before {
		ptrAfter, ok := after[key]
		if !ok {
			t.Errorf("zone %q disappeared after reload", key)
			continue
		}
		if ptrBefore != ptrAfter {
			t.Errorf("zone %q pointer changed after no-op reload (zone was re-parsed)", key)
		}
	}

	// The summary must show all zones reused and none re-parsed.
	if summary.Reparsed != 0 {
		t.Errorf("expected reparsed=0 on no-change reload, got %d", summary.Reparsed)
	}
	if summary.Reused == 0 {
		t.Errorf("expected reused>0 on no-change reload, got 0")
	}
}

// ---------------------------------------------------------------------------
// Task 7.2 — rsync --inplace scenario: hash detects, size misses
// ---------------------------------------------------------------------------

// TestReloadDiff_SameSizeContentChange_HashDetects simulates the rsync
// "-avc --inplace" scenario where a zone file is rewritten with identical size
// and (potentially) preserved mtime but different content.
//
// Under --reload-verify=hash the change must be detected and the zone re-parsed.
// Under --reload-verify=size the change is invisible (negative-control assertion).
func TestReloadDiff_SameSizeContentChange_HashDetects(t *testing.T) {
	tmpDir := t.TempDir()
	copyFixtures(t, tmpDir)

	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)
	patchNamedConf(t, tmpDir)

	namedConf := filepath.Join(tmpDir, "named.conf")
	logger := zap.NewNop()

	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("LoadNamedConf: %v", err)
	}
	sdCfg, err := shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}
	aliases := sdCfg.Aliases
	country, asn, err := view.LoadGeoIP(geoIPDir, logger)
	if err != nil {
		t.Fatalf("LoadGeoIP: %v", err)
	}
	defer func() { _ = country.Close(); _ = asn.Close() }()

	// Locate example.com zone file under a specific view. The fixture defines
	// example.com in multiple views ("view-th" and "view-other"), so we must
	// pin the test to one view to get deterministic pointer lookups below.
	const targetView = "view-th"
	var zoneFilePath string
	for _, v := range cfg.Views {
		if v.Name != targetView {
			continue
		}
		for _, z := range v.Zones {
			if z.Name == "example.com" {
				zoneFilePath = z.File
				break
			}
		}
		break
	}
	if zoneFilePath == "" {
		t.Fatalf("could not find example.com zone file for view %q in loaded config", targetView)
	}

	// --- Build state1 under VerifyModeHash ---
	state1Hash, _, err := server.BuildState(cfg, aliases, nil, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		t.Fatalf("initial BuildState (hash): %v", err)
	}

	// --- Build state1 under VerifyModeSize (separate server for size-mode test) ---
	state1Size, _, err := server.BuildState(cfg, aliases, nil, nil, server.VerifyModeSize, country, asn, logger)
	if err != nil {
		t.Fatalf("initial BuildState (size): %v", err)
	}

	// Record original zone pointer for example.com under the target view.
	const exampleOrigin = "example.com."
	origPtrHash := findZonePointer(t, &state1Hash, targetView, exampleOrigin)
	origPtrSize := findZonePointer(t, &state1Size, targetView, exampleOrigin)

	// Rewrite the zone file with content of the exact same byte length but
	// different content (changed serial: "2024010101" → "2024010102", same width).
	// Preserve original mtime after writing to simulate rsync --inplace -t, which
	// keeps mtime unchanged when rewriting in-place.
	origStat, err := os.Stat(zoneFilePath)
	if err != nil {
		t.Fatalf("stat zone file: %v", err)
	}
	origMtime := origStat.ModTime()

	origContent, err := os.ReadFile(zoneFilePath)
	if err != nil {
		t.Fatalf("read zone file: %v", err)
	}
	// Replace a specific serial string that is known-length in the fixture.
	// The fixture uses serial 2024010101 (10 digits) — replace with 2024010102.
	newContent := strings.Replace(string(origContent), "2024010101", "2024010102", 1)
	if newContent == string(origContent) {
		t.Skip("fixture serial format unexpected; skipping rsync scenario test")
	}
	if len(newContent) != len(origContent) {
		t.Fatalf("test invariant violated: rewritten zone file has different length (%d vs %d); fix the replacement strings", len(newContent), len(origContent))
	}
	if err := os.WriteFile(zoneFilePath, []byte(newContent), 0o644); err != nil {
		t.Fatalf("write rewritten zone: %v", err)
	}
	// Restore mtime so VerifyModeSize (which checks mtime+size) cannot detect
	// the change — only VerifyModeHash (which reads content) can.
	if err := os.Chtimes(zoneFilePath, origMtime, origMtime); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}

	// --- Hash mode: must detect the change ---
	state2Hash, summaryHash, err := server.BuildState(cfg, aliases, nil, &state1Hash, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		t.Fatalf("reload BuildState (hash): %v", err)
	}
	newPtrHash := findZonePointer(t, &state2Hash, targetView, exampleOrigin)
	if newPtrHash == origPtrHash {
		t.Error("hash mode: expected example.com to be re-parsed (new pointer) after content change, got same pointer")
	}
	if summaryHash.Reparsed == 0 {
		t.Error("hash mode: expected reparsed>0 after content change, got 0")
	}

	// --- Size mode: must MISS the change (negative control) ---
	state2Size, summarySize, err := server.BuildState(cfg, aliases, nil, &state1Size, server.VerifyModeSize, country, asn, logger)
	if err != nil {
		t.Fatalf("reload BuildState (size): %v", err)
	}
	newPtrSize := findZonePointer(t, &state2Size, targetView, exampleOrigin)
	if newPtrSize != origPtrSize {
		t.Error("size mode: expected example.com pointer to be REUSED (size mode cannot detect same-size change), got new pointer")
	}
	if summarySize.Reused == 0 {
		t.Error("size mode: expected reused>0 (change should be invisible), got 0")
	}
}

// ---------------------------------------------------------------------------
// Task 7.3 — --reload-verify=none forces full rebuild
// ---------------------------------------------------------------------------

// TestReloadDiff_NoneMode_AlwaysReparses verifies that with VerifyModeNone
// every zone is re-parsed on every reload, even when zone files are unchanged.
// This covers the escape-hatch path.
func TestReloadDiff_NoneMode_AlwaysReparses(t *testing.T) {
	tmpDir := t.TempDir()
	copyFixtures(t, tmpDir)

	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)
	patchNamedConf(t, tmpDir)

	namedConf := filepath.Join(tmpDir, "named.conf")
	logger := zap.NewNop()

	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("LoadNamedConf: %v", err)
	}
	sdCfg, err := shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}
	aliases := sdCfg.Aliases
	country, asn, err := view.LoadGeoIP(geoIPDir, logger)
	if err != nil {
		t.Fatalf("LoadGeoIP: %v", err)
	}
	defer func() { _ = country.Close(); _ = asn.Close() }()

	// Initial build with none mode.
	state1, _, err := server.BuildState(cfg, aliases, nil, nil, server.VerifyModeNone, country, asn, logger)
	if err != nil {
		t.Fatalf("initial BuildState (none): %v", err)
	}
	before := collectZonePointers(&state1)

	// Reload with no file changes but VerifyModeNone.
	state2, summary, err := server.BuildState(cfg, aliases, nil, &state1, server.VerifyModeNone, country, asn, logger)
	if err != nil {
		t.Fatalf("reload BuildState (none): %v", err)
	}
	after := collectZonePointers(&state2)

	// Every pointer must be NEW (full rebuild).
	for key, ptrBefore := range before {
		ptrAfter, ok := after[key]
		if !ok {
			t.Errorf("zone %q disappeared after reload", key)
			continue
		}
		if ptrBefore == ptrAfter {
			t.Errorf("zone %q pointer unchanged in none mode (expected full re-parse)", key)
		}
	}

	// Summary: reused must be 0, reparsed must equal total zone count.
	if summary.Reused != 0 {
		t.Errorf("none mode: expected reused=0, got %d", summary.Reused)
	}
	if summary.Reparsed == 0 {
		t.Errorf("none mode: expected reparsed>0, got 0")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findZonePointer returns the *zone.Zone pointer for the given view and
// origin. Fails the test if not found. Callers must pass a specific view
// because the fixture defines example.com in multiple views, making a
// map-iteration lookup non-deterministic.
func findZonePointer(t *testing.T, state *server.ServerState, viewName, origin string) any {
	t.Helper()
	if zones, ok := state.RootZones[viewName]; ok {
		if z, ok := zones[origin]; ok {
			return z
		}
	}
	t.Fatalf("%s/%s not found in state.RootZones", viewName, origin)
	return nil
}
