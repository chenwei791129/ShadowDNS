## MODIFIED Requirements

### Requirement: SIGHUP triggers configuration reload

The server process SHALL listen for the SIGHUP signal. Upon receiving SIGHUP, the server SHALL re-read the named.conf configuration file and the aliases file unconditionally. For each zone file referenced by the configuration, the server SHALL compute a fingerprint and compare it to the fingerprint recorded during the previous load; the server SHALL re-parse only zone files whose fingerprint has changed, new zone files that had no previous fingerprint, and (when verify mode is `none`) all zone files unconditionally. Zone files whose fingerprint is unchanged SHALL reuse the previously parsed `*zone.Zone` object by pointer. After constructing the new server state, the server SHALL replace the in-memory server state atomically.

#### Scenario: Successful reload after zone file update

- **WHEN** a zone file on disk is modified and the operator sends SIGHUP to the server process
- **THEN** the server SHALL detect the fingerprint change, re-parse only the modified zone file, reuse pointers for all other unchanged zone files, and atomically replace the current state
- **THEN** subsequent DNS queries SHALL be answered using the new state

#### Scenario: Successful reload after aliases file update

- **WHEN** the aliases file is modified and the operator sends SIGHUP to the server process
- **THEN** the server SHALL reload the aliases file and rebuild the server state with the updated alias map
- **THEN** zone files whose fingerprints are unchanged SHALL have their `*zone.Zone` pointers reused in the new state

#### Scenario: Successful reload after named.conf update

