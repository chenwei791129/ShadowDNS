package config

import (
	"strings"
	"testing"
)

// BuildAliasMap: well-formed root → AliasGroup map is normalized into both
// the backup→root map and the backup→flag map.
func TestBuildAliasMap_WellFormed(t *testing.T) {
	m, flags, _, err := BuildAliasMap(map[string]AliasGroup{
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
	_, flags, _, err := BuildAliasMap(map[string]AliasGroup{
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
	_, flags, _, err := BuildAliasMap(map[string]AliasGroup{
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
	m, _, _, err := BuildAliasMap(map[string]AliasGroup{
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
	_, _, _, err := BuildAliasMap(map[string]AliasGroup{
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
	_, _, _, err := BuildAliasMap(map[string]AliasGroup{
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
	_, _, _, err := BuildAliasMap(map[string]AliasGroup{
		"root.com": {Members: []string{""}},
	})
	if err == nil {
		t.Fatal("expected error for empty backup domain")
	}
}

// BuildAliasMap: empty root key is rejected.
func TestBuildAliasMap_EmptyRoot(t *testing.T) {
	_, _, _, err := BuildAliasMap(map[string]AliasGroup{
		"": {Members: []string{"backup.com"}},
	})
	if err == nil {
		t.Fatal("expected error for empty root domain")
	}
}

// BuildAliasMap: empty input yields empty maps with no error.
func TestBuildAliasMap_EmptyMap(t *testing.T) {
	m, flags, _, err := BuildAliasMap(nil)
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

// BuildAliasMap must not mutate the caller's AliasGroup.Members slice. The
// fold-for-comparison happens in a local variable so the operator-authored
// case is preserved on the original group for the rewrite path to consume.
func TestBuildAliasMap_MembersOriginalCasePreserved(t *testing.T) {
	groups := map[string]AliasGroup{
		"Root.Com": {
			Members: []string{"Example.Com", "MIRROR.com"},
		},
	}
	if _, _, _, err := BuildAliasMap(groups); err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	got := groups["Root.Com"].Members
	if len(got) != 2 {
		t.Fatalf("Members len = %d, want 2", len(got))
	}
	if got[0] != "Example.Com" {
		t.Errorf("Members[0] = %q, want %q (original yaml case must survive)", got[0], "Example.Com")
	}
	if got[1] != "MIRROR.com" {
		t.Errorf("Members[1] = %q, want %q (original yaml case must survive)", got[1], "MIRROR.com")
	}
}

// BuildAliasMap: lookup with the lowercase fold of a mixed-case backup hits
// the same root the YAML declared, regardless of how the operator cased the
// names in config. Both keys and values in the returned map are LookupKey
// folds so callers index with dnsutil.LookupKey(qname).
func TestBuildAliasMap_LookupViaLowercaseFold(t *testing.T) {
	m, flags, _, err := BuildAliasMap(map[string]AliasGroup{
		"Root.Com": {
			Members:            []string{"Example.Com"},
			RewriteRDATALabels: true,
		},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if got := m["example.com."]; got != "root.com." {
		t.Errorf("m[example.com.] = %q, want %q", got, "root.com.")
	}
	if !flags["example.com."] {
		t.Errorf("flags[example.com.] = false, want true")
	}
}

// BuildAliasMap: a root whose Members list is empty yields empty maps and no error.
func TestBuildAliasMap_EmptyMembers(t *testing.T) {
	m, flags, _, err := BuildAliasMap(map[string]AliasGroup{
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

// BuildAliasMap: the collapse lookup is keyed by the root origin's
// lookup-fold FQDN (mixed-case root key folds to lowercase + trailing dot)
// and carries the group's CollapseCNAMEChain setting.
func TestBuildAliasMap_CollapseFlagKeyedByRootFold(t *testing.T) {
	_, _, collapse, err := BuildAliasMap(map[string]AliasGroup{
		"Root.COM": {
			Members:            []string{"backup.com"},
			CollapseCNAMEChain: true,
		},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if !collapse["root.com."] {
		t.Errorf("collapse[root.com.] = false, want true")
	}
	if collapse["backup.com."] {
		t.Errorf("collapse[backup.com.] = true, want false (lookup is keyed by root, not backup)")
	}
}

// BuildAliasMap: a group that does not enable collapse contributes no entry
// to the collapse lookup — a missing key means disabled.
func TestBuildAliasMap_CollapseFlagAbsentMeansNoEntry(t *testing.T) {
	_, _, collapse, err := BuildAliasMap(map[string]AliasGroup{
		"root.com": {Members: []string{"backup.com"}},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if _, ok := collapse["root.com."]; ok {
		t.Errorf("collapse[root.com.] entry exists, want no entry when the flag is not set")
	}
	if len(collapse) != 0 {
		t.Errorf("len(collapse) = %d, want 0", len(collapse))
	}
}

// BuildAliasMap: groups with different collapse settings coexist — only the
// enabled root gains an entry.
func TestBuildAliasMap_CollapseFlagMixedGroups(t *testing.T) {
	_, _, collapse, err := BuildAliasMap(map[string]AliasGroup{
		"root-a.net": {Members: []string{"alias-a.net"}, CollapseCNAMEChain: true},
		"root-b.net": {Members: []string{"alias-b.net"}},
	})
	if err != nil {
		t.Fatalf("BuildAliasMap: %v", err)
	}
	if !collapse["root-a.net."] {
		t.Errorf("collapse[root-a.net.] = false, want true")
	}
	if _, ok := collapse["root-b.net."]; ok {
		t.Errorf("collapse[root-b.net.] entry exists, want no entry")
	}
}
