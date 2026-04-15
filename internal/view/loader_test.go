package view

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test-local filename constants for the GeoIP candidate basenames. Kept
// separate from the production candidate slices so test intent stays explicit
// and tests don't depend on the production slice's ordering.
const (
	geoIP2CountryFilename   = "GeoIP2-Country.mmdb"
	geoLite2CountryFilename = "GeoLite2-Country.mmdb"
	geoIP2ASNFilename       = "GeoIP2-ASN.mmdb"
	geoLite2ASNFilename     = "GeoLite2-ASN.mmdb"
)

// fixtureKind selects the content written for a fixture file.
type fixtureKind int

const (
	kindCountry fixtureKind = iota // valid country mmdb bytes
	kindASN                        // valid ASN mmdb bytes
	kindInvalid                    // garbage bytes that fail mmdb validation
)

// fixtureFile names one mmdb file to materialize under the test temp dir.
type fixtureFile struct {
	name string
	kind fixtureKind
}

// buildGeoIPFixture creates a temp dir and materializes the listed mmdb
// candidate files, returning the directory path. Reuses the valid mmdb byte
// streams from buildCountryMMDB / buildASNMMDB in testhelper_test.go.
func buildGeoIPFixture(t *testing.T, files ...fixtureFile) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		switch f.kind {
		case kindCountry:
			writeCountryMMDB(t, path)
		case kindASN:
			writeASNMMDB(t, path)
		case kindInvalid:
			writeInvalidMMDB(t, path)
		default:
			t.Fatalf("buildGeoIPFixture: unknown fixture kind %d for %s", f.kind, f.name)
		}
	}
	return dir
}

// writeInvalidMMDB writes garbage bytes (deliberately not a valid mmdb) to path.
func writeInvalidMMDB(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("this is not a valid mmdb file"), 0o600); err != nil {
		t.Fatalf("write invalid mmdb at %s: %v", path, err)
	}
}

// recordedLog is one log event captured by recordingLogHandler.
type recordedLog struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

// recordingLogHandler is an slog.Handler that captures records for assertion.
type recordingLogHandler struct {
	entries []recordedLog
}

func (h *recordingLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *recordingLogHandler) Handle(_ context.Context, r slog.Record) error {
	e := recordedLog{level: r.Level, msg: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		e.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.entries = append(h.entries, e)
	return nil
}

func (h *recordingLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingLogHandler) WithGroup(_ string) slog.Handler      { return h }

// buildBothMMDBs creates a temp dir with both valid GeoLite2 mmdb files and
// returns the dir path. Used by older tests that pre-date the GeoIP2/GeoLite2
// fallback chain.
func buildBothMMDBs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeCountryMMDB(t, filepath.Join(dir, geoLite2CountryFilename))
	writeASNMMDB(t, filepath.Join(dir, geoLite2ASNFilename))

	return dir
}

// writeCountryMMDB writes a tiny country mmdb to path.
func writeCountryMMDB(t *testing.T, path string) {
	t.Helper()
	// Reuse the same logic as buildCountryMMDB but write to a specific path.
	import_path := buildCountryMMDB(t)
	data, err := os.ReadFile(import_path)
	if err != nil {
		t.Fatalf("read country mmdb: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write country mmdb: %v", err)
	}
}

// writeASNMMDB writes a tiny ASN mmdb to path.
func writeASNMMDB(t *testing.T, path string) {
	t.Helper()
	import_path := buildASNMMDB(t)
	data, err := os.ReadFile(import_path)
	if err != nil {
		t.Fatalf("read ASN mmdb: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write ASN mmdb: %v", err)
	}
}

func TestLoadGeoIP_EmptyDir(t *testing.T) {
	dir := t.TempDir() // empty directory
	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
	if !strings.Contains(err.Error(), geoLite2CountryFilename) {
		t.Errorf("error should mention %q, got: %v", geoLite2CountryFilename, err)
	}
}

func TestLoadGeoIP_OnlyCountryDB(t *testing.T) {
	dir := t.TempDir()
	// Write only the country mmdb.
	writeCountryMMDB(t, filepath.Join(dir, geoLite2CountryFilename))

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error when ASN db is missing, got nil")
	}
	if !strings.Contains(err.Error(), geoLite2ASNFilename) {
		t.Errorf("error should mention %q, got: %v", geoLite2ASNFilename, err)
	}
}

