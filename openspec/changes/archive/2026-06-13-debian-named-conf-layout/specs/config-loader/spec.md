## ADDED Requirements

### Requirement: Honor options block from included files

The config-loader SHALL apply an `options { ... }` block regardless of which file in the include tree declares it: a block appearing in a file reached via an `include "...";` directive (for example the Debian-idiomatic `named.conf.options`) SHALL populate the loaded configuration's options exactly as if the block were written inline in the root `named.conf`. This applies to every options field (`directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `rate-limit`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`, `notify`).

Because options fields consumed at parse time (notably `directory` for relative zone-`file` resolution) read the options state accumulated so far, an `options` block intended to govern later views/zones SHALL be included before those views/zones in declaration order (as in the Debian layout, where `named.conf` includes `named.conf.options` before `named.conf.local`).

When more than one `options { ... }` block is encountered across the include tree, the last-parsed block SHALL take effect (BIND permits a single `options` statement; ShadowDNS tolerates duplicates rather than failing) and the loader SHALL emit a warning naming the file and line of the additional block.

#### Scenario: Options block in an included file is honored

- **WHEN** `named.conf` contains only `include "named.conf.options";` and `include "named.conf.local";`, and `named.conf.options` contains `options { directory "/etc/bind"; geoip-directory "/etc/bind/geoip"; listen-on { 192.0.2.1; }; };`
- **THEN** the loaded configuration's options record has `directory="/etc/bind"`, `geoipDirectory="/etc/bind/geoip"`, and `listenOn=[192.0.2.1]` (not empty/dropped)

#### Scenario: Geo views in an included view file start when geoip-directory is in the included options file

- **WHEN** `named.conf` includes `named.conf.options` (declaring `geoip-directory`) before `named.conf.local` (declaring a view with a `geoip country` match-clients rule)
- **THEN** startup GeoIP loading reads the populated `geoip-directory` and does not fail with "geoip-directory is not set"

#### Scenario: Multiple options blocks across the include tree warn and last wins

- **WHEN** the root `named.conf` declares `options { directory "/first"; };` and then includes a file declaring `options { directory "/second"; };`
- **THEN** the loaded configuration's options record has `directory="/second"` AND the loader emits exactly one warning naming the file and line of the second block
