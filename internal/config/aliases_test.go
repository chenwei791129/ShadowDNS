package config

import (
	"strings"
	"testing"
)

// BuildAliasMap: well-formed backup→root map is normalized and returned.
func TestBuildAliasMap_WellFormed(t *testing.T) {
	m, err := BuildAliasMap(map[string]string{
		"backup.com": "root.com",
		"mirror.com": "root.com",
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if got := m["backup.com."]; got != "root.com." {
		t.Errorf("m[backup.com.] = %q, want %q", got, "root.com.")
	}
	if got := m["mirror.com."]; got != "root.com." {
		t.Errorf("m[mirror.com.] = %q, want %q", got, "root.com.")
	}
	if len(m) != 2 {
		t.Errorf("len = %d, want 2", len(m))
	}
}

// BuildAliasMap: mixed case is normalized to lowercase.
func TestBuildAliasMap_Normalization(t *testing.T) {
	m, err := BuildAliasMap(map[string]string{
		"Backup.COM": "Root.Com",
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if got := m["backup.com."]; got != "root.com." {
		t.Errorf("m[backup.com.] = %q, want %q", got, "root.com.")
	}
}

// BuildAliasMap: backup equal to root is rejected.
func TestBuildAliasMap_SelfAlias(t *testing.T) {
	_, err := BuildAliasMap(map[string]string{
		"loop.com": "loop.com",
	})
	if err == nil {
		t.Fatal("expected error for self-alias")
	}
	if !strings.Contains(err.Error(), "self-alias") {
		t.Errorf("error should mention 'self-alias': %v", err)
	}
}

// BuildAliasMap: same backup (after normalization) mapped to two roots is rejected.
func TestBuildAliasMap_DuplicateBackup(t *testing.T) {
	_, err := BuildAliasMap(map[string]string{
		"Backup.com": "root1.com",
		"backup.com": "root2.com",
	})
	if err == nil {
		t.Fatal("expected error for duplicate backup across different roots")
	}
	if !strings.Contains(err.Error(), "backup.com") {
		t.Errorf("error should name the duplicate backup: %v", err)
	}
}

// BuildAliasMap: empty keys are rejected.
func TestBuildAliasMap_EmptyKey(t *testing.T) {
	_, err := BuildAliasMap(map[string]string{"": "root.com"})
	if err == nil {
		t.Fatal("expected error for empty backup domain")
	}
}

func TestBuildAliasMap_EmptyValue(t *testing.T) {
	_, err := BuildAliasMap(map[string]string{"backup.com": ""})
	if err == nil {
		t.Fatal("expected error for empty root domain")
	}
}

// BuildAliasMap: empty map yields empty AliasMap, no error.
func TestBuildAliasMap_EmptyMap(t *testing.T) {
	m, err := BuildAliasMap(nil)
	if err != nil {
		t.Fatalf("BuildAliasMap(nil): %v", err)
	}
	if len(m) != 0 {
		t.Errorf("len = %d, want 0", len(m))
	}
}
