## ADDED Requirements

### Requirement: prune-backup rewrite keeps a valid file at the path at all times

The prune-backup apply step SHALL rewrite a zone file so that the original file remains in place at its path until a fully-written replacement is atomically renamed over it. The apply step SHALL NOT remove or rename the original away from its path before the replacement content has been written and fsync'd. The `.bak` backup SHALL be produced from the original while the original still resides at its path (via hardlink where possible, otherwise a byte copy). After the replacement is renamed into place, the apply step SHALL fsync the containing directory. The original file's permission bits SHALL be preserved on the replacement.

#### Scenario: Successful apply leaves both new file and backup

- **WHEN** the apply step rewrites a zone file successfully
- **THEN** the path holds the new content with the original permission bits, and the `.bak` path holds the pre-apply content

#### Scenario: Crash during apply never leaves the path missing

- **WHEN** the apply step is interrupted at any point after the backup is created and up to the final atomic rename
- **THEN** the zone-file path still resolves to a complete valid file — either the original content or the fully-written new content — and is never absent
