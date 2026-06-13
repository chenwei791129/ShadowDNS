## MODIFIED Requirements

### Requirement: Fail startup when GeoIP databases are missing or unreadable

For the purpose of this requirement, `options.geoip-directory` counts as **unset** when the option is absent or its value is the empty string; the two cases SHALL behave identically.

When `options.geoip-directory` is set, the view-matcher SHALL load a country mmdb and an ASN mmdb from that directory at startup, regardless of whether any view declares a geo rule. For each database, the loader SHALL try candidate filenames in a fixed priority order and accept the first file that successfully opens and passes MaxMind mmdb validation:

- Country: `GeoIP2-Country.mmdb`, then `GeoLite2-Country.mmdb`.
- ASN: `GeoIP2-ASN.mmdb`, then `GeoLite2-ASN.mmdb`.

Country and ASN SHALL be resolved independently — the loader MUST NOT require both databases to use the same edition.

If every candidate filename for either database fails to open or validate, the server SHALL exit with a non-zero status and an error message listing every candidate path that was attempted and the reason each failed.

Upon successful load, the server SHALL log the full path of each opened mmdb so operators can identify the edition in use from the `path` field.

When `options.geoip-directory` is unset and at least one view's `match-clients` contains a country or ASN rule, the server SHALL exit with a non-zero status and an explicit configuration error (never a file-open error) naming the first such view (in declaration order) together with that view's source file path and line number. When `options.geoip-directory` is unset and no view declares a country or ASN rule, startup SHALL proceed without loading any GeoIP database.

#### Scenario: Paid-edition country mmdb loads when present

- **WHEN** `GeoIP2-Country.mmdb` exists in the geoip-directory and is a valid mmdb
- **THEN** the loader opens it AND does not attempt `GeoLite2-Country.mmdb`

#### Scenario: Falls back to GeoLite2 when GeoIP2 is absent

- **WHEN** `GeoIP2-Country.mmdb` does not exist AND `GeoLite2-Country.mmdb` exists and is a valid mmdb
- **THEN** the loader opens `GeoLite2-Country.mmdb`

#### Scenario: Mixed editions across Country and ASN

- **WHEN** only `GeoIP2-Country.mmdb` and `GeoLite2-ASN.mmdb` exist in the geoip-directory
- **THEN** the loader opens `GeoIP2-Country.mmdb` for country AND `GeoLite2-ASN.mmdb` for ASN

#### Scenario: Higher-priority file that fails validation falls through to next candidate

- **WHEN** `GeoIP2-Country.mmdb` exists but fails mmdb validation AND `GeoLite2-Country.mmdb` exists and is valid
- **THEN** the loader opens `GeoLite2-Country.mmdb`

#### Scenario: All country candidates missing is fatal

- **WHEN** neither `GeoIP2-Country.mmdb` nor `GeoLite2-Country.mmdb` exists at the configured path
- **THEN** the process exits with a non-zero status AND the error message names both attempted paths

#### Scenario: All ASN candidates invalid is fatal

- **WHEN** both `GeoIP2-ASN.mmdb` and `GeoLite2-ASN.mmdb` exist but both fail library-level mmdb validation
- **THEN** the process exits with a non-zero status AND the error message lists both paths together with the validation error for each

#### Scenario: Successful load is logged with full path

- **WHEN** the loader successfully opens a mmdb file for either database
- **THEN** the server emits an info-level log entry whose `path` field contains the full path of the opened file

#### Scenario: Geo rules without geoip-directory fail startup naming the offending view

- **WHEN** a view `"view-th"` declared in `master.zones` at line 12 contains `geoip country TH;` in its match-clients AND `options` has no `geoip-directory`
- **THEN** the process exits with a non-zero status AND the error message contains the view name `view-th`, the `master.zones` path, and line 12

#### Scenario: Empty-string geoip-directory behaves as unset

- **WHEN** `options` declares `geoip-directory "";` AND a view's match-clients contains `geoip country TH;`
- **THEN** the process exits with the same explicit configuration error as when the option is absent — never a relative-path file-open error

#### Scenario: Directory set but no geo rules still loads and validates

- **WHEN** `geoip-directory` is set to a non-empty path AND no view declares a country or ASN rule
- **THEN** both mmdb databases are loaded and validated exactly as before, and a load failure remains fatal

## ADDED Requirements

### Requirement: Operate without GeoIP databases when configuration declares no geo rules

When the entire configuration (the root `named.conf` plus every included file) declares no country and no ASN match-clients rule and `options.geoip-directory` is unset (absent or empty), the server SHALL start and serve queries with nil GeoIP database handles: `any`, IP, and CIDR rules SHALL evaluate exactly as when databases are loaded, and view resolution SHALL be unaffected. The startup readiness log SHALL carry a boolean field named `geoip_enabled` reporting whether GeoIP databases are loaded, and the `--dry-run` summary log SHALL carry the same `geoip_enabled` field. `--dry-run` SHALL succeed and fail under exactly the same GeoIP conditions as a real startup: it SHALL succeed without mmdb files when no geo rule exists, and SHALL fail with the same configuration error when a geo rule exists without `geoip-directory`. Whenever a configuration load (startup, or a SIGHUP reload) completes with `--ecs-enable` active and no GeoIP database loaded, the server SHALL log one warning stating that ECS cannot influence view selection (the ECS option echo behavior is retained); the warning SHALL NOT prevent startup or reload.

#### Scenario: Config with only IP and CIDR rules starts without mmdb files

- **WHEN** every view's match-clients uses only `any`, IP, or CIDR rules AND `geoip-directory` is not set AND no mmdb file exists on the host
- **THEN** the server starts successfully AND a query from a source IP matching a CIDR rule receives the authoritative answer from that view's zone

#### Scenario: Readiness log reports GeoIP state

- **WHEN** the server finishes startup without loading GeoIP databases
- **THEN** the readiness log line carries `geoip_enabled=false`

#### Scenario: Dry-run succeeds without GeoIP and reports the state

- **WHEN** `--dry-run` runs against a config with no geo rules and no `geoip-directory` on a host with no mmdb files
- **THEN** the dry-run exits successfully AND its summary log line carries `geoip_enabled=false`

#### Scenario: Dry-run fails when geo rules lack geoip-directory

- **WHEN** `--dry-run` runs against a config where a view declares `geoip country TH;` and `geoip-directory` is unset
- **THEN** the dry-run exits with a non-zero status AND the error names the offending view with its source file and line — identical to a real startup

#### Scenario: ECS enabled without GeoIP warns at startup

- **WHEN** the server starts with `--ecs-enable` AND no GeoIP database is loaded
- **THEN** one warning is logged stating ECS has no effect on view selection without GeoIP databases AND the server starts normally

#### Scenario: ECS warning repeats when a reload disables GeoIP

- **WHEN** a server running with `--ecs-enable` and loaded GeoIP databases completes a SIGHUP reload whose new configuration has no geo rules and no `geoip-directory`
- **THEN** the same ECS-without-GeoIP warning is logged once for that reload
