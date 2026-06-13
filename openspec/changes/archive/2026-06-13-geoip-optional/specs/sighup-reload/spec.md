## MODIFIED Requirements

### Requirement: GeoIP databases are reloaded on SIGHUP

The reload sequence SHALL apply the same conditional GeoIP requirement as startup, with `geoip-directory` counting as unset when absent or set to the empty string: when the reloaded named.conf sets `geoip-directory` to a non-empty path, the server SHALL re-open the GeoIP country and ASN mmdb files from that path; when `geoip-directory` is unset and at least one view's match-clients contains a country or ASN rule, the reload SHALL fail with an explicit configuration error naming the first such view with its source file path and line number — never a relative-path file-open error (this preserves the previous guarantee that an empty `geoip-directory` fails as an explicit configuration error mirroring the startup validation); when `geoip-directory` is unset and no view declares a country or ASN rule, the reload SHALL proceed with nil GeoIP handles. The reload-completion log SHALL carry a boolean field named `geoip_enabled` reporting whether the new state has GeoIP databases loaded. When new handles are opened, they SHALL be used when building the new server state. After the state swap, the superseded DB handles SHALL NOT be closed immediately — in-flight queries can still resolve views against the previous state, and closing an mmdb unmaps its memory (use-after-munmap is a fatal, unrecoverable crash). Superseded handles SHALL instead be retained and closed at the start of the next reload, or at process shutdown after the reload goroutine has been joined, whichever comes first (deferred-by-one-generation close); this lifecycle applies equally when the replacing generation is nil (GeoIP disabled by the reload). If either mmdb cannot be opened, the reload SHALL fail and the server SHALL retain the previous server state and the previous DB handles.

#### Scenario: GeoIP databases reloaded after mmdb file update

- **WHEN** the operator places updated mmdb files in the configured `geoip-directory` and sends SIGHUP
- **THEN** the server SHALL open new DB handles from the updated files and build the new state with them
- **THEN** subsequent DNS queries SHALL use the updated GeoIP data for view matching

#### Scenario: GeoIP reload failure preserves existing state

- **WHEN** the mmdb files are temporarily unavailable (removed or permission-denied) and SIGHUP is received
- **THEN** `reload()` SHALL return an error, the previous server state SHALL remain active, and the previous DB handles SHALL remain in use
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment

#### Scenario: GeoIP db_info gauge updated after successful reload

- **WHEN** a SIGHUP reload completes successfully with new mmdb files whose build epochs differ from the startup values
- **THEN** `shadowdns_geoip_db_info{database="country",build_time="<new-ISO8601>"}` and `shadowdns_geoip_db_info{database="asn",build_time="<new-ISO8601>"}` SHALL be set to 1
- **THEN** the gauge series carrying the previous `build_time` label values SHALL be deleted, so at most one `build_time` series exists per `database` label at any time

#### Scenario: Superseded GeoIP handles are closed deferred, never immediately after the swap

- **WHEN** a SIGHUP reload completes successfully and replaces the GeoIP handles
- **THEN** the superseded handles SHALL remain open and usable (in-flight queries holding the previous state snapshot can still perform lookups against them)
- **THEN** the superseded handles SHALL be closed at the start of the next reload, or at process shutdown after the reload goroutine has finished — and at no other time

##### Example: handle lifecycle across two reloads

| Event | gen-1 handles (startup) | gen-2 handles | gen-3 handles |
| ----- | ----------------------- | ------------- | ------------- |
| startup | current (open) | — | — |
| reload #1 succeeds | prev (open, deferred) | current (open) | — |
| reload #2 begins (step 0) | closed | current (open) | — |
| reload #2 succeeds | closed | prev (open, deferred) | current (open) |
| shutdown (after reload-goroutine join) | closed | closed | closed |

#### Scenario: Reload enables GeoIP on a server started without it

- **WHEN** a server started without GeoIP databases (no geo rules, no `geoip-directory`) is reloaded with a named.conf that adds `geoip-directory` and views with country rules
- **THEN** the reload SHALL open the mmdb files, build the new state with them, and subsequent queries SHALL use GeoIP view matching
- **THEN** the reload-completion log SHALL carry `geoip_enabled=true`
- **THEN** if either mmdb cannot be opened, the reload SHALL fail and the server SHALL keep serving with the previous (GeoIP-less) state

#### Scenario: Reload disables GeoIP on a server started with it

- **WHEN** a server running with loaded GeoIP databases is reloaded with a named.conf that removes every country/ASN rule and removes `geoip-directory`
- **THEN** the reload SHALL succeed, the new state SHALL resolve views with nil GeoIP handles, and the superseded handles SHALL follow the deferred-by-one-generation close lifecycle
- **THEN** the reload-completion log SHALL carry `geoip_enabled=false`

#### Scenario: Reload with geo rules but no geoip-directory fails keep-old

- **WHEN** the reloaded named.conf declares a view with `geoip country TH;` but no `geoip-directory`
- **THEN** `reload()` SHALL return an error naming that view with its source file and line, the previous server state SHALL remain active, and `shadowdns_reload_total{result="failure"}` SHALL increment
