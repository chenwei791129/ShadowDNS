## MODIFIED Requirements

### Requirement: Reload verify mode configuration

The server SHALL expose a CLI flag `--reload-verify` that accepts exactly one of the values `hash`, `size`, or `none`. The default value SHALL be `hash`. The value SHALL be read at process startup from `os.Args` and SHALL remain sticky across SIGHUP reloads for the entire process lifetime. The server SHALL reject startup with a non-zero exit code if `--reload-verify` is set to any value other than `hash`, `size`, or `none`. The fingerprint comparison behavior SHALL be selected by this flag as follows:

- `hash`: The server SHALL compute and compare both the size component and the xxhash64 content-hash component.
- `size`: The server SHALL compare only the size component and the file modification time (ns precision), and SHALL NOT read zone file contents for fingerprinting.
- `none`: The server SHALL NOT compute any fingerprint and SHALL re-parse every zone file unconditionally, matching the pre-optimization reload behavior.

#### Scenario: Default reload verify mode is hash

- **WHEN** the server is started without the `--reload-verify` flag
- **THEN** the effective verify mode SHALL be `hash`
- **THEN** subsequent reloads SHALL compute xxhash64 for zone files whose size matches

#### Scenario: Explicit size mode skips content hashing

- **WHEN** the server is started with `--reload-verify=size` and a reload is triggered
- **THEN** the server SHALL compare only `(mtime, size)` fingerprints and SHALL NOT read any zone file contents for fingerprinting purposes
- **THEN** zone files with identical `(mtime, size)` SHALL be treated as unchanged and their pointers reused

#### Scenario: None mode forces full rebuild

- **WHEN** the server is started with `--reload-verify=none` and a reload is triggered
- **THEN** the server SHALL re-parse every zone file referenced by the configuration regardless of any fingerprint
- **THEN** no zone `*zone.Zone` pointer SHALL be reused from the previous state

#### Scenario: Invalid reload verify value rejected at startup

- **WHEN** the server is started with `--reload-verify=foo` (any value other than `hash`, `size`, or `none`)
- **THEN** the server SHALL print an error identifying the invalid value and the set of accepted values
- **THEN** the server SHALL exit with a non-zero exit code before binding listeners
