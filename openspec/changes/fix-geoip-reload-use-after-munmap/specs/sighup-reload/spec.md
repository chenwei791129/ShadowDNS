## MODIFIED Requirements

### Requirement: GeoIP databases are reloaded on SIGHUP

The reload sequence SHALL apply the same conditional GeoIP requirement as startup, with `geoip-directory` counting as unset when absent or set to the empty string: when the reloaded named.conf sets `geoip-directory` to a non-empty path, the server SHALL re-open the GeoIP country and ASN mmdb files from that path; when `geoip-directory` is unset and at least one view's match-clients contains a country or ASN rule, the reload SHALL fail with an explicit configuration error naming the first such view with its source file path and line number — never a relative-path file-open error (this preserves the previous guarantee that an empty `geoip-directory` fails as an explicit configuration error mirroring the startup validation); when `geoip-directory` is unset and no view declares a country or ASN rule, the reload SHALL proceed with nil GeoIP handles. The reload-completion log SHALL carry a boolean field named `geoip_enabled` reporting whether the new state has GeoIP databases loaded. When new handles are opened, they SHALL be used when building the new server state.

After the state swap, the reload path SHALL NOT close or unmap the superseded DB handles — neither immediately after the swap nor at the start of any later reload. In-flight queries can still resolve views against the previous state snapshot, and unmapping an mmdb frees mapped (non-Go-heap) memory, so unmapping one still referenced by a query is a fatal, unrecoverable use-after-munmap crash. Reclamation SHALL instead be reachability-driven: a superseded generation's mmdb SHALL NOT be unmapped while the generation is still reachable from any loaded server-state snapshot — that is, not until the last in-flight query holding that generation has completed. This guarantee SHALL hold under any reload cadence, including two or more SIGHUP reloads arriving in immediate succession. The reload path SHALL NOT retain superseded generations in a deferred-close slot; after the swap it SHALL drop its reference to the superseded generation and close nothing. Once a generation is no longer reachable, its mmdb mapping is released by the mmdb reader's own garbage-collector cleanup, or, failing that, reclaimed by the operating system at process exit. This lifecycle applies equally when the replacing generation is nil (GeoIP disabled by the reload): the outgoing non-nil generation reference is dropped, not parked. Process shutdown SHALL NOT close the current generation either: shutdown joins do not cover every query (a DoH handler that outruns the bounded HTTP listener drain keeps executing after the run loop returns), so an explicit shutdown close could unmap a generation a straggler query is still reading — the same crash class relocated to shutdown. No path in the process SHALL close an installed generation; the current generation is reclaimed by the operating system at process exit (or by the reader's cleanup if it becomes unreachable first). If either mmdb cannot be opened, the reload SHALL fail and the server SHALL retain the previous server state and the previous DB handles.

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

#### Scenario: Superseded GeoIP handles are never closed by the reload path

- **WHEN** a SIGHUP reload completes successfully and replaces the GeoIP handles
- **THEN** the reload path SHALL NOT close or unmap the superseded handles at any point (not after the swap and not at the start of a later reload)
- **THEN** the superseded handles SHALL remain open and usable while any in-flight query still holds the previous state snapshot, and their mapping SHALL be released only after the generation is unreachable — by the mmdb reader's garbage-collector cleanup, or by the operating system at process exit

#### Scenario: Rapid double SIGHUP does not unmap a generation still held by an in-flight query

- **WHEN** a query loads a state snapshot referencing GeoIP generation N and is still performing a country/ASN lookup while two SIGHUP reloads (to generation N+1 then N+2) complete in immediate succession
- **THEN** generation N's mmdb SHALL remain mapped for the full duration of that query and SHALL NOT be unmapped by either reload
- **THEN** the lookup SHALL complete without a use-after-munmap crash or data race

##### Example: handle lifecycle is governed by reachability, not reload count

| Event | gen-1 handles (startup) | gen-2 handles | gen-3 handles |
| ----- | ----------------------- | ------------- | ------------- |
| startup | current (open) | — | — |
| reload #1 succeeds | reference dropped; mapped while any in-flight query holds gen-1 | current (open) | — |
| reload #2 succeeds (immediately after) | still mapped if a query loaded before reload #1 is in flight; reload closes nothing | reference dropped; mapped while any in-flight query holds gen-2 | current (open) |
| no in-flight query holds gen-1 | unmapped by the reader's GC cleanup once unreachable | mapped | current (open) |
| shutdown | already unmapped, or reclaimed by the OS at exit | reclaimed once unreachable, no later than the OS reclaiming it at exit | never closed by the process; reclaimed by the OS at exit |

#### Scenario: Process shutdown never closes GeoIP handles

- **WHEN** the server shuts down while a query that outlived the bounded DoH listener drain is still performing a country/ASN lookup against the current generation
- **THEN** no shutdown path SHALL close or unmap any GeoIP generation; the lookup SHALL complete against the still-mapped mmdb and the mapping SHALL be reclaimed by the operating system at process exit

#### Scenario: Reload enables GeoIP on a server started without it

- **WHEN** a server started without GeoIP databases (no geo rules, no `geoip-directory`) is reloaded with a named.conf that adds `geoip-directory` and views with country rules
- **THEN** the reload SHALL open the mmdb files, build the new state with them, and subsequent queries SHALL use GeoIP view matching
- **THEN** the reload-completion log SHALL carry `geoip_enabled=true`
- **THEN** if either mmdb cannot be opened, the reload SHALL fail and the server SHALL keep serving with the previous (GeoIP-less) state

#### Scenario: Reload disables GeoIP on a server started with it

- **WHEN** a server running with loaded GeoIP databases is reloaded with a named.conf that removes every country/ASN rule and removes `geoip-directory`
- **THEN** the reload SHALL succeed, the new state SHALL resolve views with nil GeoIP handles, and the superseded handles SHALL follow the reachability-driven lifecycle (reference dropped, closed by no one in the reload path, reclaimed once unreachable)
- **THEN** the reload-completion log SHALL carry `geoip_enabled=false`

#### Scenario: Reload with geo rules but no geoip-directory fails keep-old

- **WHEN** the reloaded named.conf declares a view with `geoip country TH;` but no `geoip-directory`
- **THEN** `reload()` SHALL return an error naming that view with its source file and line, the previous server state SHALL remain active, and `shadowdns_reload_total{result="failure"}` SHALL increment

### Requirement: All fallible reload steps precede the state swap

The reload sequence SHALL perform every step that can fail — named.conf parse, ShadowDNS config load, GeoIP database open, server state build, rate-limiter construction, and query-log sink open — before `SwapState` is called. Steps executed after `SwapState` (installing the new rate limiter, query-log logger, and GeoIP handles; closing the superseded query-log sink; dropping the reference to the superseded GeoIP generation without closing it — its mmdb mapping is released by the mmdb reader's own garbage-collector cleanup once no in-flight query can reach it, or by the OS at process exit, never synchronously in the reload path; clearing the ephemeral store; recording metrics) SHALL be infallible installation steps that cannot abort the reload. This guarantees a failed reload never leaves a partially applied configuration.

#### Scenario: Any fallible step failure preserves the full previous runtime state

- **WHEN** any fallible reload step returns an error (parse, GeoIP open, state build, limiter construction, or query-log sink open)
- **THEN** the previous server state SHALL remain active in full: zone data, view matching, rate limiter, query-log logger, and GeoIP handles are all unchanged
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment and `reload()` SHALL return a non-nil error