func TestLoadGeoIP_OnlyASNDB(t *testing.T) {
	dir := t.TempDir()
	// Write only the ASN mmdb (country is missing).
	writeASNMMDB(t, filepath.Join(dir, geoLite2ASNFilename))

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error when country db is missing, got nil")
	}
	if !strings.Contains(err.Error(), geoLite2CountryFilename) {
		t.Errorf("error should mention %q, got: %v", geoLite2CountryFilename, err)
	}
}

func TestLoadGeoIP_BothValid(t *testing.T) {
	dir := buildBothMMDBs(t)

	countryDB, asnDB, err := LoadGeoIP(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countryDB == nil {
		t.Error("expected non-nil countryDB")
	}
	if asnDB == nil {
		t.Error("expected non-nil asnDB")
	}
	defer func() { _ = countryDB.Close() }()
	defer func() { _ = asnDB.Close() }()
}

func TestLoadGeoIP_CorruptCountryFile(t *testing.T) {
	dir := t.TempDir()
	// Write garbage bytes to the country mmdb file.
	writeInvalidMMDB(t, filepath.Join(dir, geoLite2CountryFilename))
	// Write a valid ASN mmdb.
	writeASNMMDB(t, filepath.Join(dir, geoLite2ASNFilename))

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error for corrupt country file, got nil")
	}
	if !strings.Contains(err.Error(), geoLite2CountryFilename) {
		t.Errorf("error should mention %q, got: %v", geoLite2CountryFilename, err)
	}
}

// 2.1 Both GeoIP2 (paid) candidates present → loader picks them.
func TestLoadGeoIP_GeoIP2OnlySucceeds(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoIP2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoIP2ASNFilename, kind: kindASN},
	)

	countryDB, asnDB, err := LoadGeoIP(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countryDB == nil || asnDB == nil {
		t.Fatal("expected non-nil DB handles")
	}
	t.Cleanup(func() {
		_ = countryDB.Close()
		_ = asnDB.Close()
	})
}

// Spec scenario 1: GeoIP2 wins and the loader does NOT attempt GeoLite2.
// Verified by placing a valid GeoIP2 file alongside an *invalid* GeoLite2
// file: if the loader walked past GeoIP2 to GeoLite2 (or tried both), the
// invalid bytes would surface as an error. Success proves short-circuit.
func TestLoadGeoIP_GeoIP2WinsAndGeoLite2NotAttempted(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoIP2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoLite2CountryFilename, kind: kindInvalid},
		fixtureFile{name: geoIP2ASNFilename, kind: kindASN},
		fixtureFile{name: geoLite2ASNFilename, kind: kindInvalid},
	)

	rec := &recordingLogHandler{}
	countryDB, asnDB, err := LoadGeoIP(dir, slog.New(rec))
	if err != nil {
		t.Fatalf("loader should short-circuit on first valid candidate, got: %v", err)
	}
	t.Cleanup(func() {
		_ = countryDB.Close()
		_ = asnDB.Close()
	})

	wantCountry := filepath.Join(dir, geoIP2CountryFilename)
	wantASN := filepath.Join(dir, geoIP2ASNFilename)
	for _, e := range rec.entries {
		if e.level != slog.LevelInfo {
			continue
		}
		switch e.msg {
		case "loaded GeoIP country database":
			if got := e.attrs["path"]; got != wantCountry {
				t.Errorf("country path = %v, want %q (loader must not open GeoLite2 when GeoIP2 succeeds)", got, wantCountry)
			}
		case "loaded GeoIP ASN database":
			if got := e.attrs["path"]; got != wantASN {
				t.Errorf("ASN path = %v, want %q (loader must not open GeoLite2 when GeoIP2 succeeds)", got, wantASN)
			}
		}
	}
}

// 2.2 Existing GeoLite2-only deployment still works (regression guard).
func TestLoadGeoIP_GeoLite2OnlySucceeds(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoLite2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoLite2ASNFilename, kind: kindASN},
	)

	countryDB, asnDB, err := LoadGeoIP(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countryDB == nil || asnDB == nil {
		t.Fatal("expected non-nil DB handles")
	}
	t.Cleanup(func() {
		_ = countryDB.Close()
		_ = asnDB.Close()
	})
}

// 2.3 Mixed editions: GeoIP2-Country + GeoLite2-ASN — Country and ASN are
// resolved independently.
func TestLoadGeoIP_MixedEditionsSucceeds(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoIP2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoLite2ASNFilename, kind: kindASN},
	)

	countryDB, asnDB, err := LoadGeoIP(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countryDB == nil || asnDB == nil {
		t.Fatal("expected non-nil DB handles")
	}
	t.Cleanup(func() {
		_ = countryDB.Close()
		_ = asnDB.Close()
	})
}

