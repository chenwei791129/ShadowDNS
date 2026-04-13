package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test 1: Well-formed YAML produces correct reverse alias map.
func TestLoadAliases_WellFormed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	content := []byte("root.com:\n  - backup.com\n  - mirror.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	m, err := LoadAliases(path, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// backup.com → root.com (both normalized)
	if got := m["backup.com."]; got != "root.com." {
		t.Errorf("m[backup.com.] = %q, want %q", got, "root.com.")
	}
	if got := m["mirror.com."]; got != "root.com." {
		t.Errorf("m[mirror.com.] = %q, want %q", got, "root.com.")
	}
	if len(m) != 2 {
		t.Errorf("map length = %d, want 2", len(m))
	}
}

// Test 2: Same backup appearing under two different roots is rejected.
func TestLoadAliases_DuplicateBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	content := []byte("root1.com:\n  - shared.com\nroot2.com:\n  - shared.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	_, err := LoadAliases(path, logger)
	if err == nil {
		t.Fatal("expected error for duplicate backup, got nil")
	}
	// Error must mention both root names.
	if !strings.Contains(err.Error(), "root1.com") || !strings.Contains(err.Error(), "root2.com") {
		t.Errorf("error should mention both roots, got: %v", err)
	}
}

// Test 3: Backup domain equal to its root is rejected (self-alias).
func TestLoadAliases_SelfAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	content := []byte("loop.com:\n  - loop.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	_, err := LoadAliases(path, logger)
	if err == nil {
		t.Fatal("expected error for self-alias, got nil")
	}
	if !strings.Contains(err.Error(), "loop.com") {
		t.Errorf("error should mention the offending domain, got: %v", err)
	}
	if !strings.Contains(err.Error(), "self-alias") {
		t.Errorf("error should mention 'self-alias', got: %v", err)
	}
}

// Test 4: Empty path returns empty map and logs info, no error.
func TestLoadAliases_EmptyPath(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	m, err := LoadAliases("", logger)
	if err != nil {
		t.Fatalf("unexpected error for empty path: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
	// Expect an info log.
	if !strings.Contains(buf.String(), "INFO") {
		t.Errorf("expected info log, got: %s", buf.String())
	}
}

// Test 5: Non-existent file returns empty map and logs info, no error.
func TestLoadAliases_NonExistentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-file.yaml")

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	m, err := LoadAliases(path, logger)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
	// Expect an info log.
	if !strings.Contains(buf.String(), "INFO") {
		t.Errorf("expected info log, got: %s", buf.String())
	}
}

// Test 6: Malformed YAML returns an error.
func TestLoadAliases_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	// Unclosed bracket — invalid YAML.
	content := []byte("root.com: [backup.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	_, err := LoadAliases(path, logger)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

// Test 7: Domain name normalization — mixed case and missing trailing dot.
func TestLoadAliases_Normalization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	content := []byte("Root.COM:\n  - Backup.Com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	m, err := LoadAliases(path, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := m["backup.com."]; got != "root.com." {
		t.Errorf("m[backup.com.] = %q, want %q", got, "root.com.")
	}
}

// Test 8: Empty domain string (empty root key) is rejected.
func TestLoadAliases_EmptyDomainKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	// An empty string as the root key.
	content := []byte("\"\":\n  - backup.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	_, err := LoadAliases(path, logger)
	if err == nil {
		t.Fatal("expected error for empty root domain, got nil")
	}
}

// Test 8b: Empty domain string in backup list is rejected.
func TestLoadAliases_EmptyDomainBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	content := []byte("root.com:\n  - \"\"\n  - backup.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	_, err := LoadAliases(path, logger)
	if err == nil {
		t.Fatal("expected error for empty backup domain, got nil")
	}
}

// Test 9: Domain containing whitespace is rejected.
func TestLoadAliases_WhitespaceDomain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.yaml")
	content := []byte("\"root .com\":\n  - backup.com\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	_, err := LoadAliases(path, logger)
	if err == nil {
		t.Fatal("expected error for whitespace in domain, got nil")
	}
}
