package prunebackup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"go.uber.org/zap"
)

// errSimulatedCrash is the error injected by afterBackupHook in tests to
// simulate a crash after the backup is created but before the final rename.
var errSimulatedCrash = errors.New("prunebackup: simulated crash (test only)")

// afterBackupHook is a test-only seam. In production it is nil; tests set it to
// force applyFile to abort immediately after the backup exists but before the
// atomic rename swaps in the replacement, so the ordering invariant (path is
// never missing) can be asserted. It is never non-nil in shipped binaries.
var afterBackupHook func() error

// applyFile atomically replaces path with newContent while keeping a complete,
// valid file at path at all times. Steps:
//  1. Write the new content to a sibling temp file, then Sync and Chmod it to
//     the original mode (so a non-root daemon keeps read access).
//  2. Produce path+".bak" from the original while it still resides at path —
//     hardlink (os.Link) when possible, else a byte copy across filesystems —
//     after removing any pre-existing .bak.
//  3. os.Rename(tmp, path) to atomically replace the original with the new
//     content.
//  4. fsync the containing directory so the rename metadata is durable.
//
// Because the original is never renamed away before the replacement is fully
// written and swapped in, a crash at any instant leaves path holding either the
// original or the complete new content, never a missing file. Failure is
// surfaced as an error without rollback; the caller recovers pre-apply state
// from .bak.
func applyFile(path string, newContent []byte, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Capture the original file mode so the rewritten file inherits the
	// operator's chmod, not os.CreateTemp's 0600. Without this, files served
	// by a non-root daemon (e.g. shadowdns running as its own user) become
	// unreadable after --apply.
	origInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("prunebackup: stat %q: %w", path, err)
	}
	origMode := origInfo.Mode().Perm()

	// Step 1: write the replacement to a sibling temp file first. The original
	// is untouched at path throughout this step.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("prunebackup: create tmp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(newContent); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("prunebackup: write tmp %q: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("prunebackup: fsync tmp %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("prunebackup: close tmp %q: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, origMode); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("prunebackup: chmod %q: %w", tmpName, err)
	}

	// Step 2: back up the original while it is still at path. Removing any
	// stale .bak first keeps os.Link's semantics simple; the original remains
	// readable at path the whole time.
	bakPath := path + ".bak"
	if err := backupOriginal(path, bakPath, origMode, logger); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	// Test-only injection point: at this instant the backup exists and the
	// original still lives at path. A crash here must leave path intact.
	if afterBackupHook != nil {
		if err := afterBackupHook(); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
	}

	// Step 3: atomically replace the original with the new content.
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("prunebackup: rename %q -> %q: %w", tmpName, path, err)
	}

	// Step 4: fsync the containing directory so the rename is durably
	// persisted. A failure here does not undo the rename; the new content is
	// already at path, so surface the error without removing anything.
	if err := fsyncDir(dir); err != nil {
		return err
	}
	return nil
}

// backupOriginal creates bakPath from the original still residing at path,
// removing any pre-existing .bak first. It hardlinks the original into place
// when possible and falls back to a byte copy (preserving origMode) when the
// link crosses a filesystem boundary or is otherwise unsupported.
func backupOriginal(path, bakPath string, origMode os.FileMode, logger *zap.Logger) error {
	if _, err := os.Lstat(bakPath); err == nil {
		logger.Sugar().Infow("overwriting existing .bak backup", "path", bakPath)
		if err := os.Remove(bakPath); err != nil {
			return fmt.Errorf("prunebackup: remove existing %q: %w", bakPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("prunebackup: stat %q: %w", bakPath, err)
	}

	if err := os.Link(path, bakPath); err == nil {
		return nil
	}
	// Cross-filesystem or unsupported link: fall back to a byte copy.
	if err := copyFile(path, bakPath, origMode); err != nil {
		return fmt.Errorf("prunebackup: backup %q -> %q: %w", path, bakPath, err)
	}
	return nil
}

// copyFile copies src to dst with the given mode, fsyncing dst so the backup is
// durable before the original is replaced.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// fsyncDir opens dir and fsyncs it so recently renamed entries are durably
// persisted.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("prunebackup: open dir %q: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("prunebackup: fsync dir %q: %w", dir, err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("prunebackup: close dir %q: %w", dir, err)
	}
	return nil
}

// ApplyAll applies newContent for every (path → bytes) pair in changes,
// stopping on the first failure. Files absent from changes are never read,
// renamed, or backed up. Iteration is sorted so behaviour is deterministic.
func ApplyAll(changes map[string][]byte, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	paths := make([]string, 0, len(changes))
	for p := range changes {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	for _, p := range paths {
		if err := applyFile(p, changes[p], logger); err != nil {
			return err
		}
	}
	return nil
}