- **WHEN** named.conf is modified (e.g., a new zone is added or a view's match-clients is changed) and the operator sends SIGHUP to the server process
- **THEN** the server SHALL reload named.conf and rebuild the server state reflecting the new configuration
- **THEN** newly added zones SHALL be parsed fresh, removed zones SHALL be dropped, and unchanged zones SHALL reuse their `*zone.Zone` pointer

#### Scenario: First reload with no previous fingerprint

- **WHEN** the server starts up and performs its initial state build before any SIGHUP has been received
- **THEN** the server SHALL parse every zone file referenced by the configuration (no previous fingerprint exists)
- **THEN** the server SHALL record a fingerprint for each parsed zone file for use by subsequent reloads

## ADDED Requirements

### Requirement: Zone file fingerprint strategy

The server SHALL compute a fingerprint for each zone file consisting of a size component and a content-hash component. The size component SHALL be the file size in bytes obtained via a single `os.Stat` call. The content-hash component SHALL be the xxhash64 of the full file contents computed using `github.com/cespare/xxhash/v2`. A zone file's fingerprint SHALL be treated as unchanged if and only if both the size component and the content-hash component match the fingerprint recorded during the previous successful load. Fingerprints SHALL be stored in the server state so that each reload compares against the fingerprints produced by the immediately preceding successful load.

#### Scenario: Unchanged zone file is detected as unchanged

- **WHEN** a zone file has the same size and the same content as at the previous load
- **THEN** the xxhash64 computed from its contents SHALL equal the previously recorded hash
- **THEN** the fingerprint comparison SHALL return unchanged and the `*zone.Zone` pointer SHALL be reused

#### Scenario: Zone file with same size but different content is detected as changed

- **WHEN** a zone file has been modified such that its size is identical to the previous load but its contents differ (e.g., after `rsync -avc --inplace` from a source that preserves mtime)
- **THEN** the xxhash64 SHALL differ from the previously recorded hash
- **THEN** the fingerprint comparison SHALL return changed and the zone file SHALL be re-parsed

#### Scenario: Zone file with different size is detected as changed

- **WHEN** a zone file's size differs from the previously recorded size
- **THEN** the fingerprint comparison SHALL return changed without necessarily computing the content hash
- **THEN** the zone file SHALL be re-parsed

### Requirement: Reload verify mode configuration

The server SHALL expose a CLI flag `-reload-verify` that accepts exactly one of the values `hash`, `size`, or `none`. The default value SHALL be `hash`. The value SHALL be read at process startup from `os.Args` and SHALL remain sticky across SIGHUP reloads for the entire process lifetime. The server SHALL reject startup with a non-zero exit code if `-reload-verify` is set to any value other than `hash`, `size`, or `none`. The fingerprint comparison behavior SHALL be selected by this flag as follows:

- `hash`: The server SHALL compute and compare both the size component and the xxhash64 content-hash component.
- `size`: The server SHALL compare only the size component and the file modification time (ns precision), and SHALL NOT read zone file contents for fingerprinting.
- `none`: The server SHALL NOT compute any fingerprint and SHALL re-parse every zone file unconditionally, matching the pre-optimization reload behavior.

#### Scenario: Default reload verify mode is hash

- **WHEN** the server is started without the `-reload-verify` flag
- **THEN** the effective verify mode SHALL be `hash`
- **THEN** subsequent reloads SHALL compute xxhash64 for zone files whose size matches

#### Scenario: Explicit size mode skips content hashing

- **WHEN** the server is started with `-reload-verify=size` and a reload is triggered
- **THEN** the server SHALL compare only `(mtime, size)` fingerprints and SHALL NOT read any zone file contents for fingerprinting purposes
- **THEN** zone files with identical `(mtime, size)` SHALL be treated as unchanged and their pointers reused

#### Scenario: None mode forces full rebuild

- **WHEN** the server is started with `-reload-verify=none` and a reload is triggered
- **THEN** the server SHALL re-parse every zone file referenced by the configuration regardless of any fingerprint
- **THEN** no zone `*zone.Zone` pointer SHALL be reused from the previous state

#### Scenario: Invalid reload verify value rejected at startup

- **WHEN** the server is started with `-reload-verify=foo` (any value other than `hash`, `size`, or `none`)
- **THEN** the server SHALL print an error identifying the invalid value and the set of accepted values
- **THEN** the server SHALL exit with a non-zero exit code before binding listeners

### Requirement: Diff-based zone pointer reuse preserves immutability

When the server reuses a `*zone.Zone` pointer from the previous state in a newly built state, the server SHALL NOT mutate any field of the reused `*zone.Zone` object, including its `Records` map, `SOA`, `Role`, `Origin`, or `Path` fields. Any handler or state-building code that needs to modify zone data SHALL construct a new `*zone.Zone` rather than mutate a shared one.

#### Scenario: Reused zone is not mutated by new state construction

- **WHEN** a zone file's fingerprint is unchanged and its `*zone.Zone` pointer is reused in the new state
- **THEN** no field of the reused `*zone.Zone` SHALL be modified by the reload path
- **THEN** DNS queries served from the old state (in-flight during the swap) and queries served from the new state SHALL observe identical zone data for that zone

### Requirement: Post-swap garbage collection

After `Server.SwapState` stores the new state pointer, the server SHALL invoke `runtime.GC()` followed by `runtime/debug.FreeOSMemory()` exactly once per successful state swap. This SHALL apply regardless of the `-reload-verify` mode.

#### Scenario: GC and memory release invoked after successful swap

- **WHEN** `SwapState` is called with a newly built state and the atomic pointer store completes
- **THEN** the server SHALL invoke `runtime.GC()` synchronously
- **THEN** the server SHALL invoke `debug.FreeOSMemory()` synchronously
- **THEN** the operating system resident set size SHALL begin decreasing toward the post-reload steady state without waiting for the runtime's background GC cycle

#### Scenario: GC is not invoked on failed reload

- **WHEN** reload fails before `SwapState` is called (e.g., due to a zone parse error)
- **THEN** `runtime.GC()` and `debug.FreeOSMemory()` SHALL NOT be invoked by the reload path
- **THEN** the previously loaded state SHALL continue serving queries

### Requirement: Reload diff logging

On each successful reload, the server SHALL log at INFO level the count of zones that were reused by pointer and the count of zones that were re-parsed, per view. These counts SHALL allow operators to verify that the diff-based reload is reusing pointers as expected.

#### Scenario: Reload log reports reuse counts

- **WHEN** a reload completes successfully
- **THEN** the server SHALL emit an INFO log entry containing at minimum: the effective reload verify mode, the total number of zones reused, and the total number of zones re-parsed
- **THEN** when verify mode is `none`, the reused count SHALL be zero and the re-parsed count SHALL equal the total zone count
