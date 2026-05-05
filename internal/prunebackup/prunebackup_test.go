package prunebackup

import (
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestPlanPair_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	backupFile := writeFile(t, dir, "backup.fwd", `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
@ IN NS  ns1.backup.example.
@ IN MX  10 shared.example.net.
www IN A 192.0.2.10
child IN NS ns.other.
mail IN TXT "same-as-root"
mail IN MX  20 differs-from-root.example.net.
`)

	rootFile := writeFile(t, dir, "root.fwd", `$TTL 600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. 2024010101 3600 600 604800 300
@ IN NS  ns1.example.com.
@ IN MX  10 shared.example.net.
mail IN TXT "same-as-root"
mail IN MX  10 root-only.example.net.
`)

	plan, err := PlanPair(backupFile, rootFile, "view-th",
		"backup.example.", "example.com.", dir, zap.NewNop())
	if err != nil {
		t.Fatalf("PlanPair: %v", err)
	}

	delKinds := map[string]bool{}
	for _, d := range plan.Deletions {
		delKinds[d.Type+"/"+strings.TrimSuffix(strings.ToLower(d.Owner), ".")] = true
	}

	// Expected deletions:
	//   - A @ www.backup.example. (non-overridable)
	//   - NS @ child.backup.example. (non-apex NS, non-overridable path)
	//   - MX @ backup.example. (apex MX equal to root's apex MX)
	//   - TXT @ mail.backup.example. (equal RRSet)
	wantDeletes := []string{
		"A/www.backup.example",
		"NS/child.backup.example",
		"MX/backup.example",
		"TXT/mail.backup.example",
	}
	for _, w := range wantDeletes {
		if !delKinds[w] {
			t.Errorf("expected deletion %s, got %v", w, delKinds)
		}
	}

	// Apex NS and SOA must be retained.
	for _, d := range plan.Deletions {
		if d.Type == "SOA" {
			t.Errorf("SOA incorrectly deleted: %+v", d)
		}
		owner := strings.TrimSuffix(strings.ToLower(d.Owner), ".")
		if d.Type == "NS" && owner == "backup.example" {
			t.Errorf("apex NS incorrectly deleted: %+v", d)
		}
	}

	// Mail MX differs from root (20 vs 10) so whole mail MX RRSet is retained.
	if delKinds["MX/mail.backup.example"] {
		t.Errorf("mail MX should be retained (differs from root), but was deleted")
	}

	// plan.Files should contain the backup file with a new body.
	if _, ok := plan.Files[filepath.Clean(backupFile)]; !ok {
		// LoadZoneTree normalises paths via filepath.Abs; the test file is
		// already absolute, so accept either form.
		abs, _ := filepath.Abs(backupFile)
		if _, ok := plan.Files[abs]; !ok {
			t.Errorf("plan.Files missing backup file; got keys: %v", keysOf(plan.Files))
		}
	}
}

// TestPlanPair_RootLessMode pins PlanPair's behaviour when rootFile is
// empty: the type-only fast path drives every (owner, rtype) RRSet. Records
// of non-overridable types (CNAME/A/AAAA/PTR/sub-delegation NS) are planned
// for deletion; records of overridable types (TXT/MX/SRV) are retained
// because byte-equality against root cannot be evaluated.
func TestPlanPair_RootLessMode(t *testing.T) {
	dir := t.TempDir()

	backupFile := writeFile(t, dir, "rootless_backup.fwd", `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
@ IN NS  ns1.backup.example.
www IN A 192.0.2.10
ipv6 IN AAAA 2001:db8::1
host IN CNAME root.example.
child IN NS ns.other.
mail IN MX 10 shared.example.net.
mail IN TXT "spf-marker"
sip IN SRV 0 5 5060 sipserver.example.
`)

	plan, err := PlanPair(backupFile, "", "view-th",
		"backup.example.", "root.example.", dir, zap.NewNop())
	if err != nil {
		t.Fatalf("PlanPair (root-less): %v", err)
	}

	got := map[string]int{}
	for _, d := range plan.Deletions {
		got[d.Type]++
	}

	wantDelete := map[string]int{
		"A":     1,
		"AAAA":  1,
		"CNAME": 1,
		"NS":    1, // sub-delegation child IN NS only; apex NS is retained
	}
	for typ, want := range wantDelete {
		if got[typ] != want {
			t.Errorf("root-less deletions for %s: got %d, want %d (full %v)", typ, got[typ], want, got)
		}
	}
	for _, retainType := range []string{"TXT", "MX", "SRV", "SOA"} {
		if got[retainType] != 0 {
			t.Errorf("root-less should not delete %s, got %d (full %v)", retainType, got[retainType], got)
		}
	}
	// Apex NS must be retained.
	for _, d := range plan.Deletions {
		if d.Type == "NS" && strings.EqualFold(strings.TrimSuffix(d.Owner, "."), "backup.example") {
			t.Errorf("apex NS must be retained in root-less mode, but was deleted: %+v", d)
		}
	}
}

