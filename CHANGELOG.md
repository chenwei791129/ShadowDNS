# Changelog

## [0.24.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.23.0...v0.24.0) (2026-06-28)


### Features

* **doh:** harden ACME HTTP-01 listener with nginx return 444 semantics ([d24b2ce](https://github.com/chenwei791129/ShadowDNS/commit/d24b2ce97b43ecc5b8eecbd9c3143dd7627f4113))
* **doh:** serve application/dns-json over GET ([fff367b](https://github.com/chenwei791129/ShadowDNS/commit/fff367bacf10edb8aac1f532ecd5acee3bcbe3f4))

## [0.23.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.22.0...v0.23.0) (2026-06-27)


### Features

* **doh:** add DNS-over-HTTPS endpoint (RFC 8484) ([2e38bc9](https://github.com/chenwei791129/ShadowDNS/commit/2e38bc930ef5842474f18a29d61b031e88993310))
* **doh:** persist ACME account key across restarts ([7420b6e](https://github.com/chenwei791129/ShadowDNS/commit/7420b6e7f4708f54dc2e896e139722827acf997d))


### Bug Fixes

* **deps:** bump go-git, x/crypto, x/net to clear Trivy HIGH CVEs ([96d12ee](https://github.com/chenwei791129/ShadowDNS/commit/96d12ee049dbde8d9512ccfb8af310d5d3ece39e))
* **docs:** drop non-existent acme.email field from DoH guide ([22da067](https://github.com/chenwei791129/ShadowDNS/commit/22da067718fb2a973c73090b29cb14940acf6f76))

## [0.22.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.21.0...v0.22.0) (2026-06-14)


### Features

* **grafana:** add ready-to-import ShadowDNS overview dashboard ([cf8f79d](https://github.com/chenwei791129/ShadowDNS/commit/cf8f79d0398831a200d821061d3cd9d665ac7df8))
* **metrics:** expose Go/process collectors and ECS + view-selection metrics ([f99d9f9](https://github.com/chenwei791129/ShadowDNS/commit/f99d9f94817b8e023febaa72376192c0d29d2a03))


### Bug Fixes

* **grafana:** show value-only stats without background sparklines ([1c9140f](https://github.com/chenwei791129/ShadowDNS/commit/1c9140fdde776701bca4c46c7ee6119e07994461))

## [0.21.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.20.0...v0.21.0) (2026-06-14)


### Features

* **config:** honor options{} block declared in included files ([4c2cb47](https://github.com/chenwei791129/ShadowDNS/commit/4c2cb47e2ff28fe9060ba1cb99dba477632dd033))
* dedup byte-identical RRs within an RRset at zone load ([81df51a](https://github.com/chenwei791129/ShadowDNS/commit/81df51ac39b7701aedd3f1a91f41b766e07a62ed))
* make GeoIP database loading conditional ([dbd79bf](https://github.com/chenwei791129/ShadowDNS/commit/dbd79bf29652c6a84b9f7152595b6da33c96d7f6))
* **packaging:** split named.conf example into Debian three-file layout ([cd2858d](https://github.com/chenwei791129/ShadowDNS/commit/cd2858d67c506e6e85eaf60845d555c1f6c0fc15))
* ship a viewless BIND-style example config in the .deb ([2bb8489](https://github.com/chenwei791129/ShadowDNS/commit/2bb848957ffd7cabc5c494bbcd597972e1575c9a))
* support named ACLs, negation, nested groups, and built-in ACLs in match-clients ([6057cf2](https://github.com/chenwei791129/ShadowDNS/commit/6057cf24ff69641c5f41265a9b859d98708d7238))
* support viewless named.conf via implicit _default view ([9f73695](https://github.com/chenwei791129/ShadowDNS/commit/9f73695c4a637d8325847b7c60fa49b52fe0e8ef))
* tolerate unrecognized BIND named.conf constructs ([5cbd460](https://github.com/chenwei791129/ShadowDNS/commit/5cbd4602971d802fb0b69391a8acd10495ac8af1))


### Bug Fixes

* **config:** balance acl braces by token so comments can't truncate bodies ([dd48637](https://github.com/chenwei791129/ShadowDNS/commit/dd486377033384fe02adb374efe356ea901285c6))
* **config:** harden named.conf match-clients and directive handling ([30fdb08](https://github.com/chenwei791129/ShadowDNS/commit/30fdb085e6747a6e77c32de8ec4e906402219d67))
* **config:** reject negated reference to an empty acl (fail-closed) ([e8fb4ea](https://github.com/chenwei791129/ShadowDNS/commit/e8fb4ea2b35fddfa0f69acf7dad253abbeac897c))
* **docs:** resolve broken logo path on the /zh/ home page ([2eb9aa9](https://github.com/chenwei791129/ShadowDNS/commit/2eb9aa99062b9d61af642b5cbf7d012784ac0106))
* harden match-clients ACL evaluation and named.conf loading ([d567925](https://github.com/chenwei791129/ShadowDNS/commit/d567925fbc60f9ff8f7c4ea4b968cfa81738b727))
* **view:** correct localnets prefix for IPv4-mapped interface masks ([a766f92](https://github.com/chenwei791129/ShadowDNS/commit/a766f9256fdda033de6f1bba68e6970693c72182))

## [0.20.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.19.0...v0.20.0) (2026-06-12)


### Features

* **dns:** add per-alias-group CNAME chain collapsing behind collapse_cname_chain ([a79e486](https://github.com/chenwei791129/ShadowDNS/commit/a79e486aaace606ef1346c8050a746dd539785ad))
* **dns:** add RFC 7871 EDNS Client Subnet support behind --ecs-enable ([cbc62b4](https://github.com/chenwei791129/ShadowDNS/commit/cbc62b465f1b09c6c67831626c808dbb987366b1))

## [0.19.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.18.0...v0.19.0) (2026-06-11)


### Features

* **metrics:** use DNS-optimised latency histogram buckets ([7814af1](https://github.com/chenwei791129/ShadowDNS/commit/7814af12989e199ea91e3ee1a4d097b38071b6d9))
* **reload:** reload GeoIP, rate limiter, and query log on SIGHUP with reload metrics ([adefe47](https://github.com/chenwei791129/ShadowDNS/commit/adefe475352ddb7a13dfda956a87e0dc75c9d13c))

## [0.18.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.17.0...v0.18.0) (2026-06-09)


### Features

* **ratelimit:** add BIND-compatible response rate limiting ([66040c8](https://github.com/chenwei791129/ShadowDNS/commit/66040c8ed97866874a8b88ed9e859420cbcbe3f8))
* **server:** add DNS Cookies and EDNS0 OPT echo ([87320ad](https://github.com/chenwei791129/ShadowDNS/commit/87320ad3fa73b9c0d8110db701ea00f441e19d21))
* **server:** bind IPv6 listeners from named.conf listen-on-v6 ([c2ad9e3](https://github.com/chenwei791129/ShadowDNS/commit/c2ad9e33125165d1849f45d8ba862b11968baafe))

## [0.17.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.16.1...v0.17.0) (2026-06-07)


### Features

* **notify:** resolve NOTIFY targets from in-zone glue records ([f35208a](https://github.com/chenwei791129/ShadowDNS/commit/f35208a7ac83d711eda37d6522f2d5636f36c3cb))
* **querylog:** add BIND9-compatible query logging from named.conf logging{} ([e5dcfac](https://github.com/chenwei791129/ShadowDNS/commit/e5dcfac94e43e2a9c08380c66733e3a96854fcb0))

## [0.16.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.16.0...v0.16.1) (2026-05-09)


### Performance Improvements

* **dnsutil:** eliminate string concat in IsInZone hot path ([dac6f43](https://github.com/chenwei791129/ShadowDNS/commit/dac6f43c258f75ed00ba4dcd096add0c3ea4b92f))
* **zone:** eliminate "."+origin concat in LookupWildcard/FollowCNAME ([f7c935b](https://github.com/chenwei791129/ShadowDNS/commit/f7c935b99e935cbe9e9fac238483697d32c4ba5a))

## [0.16.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.15.0...v0.16.0) (2026-05-05)


### Features

* prune backup zones without declared root + summarize discard log ([f4f5ffc](https://github.com/chenwei791129/ShadowDNS/commit/f4f5ffc5145162a49d2aff567512d9fc88ec5b7f))


### Bug Fixes

* **smoke:** use unified aliases mapping form ([319b684](https://github.com/chenwei791129/ShadowDNS/commit/319b6843a9b2b6f0fc951f84794cb1351954cf63))

## [0.15.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.14.0...v0.15.0) (2026-05-05)


### Features

* **logging:** add file-backed sink with SIGUSR1 reopen ([85344b8](https://github.com/chenwei791129/ShadowDNS/commit/85344b8976031be773518301f54b83c79bdd3e4a))
* **packaging:** install logrotate config and route daemon log to file ([0674a48](https://github.com/chenwei791129/ShadowDNS/commit/0674a48eda446ec8b226bda4568267a9a7bd4c6d))

## [0.14.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.13.0...v0.14.0) (2026-05-04)


### Features

* **prune-backup:** add offline CLI to prune redundant backup records ([#26](https://github.com/chenwei791129/ShadowDNS/issues/26)) ([b475918](https://github.com/chenwei791129/ShadowDNS/commit/b47591883936a292eb3c8ef7c428dece2bc375ab))
* **prune-backup:** stream pairs to bound peak memory ([e4ed742](https://github.com/chenwei791129/ShadowDNS/commit/e4ed742d061e123e33533eb196839ae2c9be61f8))


### Bug Fixes

* **test:** replace flaky time.Sleep sync points with explicit channels ([#28](https://github.com/chenwei791129/ShadowDNS/issues/28)) ([6aa8e9f](https://github.com/chenwei791129/ShadowDNS/commit/6aa8e9f6c666ec0940934086e669b97df22d7da7))
* **zone:** suppress zero-signal logs for expected backup-zone drops ([#29](https://github.com/chenwei791129/ShadowDNS/issues/29)) ([3bd2112](https://github.com/chenwei791129/ShadowDNS/commit/3bd2112b4f5497e5b5e908b80c67230423a6a638))

## [0.13.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.4...v0.13.0) (2026-04-29)


### Features

* **dns:** preserve query and zone-file case in DNS responses ([fa3c1da](https://github.com/chenwei791129/ShadowDNS/commit/fa3c1dacac4e66f8143a6ca5fc1d2ec763fc3ee6))


### Bug Fixes

* **alias:** rewrite root labels in mid-RDATA when opt-in flag is set ([9182659](https://github.com/chenwei791129/ShadowDNS/commit/9182659d05ac723ae213f417df3cd70ae19e3c66))

## [0.12.4](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.3...v0.12.4) (2026-04-25)


### Bug Fixes

* adjust ephemeral ttl to 0 to try fix cert issue ([b928b73](https://github.com/chenwei791129/ShadowDNS/commit/b928b73ea412a44cd5634679af91a86652b4db5d))

## [0.12.3](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.2...v0.12.3) (2026-04-24)


### Bug Fixes

* **handler:** enforce UDP budget via Pack() and enable name compression ([#21](https://github.com/chenwei791129/ShadowDNS/issues/21)) ([bc801b1](https://github.com/chenwei791129/ShadowDNS/commit/bc801b1de75a02016fe5c9a32d92e6c2b511cc27))

## [0.12.2](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.1...v0.12.2) (2026-04-24)


### Bug Fixes

* **ephemeral:** use fixed 30s TTL on ephemeral TXT DNS responses ([d627e28](https://github.com/chenwei791129/ShadowDNS/commit/d627e28ecfd406c08734774dfa958225c1d3e29e))

## [0.12.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.12.0...v0.12.1) (2026-04-23)


### Bug Fixes

* **ci:** generate shell completions before nfpm packaging ([64fa915](https://github.com/chenwei791129/ShadowDNS/commit/64fa915be00db779482594ba6f70e52d84d60d5d))

## [0.12.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.11.1...v0.12.0) (2026-04-23)


### Features

* **packaging:** generate shell completion in deb package ([1e6ce62](https://github.com/chenwei791129/ShadowDNS/commit/1e6ce6264534be1defad5314e35f8ba71918c183))

## [0.11.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.11.0...v0.11.1) (2026-04-23)


### Bug Fixes

* **ephemeral-api:** reject PUT for FQDNs outside loaded zones ([fe52ad5](https://github.com/chenwei791129/ShadowDNS/commit/fe52ad50cd817f25d9739aa5eaf4e9581a739e43))

## [0.11.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.10.0...v0.11.0) (2026-04-22)


### Features

* **config:** flip aliases schema to root→[backups] ([7ea23e5](https://github.com/chenwei791129/ShadowDNS/commit/7ea23e54661062bae65719cbdca6aa47ce5fb858))
* **server:** serve ephemeral TXT ahead of RFC 1034 CNAME fallback for TXT queries ([831d154](https://github.com/chenwei791129/ShadowDNS/commit/831d154834d665c8a77653e891ebdc9be9f65a1d))

## [0.10.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.9.1...v0.10.0) (2026-04-22)


### Features

* **dns:** exact-match ephemeral TXT takes precedence over zone wildcard ([69e3bcd](https://github.com/chenwei791129/ShadowDNS/commit/69e3bcda47576c6881468884fad60ec6359c0e0c))
* ephemeral TXT HTTP API with multi-value support and unified config ([6800b0a](https://github.com/chenwei791129/ShadowDNS/commit/6800b0a49b7762e9dc9c1dea25e2937d6adb94b0))
* **ephemeral:** add per-value DELETE and 255-byte value cap ([25135e1](https://github.com/chenwei791129/ShadowDNS/commit/25135e17d39323fbf811901829451817bd5e36d8))


### Bug Fixes

* **lint:** handle errcheck for Body.Close and f.Close ([5725353](https://github.com/chenwei791129/ShadowDNS/commit/572535390383d2b48b7829de854e5e553bf0a475))

## [0.9.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.9.0...v0.9.1) (2026-04-21)


### Bug Fixes

* **scripts:** recurse into subdirectories when copying zone testdata ([2a454e5](https://github.com/chenwei791129/ShadowDNS/commit/2a454e56aa3044e292de7327aef6b35379ae52a2))

## [0.9.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.8.0...v0.9.0) (2026-04-20)


### Features

* **cli:** add opt-in pprof endpoint on metrics HTTP server ([0d24b9f](https://github.com/chenwei791129/ShadowDNS/commit/0d24b9f12b2ad013ef148f7a4d7043eb695aab72))


### Bug Fixes

* **cli:** satisfy errcheck on flag.Usage fmt.Fprintln calls ([ec6a11d](https://github.com/chenwei791129/ShadowDNS/commit/ec6a11db3af547c8200eca9cb7b54582d5659cec))

## [0.8.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.7.0...v0.8.0) (2026-04-17)


### Features

* **server:** diff-based zone reload with fingerprint reuse ([68277b2](https://github.com/chenwei791129/ShadowDNS/commit/68277b2183c5f706d221054c493b55b26ca3eb97))


### Bug Fixes

* **test:** eliminate flaky pointer lookup in reload diff test ([06aac8b](https://github.com/chenwei791129/ShadowDNS/commit/06aac8b502cdd271b7f1df361c407bbf88ba4cdf))

## [0.7.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.6.0...v0.7.0) (2026-04-16)


### Features

* **dns:** follow in-zone CNAME targets per RFC 1034 §3.6.2 ([d624eef](https://github.com/chenwei791129/ShadowDNS/commit/d624eef0cf7e9ce736621e3fd7bfa71686b51506))

## [0.6.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.5.0...v0.6.0) (2026-04-16)


### Features

* **dns:** match wildcard records per RFC 4592 ([1f1d695](https://github.com/chenwei791129/ShadowDNS/commit/1f1d695a0e119ae7125acad1c143083e0831d3f7))


### Bug Fixes

* **config:** add pid-file option for shadowdns service ([f3a8b86](https://github.com/chenwei791129/ShadowDNS/commit/f3a8b864fa1e129b6579623d70a9354006e12d68))
* **dns:** synthesize CNAME response per RFC 1034 §3.6.2 ([17a507d](https://github.com/chenwei791129/ShadowDNS/commit/17a507dedf7be71a1d9fdf0a8dfe133e56a22dfc))

## [0.5.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.4.0...v0.5.0) (2026-04-15)


### Features

* **notify:** add -no-notify flag and options.notify directive ([43d926b](https://github.com/chenwei791129/ShadowDNS/commit/43d926b3f66d53655182b694a263ad358ef4ff75))

## [0.4.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.3.0...v0.4.0) (2026-04-15)


### Features

* **server:** honor named.conf listen-on with per-address binding ([f1607e1](https://github.com/chenwei791129/ShadowDNS/commit/f1607e1f7db1dc49d65dae956cece693c7615fb4))

## [0.3.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.2.1...v0.3.0) (2026-04-15)


### Features

* **view:** support GeoIP2/GeoLite2 mmdb fallback chain ([2c53b62](https://github.com/chenwei791129/ShadowDNS/commit/2c53b621e928b909156d171f5fcd62dc1dbae967))
* **zone:** support BIND-compatible quoted $INCLUDE syntax ([1621aa8](https://github.com/chenwei791129/ShadowDNS/commit/1621aa8cd9ec15fb1ad630d56584ae6c3de07a90))


### Bug Fixes

* **smoke:** copy entire master/ tree recursively ([13b57eb](https://github.com/chenwei791129/ShadowDNS/commit/13b57ebf46f3a10efe371328aa885f3e09eb152c))

## [0.2.1](https://github.com/chenwei791129/ShadowDNS/compare/v0.2.0...v0.2.1) (2026-04-14)


### Bug Fixes

* pass release tag to nfpm so .deb version matches the release ([beb3472](https://github.com/chenwei791129/ShadowDNS/commit/beb3472239630c4c0411d894a5d447c7fad1dbd8))

## [0.2.0](https://github.com/chenwei791129/ShadowDNS/compare/v0.1.0...v0.2.0) (2026-04-14)


### Features

* add Prometheus /metrics endpoint with 8 DNS metrics ([a0433cc](https://github.com/chenwei791129/ShadowDNS/commit/a0433cc1cc57524c8470e13ecd202944570c1c97))
