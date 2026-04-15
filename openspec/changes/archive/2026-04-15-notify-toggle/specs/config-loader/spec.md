## MODIFIED Requirements

### Requirement: Parse named.conf options block

The config-loader SHALL parse the `options { ... }` block of `named.conf` and extract at minimum the following fields: `directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`, `notify`. Unknown options SHALL be ignored with a warning log entry rather than causing a parse failure.

The `notify` directive SHALL accept exactly two values: `yes` or `no` (case-insensitive). Any other value SHALL produce a parse error that includes the file path and line number. When the `notify` directive is absent from the options block, the parsed options record SHALL indicate "not set" in a form distinguishable from both `yes` and `no` (so that downstream precedence logic can apply a default).

#### Scenario: Standard options block loads successfully

- **WHEN** `named.conf` contains `options { directory "/etc/namedb"; geoip-directory "/usr/local/share/GeoIP/"; listen-on { any; }; recursion no; };`
- **THEN** the loader produces an options record with `directory="/etc/namedb"`, `geoipDirectory="/usr/local/share/GeoIP/"`, `listenOn=[any]`, `recursion=false`

#### Scenario: Unknown option emits warning but does not fail

- **WHEN** `named.conf` contains an option key that is not in the supported list
- **THEN** the loader logs a warning including the option name and line number AND continues parsing

#### Scenario: Malformed options block fails with actionable error

- **WHEN** `named.conf` has an unmatched `{` or missing `;` inside the options block
- **THEN** the loader returns an error that includes the file path and the line number of the first unparseable token

#### Scenario: notify yes parses to enabled state

- **WHEN** `named.conf` contains `options { notify yes; };`
- **THEN** the loader produces an options record whose `notify` field indicates "set to true"

#### Scenario: notify no parses to disabled state

- **WHEN** `named.conf` contains `options { notify no; };`
- **THEN** the loader produces an options record whose `notify` field indicates "set to false"

#### Scenario: notify absent parses to not-set state

- **WHEN** `named.conf` options block omits the `notify` directive
- **THEN** the loader produces an options record whose `notify` field indicates "not set" (distinguishable from both true and false)

#### Scenario: invalid notify value fails with actionable error

- **WHEN** `named.conf` contains `options { notify bogus; };`
- **THEN** the loader returns an error that includes the file path, the line number, and the invalid value
