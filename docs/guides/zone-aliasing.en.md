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
     |   fields containing DNS names (CNAME target, NS, MX, SRV,
     |   SOA MNAME/RNAME): if the target points inside the root zone,
     |   it is rewritten to the backup zone; targets pointing elsewhere
     |   (e.g., third-party CDN hostnames) are left unchanged.
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
| DNS names in RDATA (CNAME target, NS, MX, SRV, SOA MNAME/RNAME) | Rewritten only when pointing inside the root zone — ensuring the rewritten name can also be resolved correctly through the same alias mechanism |
| RDATA names pointing externally (e.g., third-party CDN hostnames) | Left unchanged |
| A / AAAA | Carry IP addresses; never rewritten |
| TXT | RDATA is treated as opaque data; never rewritten — even if the content string happens to equal the root domain name |

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
