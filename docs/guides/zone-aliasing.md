# How Zone Aliasing Works

Zone aliasing is the core mechanism of ShadowDNS: the root domain is fully loaded into memory, a backup domain is just a pointer to the root, and at query time, in-bailiwick rewriting produces on the fly a response that "looks like a complete backup zone". This page describes the four stages of the query processing pipeline and the rewrite rules.

## Query Processing Pipeline

```text
Client query
     |
     v
[ View Matcher ]
     |   Evaluates match-clients rules (GeoIP country, GeoIP ASN,
     |   IP/CIDR, any) in declaration order. First match wins,
     |   returning the view name.
     |
     v
[ Alias Resolver ]
     |   Checks whether the queried zone is a backup alias. If so,
     |   rewrites the query name from backup.domain to root.domain
     |   before the lookup, and records the original backup name
     |   for use in the response.
     |
     v
[ Zone Lookup ]
     |   Looks up the matching owner entry in the selected view's
     |   in-memory zone tree (map[ownerName][]RR), O(1) per owner name.
     |   On no exact hit, attempts wildcard matching per RFC 4592:
     |   strips labels left to right, level by level, until a
     |   `*.<parent>` entry is found, or an existing name blocks
     |   it (ENT rule).
     |
     v
[ In-Bailiwick Rewrite ]
     |   Rewrites the owner name back to the backup domain. For RDATA
     |   fields containing DNS names (CNAME, NS, MX, SRV, SOA, HTTPS,
     |   SVCB, DNAME, and more — see the covered-type list below):
     |   if the target points inside the root zone, it is rewritten to
     |   the backup zone; targets pointing elsewhere (e.g., third-party
     |   CDN hostnames) are left unchanged. Name-bearing record types
     |   outside the covered list are withheld from backup answers.
     |
     v
Response sent to client
```

## Stage Details

### View Matcher

Each view's `match-clients` block is compiled at startup into an ordered slice of rules. Rules are evaluated left to right; the first rule matching the client's source IP determines the view, and if no view matches, the response is REFUSED. GeoIP lookups use MaxMind mmdb files read directly into memory; the mmdb files are reopened on every SIGHUP reload, so MaxMind's monthly updates take effect without restarting the process.

### Alias Resolver

At query time, the resolver performs a **longest-suffix match** against the alias map (built at startup from the `aliases` section of `shadowdns.yaml`). A backup zone entry is a thin pointer — the resolver strips the backup suffix, substitutes the root suffix, and hands the rewritten name to the zone lookup. The original backup name is retained so the rewrite stage can restore it.

### Zone Lookup

Zone data is stored as `map[viewName]map[zoneName]*Zone`, with each `Zone` holding a `map[ownerName][]dns.RR`. All structures are read-only after startup, so the read path requires no locking.

When an exact match yields no result, it falls back to wildcard matching per RFC 4592: DNS labels are stripped from the query name one at a time, probing the map for a `*.<parent>` entry, until the zone origin is reached or an existing name that blocks further traversal is hit (the empty non-terminal rule). CNAME wildcard synthesis and correct response owner name rewriting are supported.

Backup override records (TXT, MX, SRV provided by the backup zone's own zone file) are stored separately and merged into the result after the root lookup.

### In-Bailiwick Rewrite

The rewrite rules are deliberately conservative:

| Target | Rewrite behavior |
|------|----------|
| Owner name | Always rewritten (in-bailiwick by definition) |
| DNS names in RDATA of the covered record types | Rewritten only when pointing inside the root zone — ensuring the rewritten name can also be resolved correctly through the same alias mechanism |
| RDATA names pointing externally (e.g., third-party CDN hostnames) | Left unchanged |
| A / AAAA | Carry IP addresses; never rewritten |
| TXT | RDATA is treated as opaque data; never rewritten — even if the content string happens to equal the root domain name |
| Name-bearing record types outside the covered list | Withheld from backup-zone answers (see below) |

The **covered record types** — those whose RDATA name fields are rewritten — are:
`CNAME` (target), `NS`, `MX`, `PTR`, `SRV` (target), `SOA` (MNAME/RNAME), `HTTPS` (target), `SVCB` (target), `DNAME` (target), `NAPTR` (replacement), `RP` (mbox/txt), `KX` (exchanger), `AFSDB` (hostname), `PX` (map822/mapx400), and `RT` (host).

### Withholding of Uncovered Name-Bearing Types

A record type that carries a domain name in its RDATA but is **not** in the covered list (for example DNSSEC records such as `RRSIG` or `NSEC`, or the legacy mailbox types) is never emitted in a backup-zone answer: emitting it would expose the root domain in the RDATA while the owner name already says backup — leaking the very origin the alias is meant to hide. Instead, the record is **withheld**: the query gets an empty NODATA answer for that name and type (never NXDOMAIN, never SERVFAIL), a warning naming the withheld record type and owner is logged, and all other records in the zone are served normally. Whether a type is name-bearing is derived from the record type's RDATA metadata, so the protection is fail-closed — any future name-bearing type is withheld by default until it is explicitly added to the covered list. The same rule applies to synthesized alias zone transfers (AXFR); note that when a name exists in the root zone only through withheld record types, the transferred zone omits that name entirely, so a secondary answers NXDOMAIN for it while ShadowDNS itself answers NODATA.

## SOA Inheritance and Zone Transfers

- A backup zone's SOA is inherited from the root zone (the serial follows the root), so slaves can detect changes correctly.
- AXFR (full zone transfer over TCP) is supported for both root zones and alias zones; existing BIND slaves require no changes.
- NOTIFY is sent to each zone's NS records after startup and reload (can be disabled with `--no-notify` or `options { notify no; };`). NOTIFY target IPs are taken **only from in-zone glue records**; see [Migrating from BIND](../migration.md) for details.

## Configuration Example

```yaml
# shadowdns.yaml
aliases:
  example.com:          # root: fully loaded into memory
    - backup.example.com    # backup: a pointer to example.com
    - mirror.example.com
```

An A query for `www.backup.example.com` returns exactly the same response as if "a complete `backup.example.com` zone had been loaded" — but only a single copy of the `example.com` authoritative data exists in memory.

For the complete rules governing aliases (uniqueness, self-alias prohibition, override record type restrictions), see [shadowdns.yaml](../configuration/shadowdns-yaml.md).
