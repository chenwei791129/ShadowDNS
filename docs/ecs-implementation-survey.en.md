# ECS (EDNS Client Subnet, RFC 7871) Industry Implementation Survey

This document surveys the implementation status of EDNS Client Subnet (ECS, RFC 7871) across other DNS services, as a basis for deciding whether ShadowDNS should promote ECS from *Planned* in the [feature comparison table](index.md#feature-comparison-with-bind) to an implemented feature.

> **Survey date**: 2026-06-10
> **Survey method**: multi-source web research (open-source software documentation, vendors' official documentation, IETF drafts, academic measurement papers,
> hands-on tests from APNIC / Cloudflare blogs, etc.). Primary sources are listed at the end of each section.

---

## Summary

The README marks ECS as **Planned** and the BIND comparison column as "No" — both are **correct**. But BIND is the
**exception, not the norm**:

- **Nearly all mainstream open-source GeoDNS authoritative software supports ECS** — gdnsd, PowerDNS, and Knot DNS all have
  complete implementations. The ones that don't are NSD, MaraDNS, and YADIFA, which deliberately take a minimalist path, plus BIND, which removed the experimental feature.
- **Almost all commercial / cloud-hosted DNS services honor ECS** — Route 53, Azure Traffic Manager,
  Google Cloud DNS, NS1, UltraDNS, Constellix, and Gcore all enable it automatically.
- **But ECS sending on the resolver side is highly concentrated**: in practice roughly **90% of ECS traffic comes from Google Public
  DNS (8.8.8.8)**, and only about **12% of users** worldwide issue queries carrying ECS. Cloudflare 1.1.1.1 (the second-largest
  public resolver) **never sends ECS at all**, on privacy grounds.

**Conclusion**: not implementing ECS would leave ShadowDNS behind comparable GeoDNS software (gdnsd / PowerDNS / Knot), but
the practical benefit of implementing it is mostly confined to queries coming from Google DNS. ECS should be positioned as an
**opt-in enhancement layer** on top of the existing source-IP GeoIP, not a replacement.

---

## 1. Open-source / self-hosted authoritative DNS software

The key distinction: "**reads the ECS in the query to select a geo answer and writes back the SCOPE
PREFIX-LENGTH in the response**" (meeting the RFC 7871 authoritative-side contract — this is what counts) vs. "merely forwarding / stripping ECS"
(relay behavior, which doesn't count).

| Software | Authoritative ECS GeoDNS | First version | Mechanism | Enabled by default? |
|---|---|---|---|---|
| **gdnsd** | ✅ Yes (most mature) | v1.9.0 (2013), IANA-compliant since v2.0.0 | Built into `plugin_geoip` | Automatic when using geoip |
| **PowerDNS Auth** | ✅ Yes (most flexible) | pipe ~v3.x; Lua records v4.2 | GeoIP backend / Pipe backend ABI3 / Lua records (`ecswho`/`bestwho`) | ❌ Requires `edns-subnet-processing=yes` |
| **Knot DNS** | ✅ Yes (cleanest) | v2.7.0 (2018) | `mod-geoip` module (per-zone) | ❌ Requires `edns-client-subnet: on` |
| **Technitium** | ✅ Yes | geo apps v12.1 (2024); inbound ECS v15.0 (2026) | DNS Apps framework | ❌ Requires installing an App |
| **CoreDNS** | ⚠️ Partial | geoip plugin ~v1.8.5 (2021) | `geoip` plugin reads ECS for lookups; `view` plugin splits traffic | ❌ |
| **BIND (open source)** | ❌ No | Experimental feature removed in v9.13.0 (2018) | — | — |
| **NSD** | ❌ No | — | Minimalist by design, no module system | — |
| **MaraDNS / Deadwood** | ❌ No | — | Ignores the OPT record outright | — |
| **YADIFA / Bundy** | ❌ No | — | No geo / ECS functionality | — |

Points worth noting:

- **gdnsd is the industry's most mature authoritative ECS implementation**: IANA-compliant since 2014, it **automatically merges multiple
  GeoIP subnets into the largest possible supernet to optimize the SCOPE** (improving resolver-side cache efficiency), and Wikipedia
  (Wikimedia) uses it in production. It is the best reference when implementing.
- **CoreDNS only counts as half an implementation**: the `geoip` plugin reads ECS for lookups and the `view` plugin can split out different answers,
  but it **does not write back the ECS SCOPE in the response** — intermediate resolver caches don't know to cache per subnet, which violates
  the RFC 7871 authoritative-side contract. Functionally it can do split-horizon; semantically it is non-compliant.
- **BIND's removal is a warning sign for ShadowDNS**: ISC concluded that authoritative ECS was "difficult to deploy in production in practice" and
  cut it. While it looks simple at the RFC level, the hard engineering constraint that "all NS servers of the same zone must support ECS consistently — otherwise a single non-supporting
  NS returning a global-scope answer poisons the cache for the whole zone" is a real headache.

---

## 2. Recursive resolvers / public resolvers (the key factor that determines whether ECS is useful)

ECS is a two-sided collaboration between resolver and authoritative: **if the resolver doesn't send it, even a perfect authoritative-side implementation
receives nothing**.

| Resolver | Sends ECS by default? | Notes |
|---|---|---|
| **Google 8.8.8.8** | ✅ On by default | /24 IPv4, /56 IPv6; ~90% of all ECS traffic on the internet |
| **OpenDNS / Cisco Umbrella** | ✅ On by default | /24; original authors of the ECS draft |
| **Cloudflare 1.1.1.1** | ❌ Never sends (by design) | Privacy stance + sufficient anycast density; exception only for Akamai debug domains |
| **Quad9 9.9.9.9** | ❌ Doesn't send (privacy endpoint) | `9.9.9.11` is a separate ECS endpoint; users must pick the IP themselves |
| **AdGuard DNS** | ⚠️ Sends, but anonymized (ASN→random /24) | 2025 hands-on tests diverge from the official claim |
| **NextDNS** | ❌ Opt-in (anonymized ECS only) | No raw-subnet mode |
| **Unbound** | ❌ Opt-in | Requires compiling with `--enable-subnet` + configuring `send-client-subnet`; off by default |
| **BIND (named resolver)** | ❌ No | Only the commercial BIND 9-S edition has it; zero support in the open-source version |
| **PowerDNS Recursor** | ❌ Opt-in | `edns-subnet-allow-list` empty by default |
| **Knot Resolver** | ❌ Not implemented | No ECS module as of 2025 |
| **dnsmasq** | ❌ Opt-in (`add-subnet`) | OpenWrt / Pi-hole must enable it manually |
| **systemd-resolved** | ❌ Not implemented | Stub / forwarder, no ECS code |

**Key inference for the authoritative side**: in practice the ECS you actually receive comes almost exclusively from **Google DNS** (overwhelming
majority) plus a small amount of OpenDNS. All self-hosted resolvers (Unbound, PowerDNS Recursor, Knot Resolver,
open-source BIND) either don't send by default or haven't implemented it. Cloudflare, the second-largest resolver, is a **permanent blind spot**.

---

## 3. Commercial / cloud-hosted DNS and CDN GeoDNS

| Vendor | Honors ECS? | Notes |
|---|---|---|
| **AWS Route 53** | ✅ Yes | All geo routing policies, enabled automatically |
| **AWS CloudFront** | ✅ Yes | Since 2014-04-02, one of the earliest CDNs |
| **NS1 / IBM** | ✅ Yes | Integrated into the Filter Chain, minimum /24 |
| **Azure Traffic Manager** | ✅ Yes | Performance / Subnet / Geographic routing |
| **Google Cloud DNS** | ✅ Yes (public zones) | Geolocation routing policy; not used for private DNS |
| **UltraDNS / Vercara** | ✅ Yes | Directional DNS, per-record "Ignore ECS" available |
| **Constellix / Gcore** | ✅ Yes | GeoDNS / GeoProximity |
| **Cloudflare authoritative DNS** | ⚠️ Conditional | Only DNS-only Load Balancers with `prefer_ecs`; plain A/AAAA records get no ECS geo |
| **Akamai (CDN/GTM/Edge DNS)** | ⚠️ Allowlist-based | Only takes effect for resolvers with a commercial agreement (Google DNS, OpenDNS); others fall back to the resolver IP |
| **Fastly** | ⚠️ Allowlist-based | Same model as Akamai |
| **Oracle Cloud DNS** | ❓ Unclear / probably none | No ECS mention in the documentation (Dyn shut down in 2020) |
| **DNSimple** | ⚠️ Partial | Only ALIAS records forward to ECS-capable CDNs |

Two architectural divergences are worth noting:

- **Both Cloudflare and Akamai "downplay" ECS** — the former relies on ultra-dense anycast and considers ECS unnecessary; the latter uses an
  allowlist plus its own anycast / telemetry. Both represent the "my network is close enough, I don't need ECS" camp.
- **The remaining mainstream hosted DNS providers all honor ECS automatically**, with no customer configuration required.

---

## 4. Background, privacy controversy, and trends

- **RFC 7871 is Informational (not Standards Track)**: it merely "documents the existing production behavior of Google / OpenDNS",
  and edge-case specifications are unclear. The "improved Standards Track version" the RFC itself promised still (as of 2026)
  has not appeared.
- **The core cost = resolver-side cache fragmentation**: without ECS, resolver cache hit rates are around 80–85%; with /24 ECS
  enabled they drop to about 35–40%. But **this cost falls on the resolver, not the authoritative server** — for the purely authoritative
  ShadowDNS, **this cost essentially does not apply**.
- **The privacy trend is toward "more restrictions", not more openness**: Cloudflare doesn't send, Quad9's default endpoint doesn't send, NextDNS /
  AdGuard anonymize. Part of why the RFC is Informational is precisely the privacy objections.
- **Trend verdict: stable but stagnant**. It is neither deprecated nor growing, and the IETF is not advancing it.
  Academic measurements show roughly 53% of nameservers support ECS responses.
- **The anycast factor**: Cloudflare's argument has real merit — dense anycast already solves "continent-level" misrouting,
  but it cannot solve "city / ISP-level" precision. ECS delivers the most value in regions where public resolver coverage is sparse
  (Africa, West Asia).

---

## 5. Reading and recommendations for ShadowDNS

### Should we do it? Leaning "worth doing, but be clear-eyed about the benefit boundary"

**Reasons in favor of implementing:**

1. **Comparable software all has it**: gdnsd / PowerDNS / Knot all support it. Not doing it would leave ShadowDNS clearly behind in its
   GeoDNS positioning. Marking it Planned in the README is the right direction.
2. **The cache-fragmentation cost does not apply to ShadowDNS**: ShadowDNS is authoritative-only; what fragments is the resolver's
   cache, not its own. The biggest argument against ECS does not hold for this project.
3. **The Google DNS benefit is direct**: queries from 8.8.8.8 carry a real /24, directly improving geo-selection precision — and that
   is roughly 90% of all ECS traffic.
4. **A differentiating advantage over BIND**: BIND on ns1 does not support ECS; if ShadowDNS implements it, it can be more accurate than BIND
   on Google DNS traffic.

**Constraints to keep in mind:**

1. **ECS is an "enhancement", not a "replacement" for source-IP GeoIP**: Cloudflare / ISPs / privacy resolvers all skip
   ECS, so the existing source-IP geoip path (View Matcher) remains the workhorse; ECS only overrides it when the query carries ECS.
2. **Get SCOPE PREFIX-LENGTH right**: the response must write back the "largest" scope that correctly covers the geo region (if
   /16 works, don't force /24), otherwise resolver-side query volume gets amplified. **gdnsd's supernet-merging optimization** is the best
   reference template.
3. **Multi-NS consistency**: all authoritative NS servers of the same zone must support ECS consistently, otherwise the non-supporting one
   returns global-scope answers and poisons the cache. This was the main reason BIND gave up back then; watch for it at deployment time.
4. **ECS subnets are PII-adjacent**: once per-query logging lands, the recording / retention policy must be thought through together.
5. **Anonymized ECS is the resolver's business**: the authoritative side can only use whatever the resolver sends and cannot control its
   precision (e.g., AdGuard sends a fake subnet).

### One-sentence summary

> The sensible positioning for ShadowDNS implementing ECS is: **as an opt-in enhancement layer on top of source-IP GeoIP, primarily serving
> queries from Google DNS**, while making absolutely sure the response's SCOPE calculation is done correctly (see gdnsd). It will not, and should not,
> replace the existing View Matcher.

---

## Key conflicts and open questions

1. **Cloudflare 1.1.1.1 behavior**: officially it "does not send ECS", but 2025 hands-on tests found it forwarding
   ECS for Akamai domains. Possibly selective behavior toward specific CDN partners; unresolved in public sources.
2. **AdGuard DNS**: officially ASN→random-subnet anonymization, yet 2025 tests observed forwarding of real /24s. Possibly a difference
   between the public resolver and AdGuard Home (the self-hosted edition).
3. **ECS usage rate 12% vs. <5%**: APNIC reports 12% (share of users carrying ECS), other sources report <5% (share of
   queries carrying ECS); the measurement definitions differ.
4. **Oracle Cloud DNS**: absence from documentation does not equal confirmed non-support; needs hands-on verification.

---

## Primary sources

- [RFC 7871: Client Subnet in DNS Queries](https://www.rfc-editor.org/rfc/rfc7871.html)
- [Privacy and DNS Client Subnet — APNIC Blog (Geoff Huston, 2024-07)](https://blog.apnic.net/2024/07/23/privacy-and-dns-client-subnet/) — 90% Google, 12% of users, geographic distribution
- [EDNS Client Subnet in Practice — farrokhi.net (2025-10)](https://farrokhi.net/posts/2025/10/edns-client-subnet-in-practice-evaluating-public-resolver-behaviors/) — hands-on tests of each resolver
- [ECSeptional DNS Data — arXiv:2412.08478 (2024)](https://arxiv.org/abs/2412.08478) — 53% of nameservers supporting; "only PowerDNS and gdnsd support ECS"
- [BIND 9.13.0 ECS removal notes — ISC KB](https://kb.isc.org/docs/edns-client-subnet-ecs-for-resolver-operators-getting-started)
- [gdnsd plugin_geoip Wiki](https://github.com/gdnsd/gdnsd/wiki/GdnsdPluginGeoip)
- [PowerDNS GeoIP backend](https://doc.powerdns.com/authoritative/backends/geoip.html) / [Lua records](https://doc.powerdns.com/authoritative/lua-records/functions.html)
- [GeoIP in Knot DNS 2.7 — APNIC](https://blog.apnic.net/2018/11/14/geoip-in-knot-dns-2-7/)
- [Route 53 EDNS0](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-edns0.html) / [Azure Traffic Manager FAQ](https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-faqs) / [Google Cloud DNS routing policies](https://docs.cloud.google.com/dns/docs/routing-policies-overview)
- [Cloudflare 1.1.1.1 FAQ](https://developers.cloudflare.com/1.1.1.1/faq/) / [Cloudflare Load Balancing geo steering](https://developers.cloudflare.com/load-balancing/understand-basics/traffic-steering/steering-policies/geo-steering/)
- [Akamai GTM concepts](https://techdocs.akamai.com/gtm/docs/gtm-concepts) — ECS allowlist model
- [Google Public DNS ECS Guidelines](https://developers.google.com/speed/public-dns/docs/ecs)
