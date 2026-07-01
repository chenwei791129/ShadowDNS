package prunebackup

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// TestApplyFile_WriteOrder_SuccessLeavesNewAndBackup asserts the success-path
// contract: after applyFile the path holds the new content with the original
// permission bits, and path+".bak" holds the pre-apply content.
func TestApplyFile_WriteOrder_SuccessLeavesNewAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	const origMode os.FileMode = 0o640
	if err := os.WriteFile(path, []byte("original\n"), origMode); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := applyFile(path, []byte("pruned\n"), zap.NewNop()); err != nil {
		t.Fatalf("applyFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path: %v", err)
	}
	if string(got) != "pruned\n" {
		t.Errorf("path content = %q, want %q", got, "pruned\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat path: %v", err)
	}
	if m := info.Mode().Perm(); m != origMode {
		t.Errorf("path mode = %#o, want %#o", m, origMode)
	}

	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bak) != "original\n" {
		t.Errorf(".bak content = %q, want %q", bak, "original\n")
	}
}

// TestApplyFile_WriteOrder_CrashAfterBackupNeverLeavesPathMissing asserts the
// ordering invariant: if applyFile is interrupted after the backup is created
// and up to the final atomic rename, the path still resolves to a complete,
// valid file (either the original content or the fully-written new content) and
// is never absent.
//
// The failure is injected via afterBackupHook, a test-only seam that fires
// immediately after the backup exists but before os.Rename swaps in the
// replacement. Under the pre-fix destroy-then-write ordering the original was
// already renamed away at this instant, so this assertion fails.
func TestApplyFile_WriteOrder_CrashAfterBackupNeverLeavesPathMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	injected := errSimulatedCrash
	prev := afterBackupHook
	afterBackupHook = func() error { return injected }
	t.Cleanup(func() { afterBackupHook = prev })

	err := applyFile(path, []byte("pruned\n"), zap.NewNop())
	if err == nil {
		t.Fatalf("applyFile: want injected error, got nil")
	}

	// Invariant: the path must still resolve to a complete valid file.
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("path missing after simulated crash: %v", readErr)
	}
	if s := string(got); s != "original\n" && s != "pruned\n" {
		t.Errorf("path content = %q, want original or new content (never partial/missing)", s)
	}

	// The backup must exist and hold the pre-apply content, so recovery is
	// possible even though the apply itself failed.
	bak, bakErr := os.ReadFile(path + ".bak")
	if bakErr != nil {
		t.Fatalf("read .bak after simulated crash: %v", bakErr)
	}
	if string(bak) != "original\n" {
		t.Errorf(".bak content = %q, want %q", bak, "original\n")
	}
}
