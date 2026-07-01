## Problem

The `prune-backup --apply` file rewrite renames the original zone file to its `.bak` path BEFORE the replacement file exists. If the process is killed, crashes, or the machine loses power in the window between that rename and the final rename of the new content into place, the live zone-file path is left non-existent: the content survives only at `path.bak` plus an orphan temp file. On the next reload the daemon then finds a missing zone. The tool meant to safely rewrite zone files can thus lose the live file on an ill-timed crash.

## Root Cause

In `applyFile` (`internal/prunebackup/apply.go`) the steps are ordered destroy-then-write: it calls `os.Rename(path, bakPath)` to move the original out of the way first, and only afterwards creates, writes, fsyncs, and renames the temp file onto `path`. Between those two renames there is no file at `path`. There is also no directory fsync, so the rename metadata is not durably persisted.

## Proposed Solution

Reorder so the original file remains at `path` until an atomic rename swaps in the fully-written replacement:
1. Write the new content to a sibling temp file, then `Sync` and `Chmod` it to the original mode (as today).
2. Create the `.bak` backup from the original WITHOUT removing the original from `path` — hardlink the original to `bakPath` when possible (removing any pre-existing `.bak` first), falling back to a byte copy across filesystems. The original stays readable at `path` throughout.
3. `os.Rename(tmpName, path)` to atomically replace the original with the new content.
4. `fsync` the containing directory so the rename is durably persisted.

At every instant a valid file exists at `path` (either the original or the complete new content), so a crash at any point leaves the live zone file intact, with `.bak` still holding the pre-apply content.

## Non-Goals

- Preserving file ownership (uid/gid) or refusing to rewrite through symlinks — that is a separate hardening delivered by the prune-backup symlink/ownership change (issue #17); this change is limited to crash-safe write ordering + directory durability.
- Changing which files are selected for pruning, the pruning transformation itself, or `ApplyAll`'s stop-on-first-failure semantics.
- Adding a lock file to serialize concurrent `--apply` runs.

## Success Criteria

- After `applyFile` completes successfully, `path` holds the new content and `bakPath` holds the pre-apply content, with the original file's permission bits preserved (unchanged from current behavior).
- At no point during `applyFile` is `path` absent while the operation is in progress: the original remains at `path` until the final atomic rename replaces it.
- The containing directory is fsync'd after the final rename.
- A unit test asserts that after a successful apply both `path` (new content) and `path.bak` (old content) exist with the expected bytes and mode; a test simulating failure AFTER the backup step but BEFORE/AT the final rename asserts `path` still resolves to a complete valid file (original or new), never missing.
- Existing `ApplyAll` and prune-backup behavior for the success path is unchanged.

## Impact

- Affected specs: prune-backup-cli (modified)
- Affected code:
  - Modified: internal/prunebackup/apply.go
  - New: internal/prunebackup/apply_writeorder_test.go
