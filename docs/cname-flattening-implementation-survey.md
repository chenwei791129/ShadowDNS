# CNAME Flattening (apex CNAME / ANAME / ALIAS) Industry Implementation Survey

This document surveys the implementation status of CNAME Flattening (also known as ANAME, ALIAS record, or apex CNAME)
across other DNS services, as the basis for deciding whether ShadowDNS should promote this feature
from *Planned* in the [feature comparison table](index.md#feature-comparison-with-bind) to an implemented feature.

> **Survey date**: 2026-06-10
> **Survey method**: multi-source web research (open-source software documentation, each vendor's official documentation, IETF datatracker / RFCs,
> blogs from Cloudflare / APNIC / Akamai and others, community field tests). Primary sources are listed at the end of each section.

> **Scope note**: CNAME Flattening is a **purely authoritative-side** feature — the authoritative server itself
> resolves the "CNAME-like" record at the apex into A/AAAA and answers directly. This report therefore covers
> authoritative software, the standardization background, and commercial hosted services.

---

## Summary

The README marks CNAME Flattening as **Planned** and the BIND comparison column as "No" — both are **correct**.
The landscape shows a clear **"open source weak, commercial strong" split**:

- **None of the three major traditional open-source authoritative servers support it** — BIND 9, NSD, and Knot DNS all refuse to implement it
  on the grounds of "strict RFC compliance; CNAME semantics are not allowed at the apex". **In the open-source world, only PowerDNS (ALIAS, v4.0+) and
  Technitium (ANAME, v5+) genuinely support it**, and both require the server side to recursively resolve the target.
- **Commercial / cloud-hosted DNS supports it almost universally** — Cloudflare, Route 53, Azure, Google Cloud DNS,
  NS1, DNSimple, DNS Made Easy / Constellix, UltraDNS, Akamai, Gcore, Oracle, easyDNS,
  Namecheap, Netlify, Vercel, and Bunny all have it. But they split into two fundamentally different philosophies (see below).
- **There is no formal RFC for this feature**. The only standardization attempt, `draft-ietf-dnsop-aname`, **expired in early 2020 and
  exited the IETF working group in June 2021**. The modern IETF's orthodox replacement is **SVCB/HTTPS
  records from RFC 9460 (2023)**, but browser and resolver support for its AliasMode remains incomplete.
- **Wholesale conflict with DNSSEC**: dynamically synthesized A/AAAA cannot be pre-signed — the common bottleneck across all implementations.

**Conclusion**: by not implementing CNAME Flattening, ShadowDNS is **not alone among its open-source peers**
(it sits alongside BIND, NSD, and Knot). If it were to be done, the biggest obstacle is not DNSSEC (ShadowDNS does not support it anyway, so this conflict is simply void), but that
**the general-purpose version requires introducing an outbound recursive resolution path** — which directly contradicts ShadowDNS's core architectural stance of "authoritative-only, recursion
always off, NOTIFY only trusts in-zone glue, never touches resolv.conf".

**But there is a design route around this conflict** (see §5): if the flatten target is **restricted to zones
ShadowDNS itself serves** (in-bailiwick), target resolution degenerates into a pure in-memory lookup needing no outbound query — the same approach as NOTIFY's
in-zone-glue. This scoping not only dissolves the resolver conflict but also eliminates the GeoIP inaccuracy and loop detection
problems along the way. The cost is that it **does not serve the most classic use case, "apex pointing at an external CDN"** — only "apex pointing at another local zone".

---

## 1. Open-source / self-hosted authoritative DNS software

The key distinction: "**placing a record at the apex that points to another domain, with the server resolving it into A/AAAA at query time and answering directly**"
(true CNAME flattening) vs. "dynamically returns a CNAME but never expands it" or "can merely load it without implementing it" (doesn't count).

| Software | CNAME Flattening | Name | First version | Mechanism | Enabled by default? |
|---|---|---|---|---|---|
| **PowerDNS Auth** | ✅ Yes (most mature in open source) | ALIAS (RRType 65401) | v4.0.0 (2016); `expand-alias` flag since v4.1.0 | Recursively resolves via an **external resolver** at query time; AXFR expansion optional | ❌ Requires `expand-alias=yes` + configuring `resolver` |
| **Technitium** | ✅ Yes | ANAME | v5.0 (2020-07) | Server resolves the target recursively on its own; works at apex and subdomains alike | ✅ Effective as soon as an ANAME is created |
| **gdnsd** | ⚠️ Doesn't count | DYNC (Dynamic CNAME) | — | A plugin dynamically decides which CNAME to return, but it **returns the CNAME directly without expanding** and is **forbidden at the apex** | — |
| **CoreDNS** | ⚠️ Doesn't count | `alias` (third-party external plugin) | Unknown | Response rewriting: rewrites the CNAME chain returned by `file`/`auto`, not true server-side resolution; requires building from source | ❌ Not in the official build |
| **BIND 9** | ❌ No | — | — | ISC's official refusal: an apex CNAME makes a broken zone | — |
| **Knot DNS** | ❌ No | — | — | Strict RFC compliance; 3.5.4 can only "load" a private ALIAS type, with no expansion implemented | — |
| **NSD** | ❌ No | — | — | Minimalist, no embedded resolver, architecturally incapable of dynamic resolution | — |
| **YADIFA / Bundy / MaraDNS / djbdns** | ❌ No | — | — | None of them have this feature (MaraDNS even deliberately refuses dangling-CNAME recursion) | — |

Points worth noting:

- **PowerDNS is the most useful reference for self-hosted environments**: complete documentation, AXFR behavior controls
  (`outgoing-axfr-expand-alias`), commercial support. But its design reveals the core constraint — **the `resolver` setting
  is mandatory and must not point at itself (otherwise an infinite loop)**. In other words, doing CNAME flattening means introducing an outbound
  recursive query path. Disabled by default.
- **Technitium is one of only two true supporters**, usable at both apex and subdomains and effective by default, more intuitive than PowerDNS.
  But it is positioned as a self-hosted privacy/security DNS, not a large-scale authoritative deployment.
- **gdnsd's DYNC and CoreDNS's alias plugin are both "similar in form, different in substance"**: DYNC returns the CNAME directly to the
  client (no expansion) and is forbidden at the apex; the CoreDNS plugin is after-the-fact response rewriting, requires recompiling, and is not production
  grade. Neither **satisfies** the core semantics of apex flattening.
- **The unanimous refusal by the three major traditional servers (BIND / NSD / Knot) is a positioning reference for ShadowDNS**: not doing this feature
  does not make ShadowDNS look behind in the "serious authoritative server" category.

---

## 2. Standardization background (apex CNAME restriction, the ANAME draft, SVCB/HTTPS)

CNAME Flattening has no formal RFC; understanding it requires going back to "why it is needed" and "how the IETF handled it".

### Why it is needed — the apex CNAME prohibition

- **The hard rule of RFC 1034 §3.6.2**: if a node has a CNAME, it must not have any other records
  ("If a CNAME RR is present at a node, no other data should be present").
- **The zone apex must carry SOA and NS** (RFC 1034 §4.2.1): neither can be removed, so placing a CNAME at the apex directly
  violates the protocol.
- **The business conflict**: CDNs / cloud load balancers (ALB, Cloudflare, Fastly) require customers to use a CNAME pointing at their
  hostname (because their IPs change dynamically); but branding requirements often demand that the naked domain (`example.com` without `www`)
  resolve directly. Where the two intersect, they collide with the apex CNAME prohibition — CNAME Flattening was born to work around it.

### The ANAME standardization attempt and its failure

- **Origin**: in 2017-04 Evan Hunt (ISC) submitted an individual draft; in 2017-05 the DNSOP working group adopted it as
  `draft-ietf-dnsop-aname`, targeting Proposed Standard. Five WG versions were published (authors including engineers from ISC,
  PowerDNS, DNSimple, Cambridge, etc.), with `-04` (2019-07) as the final version.
- **Expiry**: **expired 2020-01-09, formally removed from the WG document list on 2021-06-25**, stream changed to None.
- **Primary causes of failure**: the working group could not reach consensus on several technical controversies —
  1. **Who is responsible for resolving the target?** The draft had the primary master periodically query the target's A/AAAA and write it back into the zone; but
     ISC held that "an authoritative server doing recursive queries is a deviation from normal behavior", and the resolution location becomes the authoritative
     server's geographic location (instead of the client's), causing **GeoIP routing inaccuracy**.
  2. **Loop detection unsolved**: draft Appendix E literally says "TODO: Solve this issue".
  3. **Zone transfer amplification**: when a frequently changing ANAME target is referenced by many zones, every IP change triggers
     an AXFR storm.
  4. Each vendor had long since shipped its own incompatible implementation, lowering the motivation to standardize.

### SVCB / HTTPS records (RFC 9460) — the modern orthodox replacement

- **RFC 9460 (published 2023-11)** defines two new RR types: `SVCB` (general-purpose) and `HTTPS` (HTTP-specialized).
- **AliasMode (SvcPriority=0) solves the apex problem directly**: HTTPS/SVCB records **can** coexist with SOA and NS at the
  apex, unconstrained by the CNAME prohibition, and resolution responsibility is handed back to the resolver (which knows the client's geographic location, avoiding
  ANAME's geo-inaccuracy problem).
- **Regarded as ANAME's orthodox IETF successor**, but it only serves HTTP traffic, not general-purpose DNS aliasing.
- **Adoption status (2024-25)**: Cloudflare automatically generates HTTPS records for hosted domains; Route 53 added support in
  2024-10; Safari supports it fully, Firefox partially, and **Chromium does not support AliasMode**. About
  25% of the Top 1M domains and roughly 4% of all domains have deployed it. **Until resolver support becomes widespread, CNAME Flattening remains the more reliable technique.**

---

## 3. Commercial / cloud-hosted DNS and CDNs

Commercial hosted DNS supports it nearly universally, but splits into two fundamental philosophies — the most critical factor when choosing:

- **(A) Server-side dynamic resolution (truly general-purpose)**: can point at **any external hostname**.
- **(B) Static alias pointing only at the vendor's own resources**: can only point at the vendor's own LB / CDN / IPs.

| Vendor | Supported? | Name | Launched | Philosophy | Can point at any external FQDN? |
|---|---|---|---|---|---|
| **DNSimple** | ✅ Yes (**earliest, 2011-11**) | ALIAS | 2011-11 | A | ✅ |
| **AWS Route 53** | ⚠️ Restricted | Alias record | 2011-05 | **B** | ❌ AWS resources only (CloudFront/ELB/S3…) |
| **DNS Made Easy** | ✅ Yes | ANAME | 2012-06 | A | ✅ |
| **Cloudflare** | ✅ Yes (**coined the term**) | CNAME Flattening | 2014-04 | A | ✅ (forced at apex on all plans; non-apex paid plans only) |
| **UltraDNS / Vercara** | ✅ Yes | Apex Alias | ~2016 | A | ✅ |
| **Azure DNS** | ⚠️ Restricted | Alias record set | 2018-09 | **B** | ❌ Azure resources only (Public IP/TM/Front Door…) |
| **Constellix** | ✅ Yes | ANAME | before ~2019 | A | ✅ (with ECS, AAAA, failover) |
| **Gcore** | ✅ Yes | CNAME flattening | 2023-03 | A | ✅ (free on all plans) |
| **Google Cloud DNS** | ✅ Yes | ALIAS | Preview 2022-08 / GA 2025-09 | A | ✅ (apex only, public zones only, no DNSSEC support) |
| **Akamai Edge DNS** | ✅ Yes | AKAMAITLC / AKAMAICDN | ~2015-16 | A (TLC) / B (CDN) | ✅ (AKAMAITLC arbitrary; AKAMAICDN bound to its own CDN) |
| **Oracle Cloud DNS** (formerly Dyn) | ✅ Yes | ALIAS | early Dyn days | A | ✅ (mutually exclusive with steering policies) |
| **easyDNS** | ✅ Yes | ANAME | before ~2015 | A | ✅ (free on all plans) |
| **Namecheap** | ✅ Yes | ALIAS | Unknown | A | ✅ (TTL selectable as 1 or 5 minutes only) |
| **NS1 / IBM** | ✅ Yes | ALIAS | Unknown (secondary apex added 2023-10) | A | ✅ |
| **Netlify / Vercel** | ✅ Yes (when using their own NS) | flattened CNAME / ALIAS | Unknown | A | ✅ (pointing at `*.netlify.com` / `cname.vercel-dns.com`) |
| **Bunny DNS** | ✅ Yes | CNAME flattening | ~2022-23 | A | ✅ (their own blog nevertheless argues ANAME hurts CDN routing) |
| **DigitalOcean / Vultr** | ❌ No / unknown | — | — | — | — |
| **Fastly** | ❌ **Actively opposed** | — | — | — | Recommends anycast A records instead |

Several architectural divergences are worth noting:

- **"Coiner of the term ≠ inventor of the feature"**: a common misconception is that Cloudflare (2014) invented this feature; in fact
  **DNSimple launched ALIAS as early as 2011-11**, and Route 53 Alias (2011-05, but limited to its own resources) and DNS Made
  Easy ANAME (2012-06) both came earlier. Cloudflare merely coined the **name** "CNAME Flattening", which became the generic term
  thanks to its market share.
- **The A vs. B philosophy is the first selection criterion**: Route 53 / Azure Alias can only point at their own resources (ecosystem lock-in),
  while all the server-side dynamic-resolution types can point at any hostname.
- **CDNs hold contradictory positions on their own feature**: Fastly **refuses to offer it** and actively advises against it (reason: it hurts CDN geo routing
  accuracy), pushing anycast A records instead; Bunny argues on its blog that ANAME hurts routing while offering it anyway. This echoes
  the core technical controversy that killed the ANAME draft — **server-side target resolution uses the authoritative server's geographic
  location, not the client's**.

---

## 4. Background, technical trade-offs, and trends

- **The feature is "mature but fragmented"**: widespread on the commercial side for many years with stable mechanics, but because standardization failed, every vendor's name and details differ
  (ALIAS / ANAME / CNAME Flattening / Apex Alias / AKAMAITLC…), interoperability is poor, and it often cannot be correctly propagated
  between secondary DNS providers (UltraDNS and PowerDNS both have this limitation).
- **Two implementation architectures**:
  - **Live-resolution type** (DNSimple, NS1, Namecheap, Google Cloud DNS, PowerDNS): the server
    resolves on every query — IPs are always fresh, but every query carries resolution cost.
  - **Cached-monitoring type** (DNS Made Easy / Constellix, easyDNS, Bunny): monitors the target IP in the background and refreshes
    the zone only on change — allows low TTLs and can even survive on stale cache when the target's DNS fails.
- **TTL and consistency**: a recurring pitfall. Cloudflare was documented in its early days as taking the **maximum** TTL of the chain (rather than the
  correct minimum); Google Cloud DNS explicitly takes the **minimum** TTL; Namecheap offers only the 1/5-minute options.
- **Wholesale conflict with DNSSEC** (the common bottleneck across all implementations):
  - Traditional DNSSEC signatures are **computed offline in advance**; dynamically synthesized A/AAAA simply do not exist at signing time and cannot be pre-signed.
  - APNIC's analysis (2020): CDNs assign edge IPs in real time based on performance, "impossible to know or predict in advance", hence impossible to sign ahead of time.
  - Even switching to ECDSA live-signing introduces a **replay attack** risk, since every IP change produces a fresh valid signature.
  - Google Cloud DNS, Akamai, and Technitium all state outright that "enabling this feature means DNSSEC cannot be used". Cloudflare's
    real-time ECDSA signing at the edge is a rare exception (but does not apply to pre-signed / secondary scenarios).
- **Trends**: vendor implementations are stable but no longer evolving; on the standards front the IETF has moved on to SVCB/HTTPS (RFC 9460). In the long
  run, the "proper answer" to the apex-pointing problem is slowly migrating from CNAME Flattening to the HTTPS record — but constrained by
  Chromium's lack of AliasMode support, this migration is still years away.

---

## 5. Interpretation and recommendations for ShadowDNS

### Should it be done? Leaning "doable if restricted to in-bailiwick targets"

**Arguments in favor of implementing:**

1. **The DNSSEC conflict does not exist for ShadowDNS**: the biggest technical obstacle to CNAME Flattening is its inability to coexist with DNSSEC
   — but ShadowDNS explicitly does not support DNSSEC per the [feature comparison table](index.md#feature-comparison-with-bind), so **this hardest constraint is simply void**.
   This is, conversely, one of the few features where ShadowDNS holds an "innate advantage" over other software.
2. **If a customer needs a naked domain pointing at a CDN, this is the only solution**: an apex cannot carry a CNAME — that is an iron protocol rule —
   so making `example.com` (without `www`) point at a CDN while retaining SOA/NS can only be done with flattening.
3. **Marking it Planned in the README is not wrong** — it is a real, widely used feature.

**Architectural conflicts of the general-purpose version (why the PowerDNS / Cloudflare approach cannot be copied):**

1. **It requires an outbound recursive resolution path — directly contradicting ShadowDNS's core stance**. ShadowDNS is
   authoritative-only with `recursion no` permanently in effect; even NOTIFY targets are **deliberately resolved from in-zone glue only,
   never touching `/etc/resolv.conf`, never doing recursive queries** (see the README NOTIFY section). Yet all server-side
   flattening (PowerDNS mandates the `resolver` setting) requires resolving targets recursively over the network. Introducing a controlled outbound
   resolver would break the "zero external DNS dependency" deployment assumption.
2. **GeoIP routing inaccuracy**: when flattening resolves an external CDN target on the server side, it uses ShadowDNS's own
   location, not the client's — exactly the main reason the ANAME draft failed and Fastly / Bunny object, and it fights against ShadowDNS's
   flagship source-IP GeoIP / ECS steering.

### The breakthrough: restrict to in-bailiwick targets (only flatten zones we serve ourselves)

If the flatten target is **restricted to any zone ShadowDNS itself has loaded** (external targets are uniformly rejected / treated as unresolvable),
target resolution degenerates from "outbound recursion" into "an in-memory zone tree lookup" — exactly the same approach as NOTIFY's in-zone-glue.
Determining "is the target local" is a longest-suffix match against all loaded zone origins (an existing capability of the alias
resolver), and "is it in-bailiwick" is an existing capability of the In-Bailiwick Rewrite stage — both reusable.

This scoping knocks out multiple obstacles at once:

| Obstacle | After restricting to in-bailiwick |
|---|---|
| Needs an outbound resolver (violates recursion-off) | ✅ Eliminated — pure read-path in-memory lookup, no external dependency |
| GeoIP routing inaccuracy | ✅ Eliminated — the target is looked up within the view the client already matched, preserving geo selection |
| Loop detection unsolved (the main killer of the ANAME draft) | ✅ Trivial — the local set is finite; a visited-set + max depth suffices |
| TTL uncertainty / the chain min-vs-max controversy | ✅ Eliminated — all TTLs are locally known values; take the chain minimum |
| No startup validation possible | ✅ Becomes feasible — targets can be validated as in-bailiwick at load time, otherwise fail-fast / warn |

**Costs and design decisions that must be pinned down:**

1. **It does not serve the most classic use case**: nine times out of ten the industry wants flattening for an apex pointing at an **external CDN** — exactly the case
   being excluded. This version only serves "apex pointing at another **local** zone" (e.g. multiple apexes sharing a centrally managed set of LB
   addresses). If the real need is an external CDN, evaluate the RFC 9460 HTTPS record instead.
2. **View consistency (the most critical)**: the target must be resolved within the view the client already matched; if the target zone is not in that
   view → return NODATA, otherwise split-horizon is broken.
3. **AXFR expansion**: the apex cannot be transferred to a slave as a CNAME; it must be expanded into A/AAAA at AXFR time (similar to PowerDNS
   `outgoing-axfr-expand-alias`), and expanded using the view that the slave matches. With the target local, expansion is still a
   pure in-memory operation.
4. **Composition order with zone aliasing**: whether a backup zone's apex can also be flattened, and which of flatten and
   in-bailiwick rewrite runs first, must be explicitly defined.
5. **Recommend apex-only**: non-apex names can already carry a CNAME, so keep the scope as tight as possible.

### One-sentence summary

> For ShadowDNS, implementing CNAME Flattening is not a "fall behind if we don't" matter in the open-source world (BIND/NSD/Knot all skip it), and its
> biggest obstacle is not the DNSSEC headache everyone else has (ShadowDNS is exempt), but that **the general-purpose version demands an outbound recursive resolution
> path, in head-on conflict with the core "authoritative-only, recursion-off, zero external dependency" stance**. **However, restricting
> flatten targets to zones ShadowDNS itself serves (in-bailiwick) dissolves the conflict** — target resolution degenerates
> into an in-memory lookup, the same approach as NOTIFY in-zone-glue, and GeoIP inaccuracy and loop detection are solved along the way.
> The cost is serving only "apex pointing at a local zone", not "apex pointing at an external CDN"; if the actual
> need is an external CDN, evaluate the RFC 9460 HTTPS record instead.

---

## Key conflicts and open questions

1. **Cloudflare TTL: max or min?** Community records from 2014 say it took the **maximum** TTL of the chain, yet current official documentation
   says "minimum". Whether this has been fixed, or corresponds to different proxied vs. non-proxied scenarios, needs hands-on testing.
2. **Does Azure Alias count as CNAME Flattening?** Strictly speaking it is control-plane resource tracking (bound to an Azure
   resource), not DNS wire-format CNAME chain expansion — similar effect, fundamentally different mechanism.
3. **Whether Oracle Cloud DNS ALIAS can point at any external FQDN**: the documented RDATA format implies yes, but it is not stated explicitly, and the
   context of its mutual exclusion with steering policies is vague; needs hands-on testing.
4. **Several vendors' "launch years" are uncertain**: the exact go-live dates for NS1, easyDNS, Constellix, Akamai, Namecheap, Netlify, Vercel, and
   Bunny cannot be pinned down precisely from public documentation.
5. **Detailed discussion of the ANAME expiry**: public data comes from the draft text and WG slides; the item-by-item discussion on the DNSOP mailing list
   was not consulted directly (browsable at `https://mailarchive.ietf.org/arch/browse/dnsop/`).
6. **DigitalOcean / Vultr**: documentation does not explicitly deny it, but multiple sources indicate no support; needs direct testing to confirm.

---

## Primary sources

- [RFC 1034 §3.6.2 / CNAME at the apex of a zone — ISC Blog](https://www.isc.org/blogs/cname-at-the-apex-of-a-zone/) — the protocol roots of the apex CNAME prohibition; ISC's official position on ALIAS/ANAME
- [ISC BIND 9 KB: CNAME at the apex of a zone](https://kb.isc.org/docs/aa-01640) — explicit statement that BIND 9 does not support apex CNAME/ALIAS
- [draft-ietf-dnsop-aname-04 — IETF Datatracker](https://datatracker.ietf.org/doc/html/draft-ietf-dnsop-aname-04) / [history](https://datatracker.ietf.org/doc/draft-ietf-dnsop-aname/history/) — full ANAME draft text, resolution design, the loop TODO, version timeline and expiry
- [RFC 9460: SVCB/HTTPS RRs — RFC Editor](https://www.rfc-editor.org/info/rfc9460/) / [RFC 9460 DNS Evolution — Peakhour](https://www.peakhour.io/blog/rfc-9460-dns-evolution/) — AliasMode solving the apex problem; adoption rates
- [HTTPS DNS Record Support Current State — Kal Feher](https://kalfeher.com/https-current-state/) — current browser support (Safari full, Chromium no AliasMode)
- [Why dynamic DNS mapping prevents DNSSEC deployment — APNIC Blog (2020)](https://blog.apnic.net/2020/01/31/why-dynamic-dns-mapping-prevents-dnssec-deployment/) — the conflict between CDN dynamic IPs and DNSSEC pre-signing; ECDSA replay
- [PowerDNS ALIAS records — official documentation](https://doc.powerdns.com/authoritative/guides/alias.html) — `expand-alias`, mandatory `resolver`, AXFR expansion, version history
- [Technitium DNS Server v5 / ANAME — Blog (2020-07)](https://blog.technitium.com/2020/07/) / [help](https://technitium.com/dns/help.html) / [DNSSEC discussion #825](https://github.com/TechnitiumSoftware/DnsServer/discussions/825) — ANAME mechanism, apex/subdomain, DNSSEC incompatibility
- [gdnsd DYNC — zonefile wiki](https://github.com/gdnsd/gdnsd/wiki/GdnsdZonefile) / [man page](https://man.archlinux.org/man/gdnsd.zonefile.5.en) — DYNC forbidden at the apex, returns CNAME directly
- [CoreDNS alias plugin](https://coredns.io/explugins/alias/) / [GitHub](https://github.com/serverwentdown/alias) — response rewriting, requires recompiling
- [Knot DNS issue #475](https://gitlab.nic.cz/knot/knot-dns/-/issues/475) / [NSD zonefile](https://github.com/NLnetLabs/nsd/blob/master/doc/manual/zonefile.rst) / [MaraDNS FAQ](https://maradns.samiam.org/faq.html) — the three vendors' non-support positions
- [Introducing CNAME Flattening — Cloudflare Blog (2014)](https://blog.cloudflare.com/introducing-cname-flattening-rfc-compliant-cnames-at-a-domains-root/) / [CNAME flattening docs](https://developers.cloudflare.com/dns/cname-flattening/) — mechanism, apex vs. non-apex, plan differences
- [How The ALIAS Virtual Record Works — DNSimple Blog (2012)](https://blog.dnsimple.com/2012/02/how-the-alias-virtual-record-works/) — origin of ALIAS (2011-11, the earliest), the erl-dns mechanism
- [Choosing between alias and non-alias records — AWS Route 53](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-choosing-alias-non-alias.html) / [Document history](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/History.html) — Alias limited to AWS resources, launched 2011-05
- [Alias records overview — Azure DNS](https://learn.microsoft.com/en-us/azure/dns/dns-alias) / [GA update](https://azure.microsoft.com/en-gb/updates/azure-dns-alias-records-generally-available/) — limited to Azure resources, GA 2018-09
- [DNS records overview — Google Cloud DNS](https://docs.cloud.google.com/dns/docs/records-overview) / [release notes](https://docs.cloud.google.com/dns/docs/release-notes) — ALIAS limited to apex/public/no-DNSSEC, Preview 2022 / GA 2025
- [Apex Alias FAQ — UltraDNS](https://dns.ultraproducts.support/hc/en-us/articles/4409649081499-Apex-Alias-Frequently-Asked-Questions) — mechanism, zone transfer limitations
- [ANAME in DNS Made Easy vs Constellix — Blog](https://social.dnsmadeeasy.com/blog/aname-records-in-dns-made-easy-vs-constellix/) — ECS / IPv6 / failover differences, the cached-monitoring type
- [Gcore CNAME flattening — Docs](https://gcore.com/docs/dns/dns-records/specify-cname-at-root) / [Blog (2023-03)](https://gcore.com/blog/gcore-dns-introduces-cname-flattening) — free on all plans, limitations
- [Akamai Edge DNS features](https://techdocs.akamai.com/edge-dns/docs/features) / [Zone Apex Mapping & DNSSEC](https://www.akamai.com/blog/security/edge-dns--zone-apex-mapping---dnssec) — AKAMAITLC / AKAMAICDN, DNSSEC incompatibility
- [Using Fastly with apex domains — Fastly Docs](https://www.fastly.com/documentation/guides/full-site-delivery/domains-and-origins/using-fastly-with-apex-domains/) — Fastly's opposition to flattening, pushing anycast
- [How ANAME records affect CDN routing — bunny.net Blog](https://bunny.net/blog/how-aname-dns-records-affect-cdn-routing/) / [support](https://support.bunny.net/hc/en-us/articles/24872742824220-Do-you-support-CNAME-flattening) — Bunny's contradictory position, the geo routing problem
- [Oracle Cloud DNS supported records](https://docs.oracle.com/en-us/iaas/Content/DNS/Reference/supporteddnsresource.htm) / [easyDNS ANAME](https://easydns.com/features/aname-root-domain-alias/) / [Namecheap ALIAS](https://www.namecheap.com/support/knowledgebase/article.aspx/10128/2237/how-to-create-an-alias-record/) / [NS1 secondary apex ALIAS](https://community.ibm.com/community/user/blogs/annie-liu/2023/10/03/ns1-now-supports-apex-alias-for-secondary-zones) — remaining vendors' mechanisms and limitations
- [Netlify external DNS](https://docs.netlify.com/manage/domains/configure-domains/configure-external-dns/) / [Vercel DNS records](https://vercel.com/docs/domains/managing-dns-records) — ALIAS / flattened CNAME recommendations on the PaaS side
