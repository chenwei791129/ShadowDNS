package prunebackup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.uber.org/zap"
)

// applyFile atomically replaces path with newContent. Steps:
//  1. Rename the original file to path+".bak" (os.Rename overwrites any
//     pre-existing .bak atomically); an INFO log is emitted when the
//     pre-rename Lstat observed an existing .bak.
//  2. Write a sibling temp file, call Sync, then rename it onto path.
//
// Failure is surfaced as an error without rollback; the caller recovers
// pre-apply state from .bak.
func applyFile(path string, newContent []byte, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Capture the original file mode before the rename so the rewritten
	// file inherits the operator's chmod, not os.CreateTemp's 0600. Without
	// this, files served by a non-root daemon (e.g. shadowdns running as
	// its own user) become unreadable after --apply.
	origInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("prunebackup: stat %q: %w", path, err)
	}
	origMode := origInfo.Mode().Perm()

	bakPath := path + ".bak"
	// os.Rename below would overwrite .bak atomically on POSIX regardless;
	// Lstat here is purely for the operator-facing log entry. A race where
	// the .bak vanishes between Lstat and Rename only loses the log line,
	// not the safety guarantee.
	if _, err := os.Lstat(bakPath); err == nil {
		logger.Sugar().Infow("overwriting existing .bak backup", "path", bakPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("prunebackup: stat %q: %w", bakPath, err)
	}

	if err := os.Rename(path, bakPath); err != nil {
		return fmt.Errorf("prunebackup: rename %q -> %q: %w", path, bakPath, err)
	}

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
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("prunebackup: rename %q -> %q: %w", tmpName, path, err)
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