// TestPlanPair_RootLessAndNormalCoexist verifies that a pair planned in
// root-less mode does not contaminate a separately planned normal-mode pair
// living in the same baseDir: each plan's deletions reference only its own
// backup file, and root-less retentions for overridable types do not leak
// into the normal-mode plan's decisions. This is the PlanPair-level
// observable contract behind design's "INFO message differs from skip
// WARN" decision (see cmd-level test for the runPruneBackup INFO emission).
func TestPlanPair_RootLessAndNormalCoexist(t *testing.T) {
	dir := t.TempDir()

	rootlessBackup := writeFile(t, dir, "rootless_backup.fwd", `$TTL 300
$ORIGIN backup1.example.
@ IN SOA ns1.backup1.example. hostmaster.backup1.example. 1 300 120 604800 300
@ IN NS  ns1.backup1.example.
www IN A 192.0.2.10
mail IN MX 10 shared.example.net.
`)

	normalBackup := writeFile(t, dir, "normal_backup.fwd", `$TTL 300
$ORIGIN backup2.example.
@ IN SOA ns1.backup2.example. hostmaster.backup2.example. 1 300 120 604800 300
@ IN NS  ns1.backup2.example.
@ IN MX  10 shared.example.net.
www IN A 192.0.2.20
`)

	normalRoot := writeFile(t, dir, "normal_root.fwd", `$TTL 600
$ORIGIN root2.example.
@ IN SOA ns1.root2.example. hostmaster.root2.example. 1 600 120 604800 300
@ IN NS  ns1.root2.example.
@ IN MX  10 shared.example.net.
`)

	plan1, err := PlanPair(rootlessBackup, "", "view-th",
		"backup1.example.", "root1.example.", dir, zap.NewNop())
	if err != nil {
		t.Fatalf("plan1 (root-less): %v", err)
	}
	plan2, err := PlanPair(normalBackup, normalRoot, "view-th",
		"backup2.example.", "root2.example.", dir, zap.NewNop())
	if err != nil {
		t.Fatalf("plan2 (normal): %v", err)
	}

	// plan1 (root-less): deletes A but retains MX (overridable, no root to compare).
	plan1Types := map[string]bool{}
	for _, d := range plan1.Deletions {
		plan1Types[d.Type] = true
		if !strings.Contains(d.File, "rootless_backup") {
			t.Errorf("plan1 (root-less) leaks deletion in foreign file: %+v", d)
		}
	}
	if !plan1Types["A"] {
		t.Errorf("plan1 expected to delete A, got types %v", plan1Types)
	}
	if plan1Types["MX"] {
		t.Errorf("plan1 (root-less) must not delete MX (no root to compare), got types %v", plan1Types)
	}

	// plan2 (normal): deletes A and apex MX (apex MX equal to root → byte-equal delete).
	plan2Types := map[string]bool{}
	for _, d := range plan2.Deletions {
		plan2Types[d.Type] = true
		if !strings.Contains(d.File, "normal_backup") {
			t.Errorf("plan2 (normal) leaks deletion in foreign file: %+v", d)
		}
	}
	if !plan2Types["A"] {
		t.Errorf("plan2 expected to delete A, got types %v", plan2Types)
	}
	if !plan2Types["MX"] {
		t.Errorf("plan2 expected to delete apex MX (equal to root), got types %v", plan2Types)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
