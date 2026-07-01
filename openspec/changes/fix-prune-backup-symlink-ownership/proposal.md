## Problem

The `prune-backup --apply` file rewrite silently destroys a zone file's symlink nature and can change its ownership. If the zone-file path is a symlink, the rewrite replaces it with a regular file (the symlink topology is lost). The replacement is created by `os.CreateTemp`, so it is owned by the invoking user (typically root when an operator runs the CLI), and only the permission bits — not uid/gid — are preserved. A daemon running as a non-root user (e.g. `shadowdns`) can then be unable to read the rewritten file.

## Root Cause

In `applyFile` (`internal/prunebackup/apply.go`) the original file is inspected with `os.Stat`, which follows symlinks, so the code never notices the path is a symlink and unconditionally replaces it with a regular file via the temp-file rename. Ownership is not captured or restored: only `origInfo.Mode().Perm()` is preserved via `os.Chmod`; the uid/gid from the original file's `syscall.Stat_t` are never applied to the replacement.

## Proposed Solution

This change builds on the crash-safe write-ordering change (issue #16) and adds two safeguards to `applyFile`:
1. Refuse to rewrite through a symlink. `os.Lstat` the path; if it is a symlink, return a descriptive error instead of replacing it, so the operator is alerted rather than silently flattening the link. (A future enhancement MAY choose to resolve-and-rewrite the target deliberately; this change conservatively refuses.)
2. Preserve ownership. Capture the original file's uid/gid from `origInfo.Sys().(*syscall.Stat_t)` and `os.Chown` the temp file to that uid/gid before the final rename, so the rewritten file keeps the original owner and stays readable by the daemon user. The chown is best-effort on platforms/filesystems where it is not permitted, and MUST be guarded so non-Unix builds still compile.

## Non-Goals

- Resolving and rewriting the symlink target in place (this change refuses symlinks rather than following them).
- The crash-safe write-ordering / directory-fsync behavior, which is delivered by the prune-backup write-order change (issue #16); this change layers on top of it.
- Changing pruning selection or the prune transformation itself.

## Success Criteria

- When the zone-file path is a symlink, `applyFile` returns a descriptive error (mentioning the symlink) and does NOT replace the symlink with a regular file.
- After a successful apply on a regular file, the rewritten file's uid/gid match the original file's uid/gid (where the platform permits chown), in addition to the preserved permission bits.
- A unit test asserts that applying to a symlinked path returns the symlink error and leaves the symlink intact; a second test (guarded for Unix) asserts the rewritten regular file retains the original uid/gid, or is skipped when the test cannot exercise chown (e.g. unprivileged and uid unchanged).
- Existing prune-backup success-path and crash-safety behavior from the write-order change is unchanged.

## Impact

- Affected specs: prune-backup-cli (modified)
- Affected code:
  - Modified: internal/prunebackup/apply.go
  - New: internal/prunebackup/apply_symlink_test.go