// 2.4 Higher-priority GeoIP2-Country exists but is invalid; loader falls
// through to the valid GeoLite2-Country.
func TestLoadGeoIP_FallbackWhenGeoIP2CountryInvalid(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoIP2CountryFilename, kind: kindInvalid},
		fixtureFile{name: geoLite2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoLite2ASNFilename, kind: kindASN},
	)

	countryDB, asnDB, err := LoadGeoIP(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countryDB == nil || asnDB == nil {
		t.Fatal("expected non-nil DB handles")
	}
	t.Cleanup(func() {
		_ = countryDB.Close()
		_ = asnDB.Close()
	})
}

// 2.5 Every Country candidate missing → error names both attempted paths.
func TestLoadGeoIP_AllCountryCandidatesMissing(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoLite2ASNFilename, kind: kindASN},
	)

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error when all country candidates are missing")
	}

	wantPaths := []string{
		filepath.Join(dir, geoIP2CountryFilename),
		filepath.Join(dir, geoLite2CountryFilename),
	}
	for _, w := range wantPaths {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error should mention %q, got: %v", w, err)
		}
	}
}

// 2.6 Both ASN candidates exist but are invalid → error names both paths and
// includes the per-attempt validation reason for each.
func TestLoadGeoIP_AllASNCandidatesInvalid(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoLite2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoIP2ASNFilename, kind: kindInvalid},
		fixtureFile{name: geoLite2ASNFilename, kind: kindInvalid},
	)

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error when all ASN candidates are invalid")
	}

	wantPaths := []string{
		filepath.Join(dir, geoIP2ASNFilename),
		filepath.Join(dir, geoLite2ASNFilename),
	}
	for _, w := range wantPaths {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error should mention %q, got: %v", w, err)
		}
	}
	// Each attempt must carry its own underlying mmdb validation reason —
	// match against the maxminddb library's stable error wording rather
	// than this package's own format string.
	const mmdbErr = "invalid MaxMind DB file"
	if got := strings.Count(err.Error(), mmdbErr); got < 2 {
		t.Errorf("expected per-attempt validation reason for both ASN paths (>=2 occurrences of %q), got %d in: %v", mmdbErr, got, err)
	}
}

// 2.7 Successful load emits an info log whose `path` attr is the actual
// opened file (verifies path field reflects which edition was picked).
func TestLoadGeoIP_LogsActualOpenedPath(t *testing.T) {
	dir := buildGeoIPFixture(t,
		fixtureFile{name: geoIP2CountryFilename, kind: kindCountry},
		fixtureFile{name: geoLite2ASNFilename, kind: kindASN},
	)

	rec := &recordingLogHandler{}
	logger := slog.New(rec)

	countryDB, asnDB, err := LoadGeoIP(dir, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_ = countryDB.Close()
		_ = asnDB.Close()
	})

	wantCountryPath := filepath.Join(dir, geoIP2CountryFilename)
	wantASNPath := filepath.Join(dir, geoLite2ASNFilename)

	var sawCountry, sawASN bool
	for _, e := range rec.entries {
		if e.level != slog.LevelInfo {
			continue
		}
		switch e.msg {
		case "loaded GeoIP country database":
			if got := e.attrs["path"]; got != wantCountryPath {
				t.Errorf("country log path = %v, want %q", got, wantCountryPath)
			}
			sawCountry = true
		case "loaded GeoIP ASN database":
			if got := e.attrs["path"]; got != wantASNPath {
				t.Errorf("ASN log path = %v, want %q", got, wantASNPath)
			}
			sawASN = true
		}
	}
	if !sawCountry {
		t.Error("missing info log for country database")
	}
	if !sawASN {
		t.Error("missing info log for ASN database")
	}
}

func TestLoadGeoIP_CorruptASNFile(t *testing.T) {
	dir := t.TempDir()
	// Write a valid country mmdb.
	writeCountryMMDB(t, filepath.Join(dir, geoLite2CountryFilename))
	// Write garbage bytes to the ASN mmdb file.
	writeInvalidMMDB(t, filepath.Join(dir, geoLite2ASNFilename))

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error for corrupt ASN file, got nil")
	}
	if !strings.Contains(err.Error(), geoLite2ASNFilename) {
		t.Errorf("error should mention %q, got: %v", geoLite2ASNFilename, err)
	}
}
