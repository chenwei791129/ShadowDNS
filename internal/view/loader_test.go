package view

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// buildBothMMDBs creates a temp dir with both valid mmdb files and returns the dir path.
func buildBothMMDBs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Build country mmdb into dir.
	countryPath := dir + "/" + countryMMDBFilename
	writeCountryMMDB(t, countryPath)

	// Build ASN mmdb into dir.
	asnPath := dir + "/" + asnMMDBFilename
	writeASNMMDB(t, asnPath)

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
	if !strings.Contains(err.Error(), countryMMDBFilename) {
		t.Errorf("error should mention %q, got: %v", countryMMDBFilename, err)
	}
}

func TestLoadGeoIP_OnlyCountryDB(t *testing.T) {
	dir := t.TempDir()
	// Write only the country mmdb.
	writeCountryMMDB(t, dir+"/"+countryMMDBFilename)

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error when ASN db is missing, got nil")
	}
	if !strings.Contains(err.Error(), asnMMDBFilename) {
		t.Errorf("error should mention %q, got: %v", asnMMDBFilename, err)
	}
}

func TestLoadGeoIP_OnlyASNDB(t *testing.T) {
	dir := t.TempDir()
	// Write only the ASN mmdb (country is missing).
	writeASNMMDB(t, dir+"/"+asnMMDBFilename)

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error when country db is missing, got nil")
	}
	if !strings.Contains(err.Error(), countryMMDBFilename) {
		t.Errorf("error should mention %q, got: %v", countryMMDBFilename, err)
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
	corruptPath := dir + "/" + countryMMDBFilename
	if err := os.WriteFile(corruptPath, []byte("this is not a valid mmdb file"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	// Write a valid ASN mmdb.
	writeASNMMDB(t, dir+"/"+asnMMDBFilename)

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error for corrupt country file, got nil")
	}
	if !strings.Contains(err.Error(), countryMMDBFilename) {
		t.Errorf("error should mention %q, got: %v", countryMMDBFilename, err)
	}
}

func TestLoadGeoIP_CorruptASNFile(t *testing.T) {
	dir := t.TempDir()
	// Write a valid country mmdb.
	writeCountryMMDB(t, dir+"/"+countryMMDBFilename)
	// Write garbage bytes to the ASN mmdb file.
	corruptPath := dir + "/" + asnMMDBFilename
	if err := os.WriteFile(corruptPath, []byte("this is not a valid mmdb file"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, _, err := LoadGeoIP(dir, slog.Default())
	if err == nil {
		t.Fatal("expected error for corrupt ASN file, got nil")
	}
	if !strings.Contains(err.Error(), asnMMDBFilename) {
		t.Errorf("error should mention %q, got: %v", asnMMDBFilename, err)
	}
}
