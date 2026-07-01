## ADDED Requirements

### Requirement: prune-backup refuses symlinked paths and preserves file ownership

The prune-backup apply step SHALL NOT silently replace a symlinked zone-file path with a regular file. Before rewriting, it SHALL detect a symlink at the path via a non-following stat and, when the path is a symlink, SHALL return a descriptive error identifying the symlink and SHALL leave the symlink intact. For a regular file, the apply step SHALL preserve the original file's ownership (uid/gid) on the replacement in addition to its permission bits, so a daemon running as a non-root user retains read access after the rewrite. Ownership preservation SHALL be best-effort where the platform or filesystem disallows it and SHALL be implemented so non-Unix builds still compile.

#### Scenario: Symlinked path is refused

- **WHEN** the apply step is asked to rewrite a zone-file path that is a symlink
- **THEN** it returns a descriptive error mentioning the symlink and the symlink is left intact (not replaced by a regular file)

#### Scenario: Ownership preserved on regular-file rewrite

- **WHEN** the apply step rewrites a regular zone file on a platform that permits chown
- **THEN** the rewritten file's uid and gid match the original file's uid and gid, in addition to preserving its permission bits
