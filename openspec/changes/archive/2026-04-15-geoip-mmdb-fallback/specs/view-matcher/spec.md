## MODIFIED Requirements

### Requirement: Fail startup when GeoIP databases are missing or unreadable

The view-matcher SHALL load a country mmdb and an ASN mmdb from the directory specified by `options.geoip-directory` at startup. For each database, the loader SHALL try candidate filenames in a fixed priority order and accept the first file that successfully opens and passes MaxMind mmdb validation:

- Country: `GeoIP2-Country.mmdb`, then `GeoLite2-Country.mmdb`.
- ASN: `GeoIP2-ASN.mmdb`, then `GeoLite2-ASN.mmdb`.

Country and ASN SHALL be resolved independently — the loader MUST NOT require both databases to use the same edition.

If every candidate filename for either database fails to open or validate, the server SHALL exit with a non-zero status and an error message listing every candidate path that was attempted and the reason each failed.

Upon successful load, the server SHALL log the full path of each opened mmdb so operators can identify the edition in use from the `path` field.

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
