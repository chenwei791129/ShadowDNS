//go:build unix

package prunebackup

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"go.uber.org/zap"
)

// TestApplyFile_SymlinkRefused verifies that applyFile refuses to rewrite
// through a symlink: it returns a descriptive error mentioning the symlink and
// leaves the symlink topology intact instead of flattening it into a regular
// file.
func TestApplyFile_SymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-zone.fwd")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	link := filepath.Join(dir, "zone.fwd")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	err := applyFile(link, []byte("pruned\n"), zap.NewNop())
	if err == nil {
		t.Fatalf("applyFile on symlink: got nil error, want a symlink refusal")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "symlink")
	}

	// The link must still be a symlink pointing at the original target, and
	// the rewrite must not have occurred anywhere.
	fi, lerr := os.Lstat(link)
	if lerr != nil {
		t.Fatalf("lstat link after refusal: %v", lerr)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link is no longer a symlink after applyFile (topology destroyed)")
	}
	dest, rerr := os.Readlink(link)
	if rerr != nil {
		t.Fatalf("readlink after refusal: %v", rerr)
	}
	if dest != target {
		t.Errorf("symlink target = %q, want %q (unchanged)", dest, target)
	}
	// The link's target content must be untouched.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "original\n" {
		t.Errorf("target content = %q, want %q (unchanged)", got, "original\n")
	}
	// A refusal must not have created a .bak either.
	if _, err := os.Lstat(link + ".bak"); !os.IsNotExist(err) {
		t.Errorf("refusal should not create %q", link+".bak")
	}
}

// TestApplyFile_PreservesOwnership verifies that a rewritten regular file
// retains the original file's uid/gid. It requires root: only a privileged
// process can seed a file owned by a *different* uid/gid and then prove
// applyFile carried that owner across the rewrite. An unprivileged process
// cannot chown to another id, so the seeded file would be owned by the invoking
// user and the post-apply assertion would hold even if preserveOwner did
// nothing — which is why we skip rather than run a test that cannot fail.
func TestApplyFile_PreservesOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root: an unprivileged process cannot chown to a different uid/gid, so this test cannot exercise preserveOwner")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Seed the file with a non-root owner (nobody, uid/gid 65534 on most Unix
	// systems). Because we run as root here, only a working preserveOwner will
	// carry this owner onto the rewritten file — if it did nothing, the tmp
	// file created by os.CreateTemp would stay owned by root (0) and the
	// assertions below would fail. That is what makes this test exercise our
	// code rather than a same-uid no-op.
	const (
		wantUID uint32 = 65534
		wantGID uint32 = 65534
	)
	if err := os.Chown(path, int(wantUID), int(wantGID)); err != nil {
		t.Skipf("cannot seed a non-root owner (%d:%d) on this system: %v", wantUID, wantGID, err)
	}

	if err := applyFile(path, []byte("pruned\n"), zap.NewNop()); err != nil {
		t.Fatalf("applyFile: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("syscall.Stat_t unavailable on this platform")
	}
	if afterStat.Uid != wantUID {
		t.Errorf("post-apply uid = %d, want %d", afterStat.Uid, wantUID)
	}
	if afterStat.Gid != wantGID {
		t.Errorf("post-apply gid = %d, want %d", afterStat.Gid, wantGID)
	}
}
