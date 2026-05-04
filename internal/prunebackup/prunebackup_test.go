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

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
