## ADDED Requirements

### Requirement: Process zone pairs as a streaming pipeline

The sub-command SHALL process each `(view, backup zone)` pair as an independent pipeline stage: plan, sort the pair's deletion list, emit dry-run output for that pair (or apply that pair's pruned files when `--apply` is supplied), then release the pair's intermediate plan structures before advancing to the next pair. The sub-command SHALL NOT retain the union of every pair's `Deletion` list or the union of every pair's pruned file contents in memory simultaneously.

#### Scenario: peak memory tracks single-pair work, not full job

- **WHEN** the sub-command runs across N pairs whose combined deletion count exceeds any single pair's count by orders of magnitude
- **THEN** the resident memory ceiling SHALL be proportional to the largest single pair plus a fixed overhead, AND SHALL NOT grow proportionally to the sum across all pairs

#### Scenario: per-pair release between pairs

- **WHEN** the sub-command completes plan generation, output, and (under `--apply`) writes for pair P
- **THEN** the in-memory `Deletion` list and pruned-file map for pair P SHALL become unreachable from the running goroutine BEFORE the sub-command begins planning pair P+1

### Requirement: Deterministic dry-run output order across pairs and within each pair

The sub-command SHALL emit dry-run output with two-level deterministic ordering: pairs SHALL be processed in ascending `(view-name, backup-origin)` order, and within each pair the emitted lines SHALL be sorted ascending by `(source-file-path, start-line)`. The sub-command SHALL NOT reorder lines across pair boundaries; pair P's output SHALL appear in full before pair P+1's first line.

#### Scenario: two pairs produce contiguous, ordered blocks

- **WHEN** the sub-command processes pairs `(view-a, backup-1)` and `(view-a, backup-2)` with deletion candidates in each
- **THEN** every line for `(view-a, backup-1)` SHALL appear before any line for `(view-a, backup-2)` AND each block's lines SHALL be sorted ascending by `(file-path, start-line)`

##### Example: ordering across two pairs

| Pair processed | File:Line emitted | Position in stream |
|---|---|---|
| (view-a, backup-1) | /zones/b1.fwd:10-10 | 1 |
| (view-a, backup-1) | /zones/b1.fwd:20-20 | 2 |
| (view-a, backup-1) | /zones/b1_inc.fwd:5-5 | 3 |
| (view-a, backup-2) | /zones/b2.fwd:1-1 | 4 |
| (view-a, backup-2) | /zones/b2.fwd:7-7 | 5 |

### Requirement: Apply writes flush per pair instead of batched at the end

When `--apply` is supplied, the sub-command SHALL invoke `ApplyAll` once per pair, immediately after that pair's plan is computed and dry-run output for the pair is emitted, BEFORE advancing to the next pair. The sub-command SHALL NOT defer all writes until every pair has been planned. The fail-stop semantics SHALL be preserved: any pair whose write step fails SHALL stop the sub-command immediately with a non-zero exit, leaving already-written files in their post-apply state and their `.bak` backups intact.

#### Scenario: write failure on pair K stops the run

- **WHEN** `--apply` runs across pairs P1 ... Pn AND the write step for pair Pk (1 < k ≤ n) fails
- **THEN** pairs P1 ... P(k-1) SHALL have their files rewritten on disk with corresponding `.bak` files present, AND pairs P(k+1) ... Pn SHALL NOT have been read or modified, AND the sub-command SHALL exit with a non-zero status

#### Scenario: parse failure on pair K stops the run before any later pair runs

- **WHEN** `--apply` runs across pairs P1 ... Pn AND a backup zone file in pair Pk fails to parse during plan generation
- **THEN** pairs P1 ... P(k-1) SHALL have already completed their writes on disk, AND pairs Pk ... Pn SHALL have produced no writes, AND the sub-command SHALL exit with a non-zero status

### Requirement: Output writer flushes before exit

The sub-command SHALL wrap its dry-run output destination in a buffered writer with a buffer size of at least 64 KiB to coalesce per-line writes into batched syscalls. The sub-command SHALL flush the writer before any return path that signals completion or failure, so no candidate line is lost on either the success or the error exit.

#### Scenario: success exit emits every candidate

- **WHEN** the sub-command completes processing without error AND M > 0 deletion candidates were produced across all pairs
- **THEN** the output SHALL contain exactly M candidate lines AND the trailing line SHALL be flushed to the destination before the sub-command exits

#### Scenario: error exit preserves emitted lines

- **WHEN** the sub-command emits J > 0 candidate lines THEN encounters a fatal error before processing finishes
- **THEN** the J already-emitted lines SHALL be flushed to the destination AND SHALL NOT be lost as a side effect of buffering
