package config

import (
	"strings"
	"testing"
)

// BuildAliasMap: well-formed root → AliasGroup map is normalized into both
// the backup→root map and the backup→flag map.
func TestBuildAliasMap_WellFormed(t *testing.T) {
	m, flags, err := BuildAliasMap(map[string]AliasGroup{
		"root.com": {
			Members:            []string{"backup.com", "mirror.com"},
			RewriteRDATALabels: false,
		},
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
		t.Errorf("len(map) = %d, want 2", len(m))
	}
	if flags["backup.com."] != false {
		t.Errorf("flags[backup.com.] = %v, want false", flags["backup.com."])
	}
	if flags["mirror.com."] != false {
		t.Errorf("flags[mirror.com.] = %v, want false", flags["mirror.com."])
	}
}

// BuildAliasMap: RewriteRDATALabels=true is propagated to every member.
func TestBuildAliasMap_RewriteFlagPropagated(t *testing.T) {
	_, flags, err := BuildAliasMap(map[string]AliasGroup{
		"root.com": {
			Members:            []string{"backup.com", "mirror.com"},
			RewriteRDATALabels: true,
		},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if !flags["backup.com."] {
		t.Errorf("flags[backup.com.] = false, want true")
	}
	if !flags["mirror.com."] {
		t.Errorf("flags[mirror.com.] = false, want true")
	}
}

// BuildAliasMap: groups with different flag values can coexist.
func TestBuildAliasMap_MixedFlags(t *testing.T) {
	_, flags, err := BuildAliasMap(map[string]AliasGroup{
		"root-a.net": {
			Members:            []string{"alias-a.net"},
			RewriteRDATALabels: false,
		},
		"root-b.net": {
			Members:            []string{"alias-b.net"},
			RewriteRDATALabels: true,
		},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if flags["alias-a.net."] {
		t.Errorf("flags[alias-a.net.] = true, want false")
	}
	if !flags["alias-b.net."] {
		t.Errorf("flags[alias-b.net.] = false, want true")
	}
}

// BuildAliasMap: mixed case is normalized to lowercase.
func TestBuildAliasMap_Normalization(t *testing.T) {
	m, _, err := BuildAliasMap(map[string]AliasGroup{
		"Root.Com": {
			Members: []string{"Backup.COM"},
		},
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
	_, _, err := BuildAliasMap(map[string]AliasGroup{
		"loop.com": {Members: []string{"loop.com"}},
	})
	if err == nil {
		t.Fatal("expected error for self-alias")
	}
	if !strings.Contains(err.Error(), "self-alias") {
		t.Errorf("error should mention 'self-alias': %v", err)
	}
}

// BuildAliasMap: same backup (after normalization) under two roots is rejected.
func TestBuildAliasMap_DuplicateBackup(t *testing.T) {
	_, _, err := BuildAliasMap(map[string]AliasGroup{
		"root1.com": {Members: []string{"Backup.com"}},
		"root2.com": {Members: []string{"backup.com"}},
	})
	if err == nil {
		t.Fatal("expected error for duplicate backup across different roots")
	}
	if !strings.Contains(err.Error(), "backup.com") {
		t.Errorf("error should name the duplicate backup: %v", err)
	}
}

// BuildAliasMap: empty backup label is rejected.
func TestBuildAliasMap_EmptyMember(t *testing.T) {
	_, _, err := BuildAliasMap(map[string]AliasGroup{
		"root.com": {Members: []string{""}},
	})
	if err == nil {
		t.Fatal("expected error for empty backup domain")
	}
}

// BuildAliasMap: empty root key is rejected.
func TestBuildAliasMap_EmptyRoot(t *testing.T) {
	_, _, err := BuildAliasMap(map[string]AliasGroup{
		"": {Members: []string{"backup.com"}},
	})
	if err == nil {
		t.Fatal("expected error for empty root domain")
	}
}

// BuildAliasMap: empty input yields empty maps with no error.
func TestBuildAliasMap_EmptyMap(t *testing.T) {
	m, flags, err := BuildAliasMap(nil)
	if err != nil {
		t.Fatalf("BuildAliasMap(nil): %v", err)
	}
	if len(m) != 0 {
		t.Errorf("len(map) = %d, want 0", len(m))
	}
	if len(flags) != 0 {
		t.Errorf("len(flags) = %d, want 0", len(flags))
	}
}

// BuildAliasMap: a root whose Members list is empty yields empty maps and no error.
func TestBuildAliasMap_EmptyMembers(t *testing.T) {
	m, flags, err := BuildAliasMap(map[string]AliasGroup{
		"root.com": {Members: nil, RewriteRDATALabels: true},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("len(map) = %d, want 0", len(m))
	}
	if len(flags) != 0 {
		t.Errorf("len(flags) = %d, want 0", len(flags))
	}
}
