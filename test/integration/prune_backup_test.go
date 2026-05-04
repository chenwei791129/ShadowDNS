package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPruneBackup_NoCandidatesOnCleanFixture verifies that running against
// the shipped backup/root fixtures reports no deletions.
//
// The invariant this relies on: every overridable RRSet (TXT/MX/SRV) in
// testdata/integration/master/backup.example_* differs from its aliased
// root RRSet in example.com_view-*. If a future edit aligns a backup RRSet
// with root (or drops a backup-side TXT/MX/SRV), this test will flip to
// reporting a deletion — update the fixture or the test accordingly.
func TestPruneBackup_NoCandidatesOnCleanFixture(t *testing.T) {
	bin := buildShadowDNSBinary(t)
	tmpDir := prepareFixture(t)

	cmd := exec.Command(bin,
		"prune-backup",
		"--named-conf", filepath.Join(tmpDir, "named.conf"),
		"--config", filepath.Join(tmpDir, "shadowdns.yaml"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prune-backup exit err=%v, out=%s", err, out)
	}
	if !strings.Contains(string(out), "no redundant records found") {
		t.Errorf("expected 'no redundant records found', got:\n%s", out)
	}

	// No mutation: every *.fwd file has no .bak sibling.
	walkAndAssertNoBak(t, filepath.Join(tmpDir, "master"))
}

// TestPruneBackup_ApplyDeletesRedundantOverlayAndCreatesBak seeds the backup
// file with a TXT that matches the root exactly, then runs the CLI with
// --apply and verifies (a) the redundant record is gone, (b) the .bak holds
// the pre-apply content verbatim, (c) included fragment files are left
// untouched when they had no deletion candidates.
func TestPruneBackup_ApplyDeletesRedundantOverlayAndCreatesBak(t *testing.T) {
	bin := buildShadowDNSBinary(t)
	tmpDir := prepareFixture(t)

	// Inject a redundant RRSet: root view-th's _sip._tcp SRV is
	// "10 5 5060 sip.example.com.". Mirroring it into backup at the same
	// relative owner makes the backup RRSet byte-equivalent to root's ⇒
	// eligible for deletion under the overridable-type equality rule.
	redundantSRV := `_sip._tcp IN SRV 10 5 5060 sip.example.com.`
	backupFile := filepath.Join(tmpDir, "master", "backup.example_view-th.fwd")
	injectLine(t, backupFile, "\n"+redundantSRV+"\n")
	content, _ := os.ReadFile(backupFile)
	if !strings.Contains(string(content), redundantSRV) {
		t.Fatalf("seed failed: %s", content)
	}
	originalBackup := string(content)

	cmd := exec.Command(bin,
		"prune-backup",
		"--named-conf", filepath.Join(tmpDir, "named.conf"),
		"--config", filepath.Join(tmpDir, "shadowdns.yaml"),
		"--apply",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prune-backup --apply exit err=%v, out=%s", err, out)
	}

	// Backup file no longer contains the redundant MX line (but keeps its SOA).
	pruned, _ := os.ReadFile(backupFile)
	if strings.Contains(string(pruned), redundantSRV) {
		t.Errorf("redundant SRV still present post-apply:\n%s", pruned)
	}
	if !strings.Contains(string(pruned), "SOA") {
		t.Errorf("SOA lost post-apply:\n%s", pruned)
	}

	// .bak preserves pre-apply content byte-identical.
	bak, err := os.ReadFile(backupFile + ".bak")
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bak) != originalBackup {
		t.Errorf(".bak content differs from pre-apply original")
	}

	// Include targets (backup.example_overrides) that had no deletion remain
	// un-backed-up.
	overrides := filepath.Join(tmpDir, "master", "backup.example_overrides")
	if _, err := os.Stat(overrides + ".bak"); !os.IsNotExist(err) {
		t.Errorf("include target .bak created unexpectedly: err=%v", err)
	}
}

// prepareFixture copies testdata/integration into a temp dir and rewrites
// placeholder paths, mirroring buildTestServer without starting the server
// (the prune-backup subcommand does not need GeoIP nor listeners).
func prepareFixture(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	copyFixtures(t, tmpDir)
	patchNamedConf(t, tmpDir)
	return tmpDir
}

func walkAndAssertNoBak(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".bak") {
			t.Errorf("unexpected .bak present: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func injectLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("append: %v", err)
	}
}
