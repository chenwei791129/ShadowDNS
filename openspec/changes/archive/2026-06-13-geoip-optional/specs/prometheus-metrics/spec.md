## MODIFIED Requirements

### Requirement: Expose GeoIP database metadata

When GeoIP databases are loaded, the system SHALL expose a gauge metric `shadowdns_geoip_db_info` with labels `database` and `build_time`, set to the constant value 1. The `database` label SHALL be `country` or `asn`. The `build_time` label SHALL contain the database build timestamp formatted as ISO 8601 (UTC). The metadata SHALL be read from `maxminddb.Reader.Metadata.BuildEpoch`. When no GeoIP database is loaded, the system SHALL expose no `shadowdns_geoip_db_info` series. The metric setter SHALL treat each invocation's database set as the complete desired set: any previously exposed series whose `database` label is absent from the current set SHALL be deleted (so a reload that disables GeoIP removes the stale series), and for a database present in the set with a new `build_time`, the series carrying the previous `build_time` SHALL be deleted, so at most one `build_time` series exists per `database` label at any time.

#### Scenario: GeoIP country database info

- **WHEN** the metrics endpoint is scraped and the loaded GeoLite2-Country database was built at Unix epoch 1700000000
- **THEN** `shadowdns_geoip_db_info{database="country",build_time="2023-11-14T22:13:20Z"}` has value 1

#### Scenario: GeoIP ASN database info

- **WHEN** an ASN database is loaded and the metrics endpoint is scraped
- **THEN** `shadowdns_geoip_db_info{database="asn",build_time="<ISO8601>"}` has value 1, where `<ISO8601>` is the loaded database's build epoch formatted as ISO 8601 UTC

#### Scenario: No series exposed when GeoIP is not loaded

- **WHEN** the server runs without GeoIP databases (no geo rules, no `geoip-directory`) and the metrics endpoint is scraped
- **THEN** the response contains no `shadowdns_geoip_db_info` series

#### Scenario: Series deleted after a reload disables GeoIP

- **WHEN** a server running with loaded GeoIP databases completes a reload that removes GeoIP (no geo rules, no `geoip-directory`)
- **THEN** the `shadowdns_geoip_db_info` series for both `country` and `asn` SHALL be deleted from the metrics endpoint
