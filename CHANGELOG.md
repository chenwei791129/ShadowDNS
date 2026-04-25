# Changelog

## Unreleased

### Features

* **cli:** add opt-in `--pprof-enable` flag that exposes Go pprof endpoints on the metrics HTTP server under `/debug/pprof/` (disabled by default)
* **cli:** migrate command-line parsing to cobra. All flags now use POSIX-style double dashes (e.g. `--named-conf`, `--listen`, `--reload-verify`); `--version` gains a `-v` short alias shown as `-v, --version` in `--help`. The former `-reload` flag is replaced by the `shadowdns reload` subcommand, which accepts only `--named-conf`. Existing single-dash flag invocations (`-named-conf`, `-listen`, `-reload`, etc.) are no longer recognized — update systemd units, scripts, and wrappers accordingly.

## [0.12.4](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.3...v0.12.4) (2026-04-25)


### Bug Fixes

* adjust ephemeral ttl to 0 to try fix cert issue ([9fb95cf](https://github.com/chenwei791129/ShadowDNS/commit/9fb95cf90ab3443be5ff329dc847a5647c77f1c8))

## [0.12.3](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.2...v0.12.3) (2026-04-24)


### Bug Fixes

* **handler:** enforce UDP budget via Pack() and enable name compression ([#21](https://github.com/chenwei791129/ShadowDNS/issues/21)) ([bda64fc](https://github.com/chenwei791129/ShadowDNS/commit/bda64fc984446309406e8980fdf68bc63123e630))

## [0.12.2](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.1...v0.12.2) (2026-04-24)


### Bug Fixes

* **ephemeral:** use fixed 30s TTL on ephemeral TXT DNS responses ([02a4362](https://github.com/chenwei791129/ShadowDNS/commit/02a4362c293ae14ffd2f6d27d7077a4fe3a41ed7))

## [0.12.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.0...v0.12.1) (2026-04-23)


### Bug Fixes

* **ci:** generate shell completions before nfpm packaging ([1c6f38e](https://github.com/chenwei791129/ShadowDNS/commit/1c6f38e57e1cb5dc1508a3515e764cc6f27c635e))

## [0.12.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.11.1...v0.12.0) (2026-04-23)


### Features

* **packaging:** generate shell completion in deb package ([fcc79a0](https://github.com/chenwei791129/ShadowDNS/commit/fcc79a01aa1c5dcfa9d577a1f8ff27c314b39564))

## [0.11.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.11.0...v0.11.1) (2026-04-23)


### Bug Fixes

* **ephemeral-api:** reject PUT for FQDNs outside loaded zones ([8aca32f](https://github.com/chenwei791129/ShadowDNS/commit/8aca32f7088fa583052513978aab12337f9bcce9))

## [0.11.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.10.0...v0.11.0) (2026-04-22)


### Features

* **config:** flip aliases schema to root→[backups] ([09041e7](https://github.com/chenwei791129/ShadowDNS/commit/09041e7da9d478175b56015de5b342e533066251))
* **server:** serve ephemeral TXT ahead of RFC 1034 CNAME fallback for TXT queries ([7674e36](https://github.com/chenwei791129/ShadowDNS/commit/7674e361c47289b1cc3583ba62403c8671297bcb))

## [0.10.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.9.1...v0.10.0) (2026-04-22)


### Features

* **dns:** exact-match ephemeral TXT takes precedence over zone wildcard ([2b2d214](https://github.com/chenwei791129/ShadowDNS/commit/2b2d2145f7af9da2981555f257fc471718b60728))
* ephemeral TXT HTTP API with multi-value support and unified config ([c63b891](https://github.com/chenwei791129/ShadowDNS/commit/c63b8917fa90a0c7c866c372b93cd24ec5f9ee5c))
* **ephemeral:** add per-value DELETE and 255-byte value cap ([d571578](https://github.com/chenwei791129/ShadowDNS/commit/d571578363d4114daf987337f76721544ab78dcc))


### Bug Fixes

* **lint:** handle errcheck for Body.Close and f.Close ([bedeb4c](https://github.com/chenwei791129/ShadowDNS/commit/bedeb4cca88f629fb0a7ad40a4ad78c9f9b39e55))

## [0.9.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.9.0...v0.9.1) (2026-04-21)


### Bug Fixes

* **scripts:** recurse into subdirectories when copying zone testdata ([2a4d8c6](https://github.com/chenwei791129/ShadowDNS/commit/2a4d8c688eef54b95135629b8bcda8c79c30e998))

## [0.9.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.8.0...v0.9.0) (2026-04-20)


### Features

* **cli:** add opt-in pprof endpoint on metrics HTTP server ([556b968](https://github.com/chenwei791129/ShadowDNS/commit/556b9688ff2aee0df4746c7689a7d2a563eb527d))


### Bug Fixes

* **cli:** satisfy errcheck on flag.Usage fmt.Fprintln calls ([e1cf836](https://github.com/chenwei791129/ShadowDNS/commit/e1cf83633e6d485946d0f3f9f384543d482f2e22))

## [0.8.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.7.0...v0.8.0) (2026-04-17)


### Features

* **server:** diff-based zone reload with fingerprint reuse ([10c1dbc](https://github.com/chenwei791129/ShadowDNS/commit/10c1dbcbe1c53322da2616b68ec59c239595e9d9))


### Bug Fixes

* **test:** eliminate flaky pointer lookup in reload diff test ([6c8f16f](https://github.com/chenwei791129/ShadowDNS/commit/6c8f16fb67f73f996ca19b7ea8b2746656eb8e22))

## [0.7.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.6.0...v0.7.0) (2026-04-16)


### Features

* **dns:** follow in-zone CNAME targets per RFC 1034 §3.6.2 ([0931d4b](https://github.com/chenwei791129/ShadowDNS/commit/0931d4bfa1b8432e1b0f1b848ae577d021f4a494))

## [0.6.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.5.0...v0.6.0) (2026-04-16)


### Features

* **dns:** match wildcard records per RFC 4592 ([4265738](https://github.com/chenwei791129/ShadowDNS/commit/42657384b856f5efe5d5733da572fed2a3fd5c2f))


### Bug Fixes

* **config:** add pid-file option for shadowdns service ([8997a6d](https://github.com/chenwei791129/ShadowDNS/commit/8997a6d5db195a0129ac35f8a897f1ece7a79772))
* **dns:** synthesize CNAME response per RFC 1034 §3.6.2 ([1b62f1e](https://github.com/chenwei791129/ShadowDNS/commit/1b62f1e926846c858e044d132af38a5a079c448d))

## [0.5.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.4.0...v0.5.0) (2026-04-15)


### Features

* **notify:** add -no-notify flag and options.notify directive ([41f30c8](https://github.com/chenwei791129/ShadowDNS/commit/41f30c8bcf283da713d74c78605f10090c6d0eb9))

## [0.4.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.3.0...v0.4.0) (2026-04-15)


### Features

* **server:** honor named.conf listen-on with per-address binding ([d027f96](https://github.com/chenwei791129/ShadowDNS/commit/d027f963ec2a5afc25af467846cecd03d6303f2f))

## [0.3.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.2.1...v0.3.0) (2026-04-15)


### Features

* **view:** support GeoIP2/GeoLite2 mmdb fallback chain ([caa9aa0](https://github.com/chenwei791129/ShadowDNS/commit/caa9aa08131896871e21b451fe5f15e44d5ad69f))
* **zone:** support BIND-compatible quoted $INCLUDE syntax ([397182e](https://github.com/chenwei791129/ShadowDNS/commit/397182e98c1a073a260dd7cab393b95ce4c11329))


### Bug Fixes

* **smoke:** copy entire master/ tree recursively ([f17e14e](https://github.com/chenwei791129/ShadowDNS/commit/f17e14eac0a189a057a33a60811ef5ed185a5f86))

## [0.2.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.2.0...v0.2.1) (2026-04-14)


### Bug Fixes

* pass release tag to nfpm so .deb version matches the release ([a496a7f](https://github.com/chenwei791129/ShadowDNS/commit/a496a7f621fe6e66a6e7cfa398b03cab9f8c711c))

## [0.2.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.1.0...v0.2.0) (2026-04-14)


### Features

* add Prometheus /metrics endpoint with 8 DNS metrics ([6920b02](https://github.com/chenwei791129/ShadowDNS/commit/6920b022760981c67c60b288831457c2e35be27c))
