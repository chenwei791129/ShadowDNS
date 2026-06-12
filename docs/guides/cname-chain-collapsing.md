# CNAME Chain Collapsing

ShadowDNS can collapse in-zone CNAME chains in responses: instead of emitting every intermediate CNAME record, the server consumes the chain internally and answers with only the final result. The names of intermediate hops — internal load balancers, pool members, routing layers — never appear on the wire, so zone-internal naming stays confidential.

Collapsing is a **per-alias-group opt-in** (`collapse_cname_chain` in `shadowdns.yaml`, default `false`). With the flag off, chain emission is byte-identical to BIND and to previous ShadowDNS versions, including record order and TTLs.

!!! note "Not apex CNAME Flattening"
    This feature collapses chains *in responses* for names that legally hold a CNAME. It does not let a CNAME coexist with SOA/NS at the zone apex (the ANAME / ALIAS / "CNAME flattening" feature offered by some hosted DNS providers). Apex flattening remains a separate, planned feature — see the [feature comparison table](../index.md#feature-comparison-with-bind).

---

## Enabling

Set the flag on an alias group in `shadowdns.yaml`:

```yaml
aliases:
  example.com:
    members:
      - example.net
    collapse_cname_chain: true
```

- The flag is declared on the **root** domain. Every backup member of the group inherits it unconditionally — queries against `example.net` collapse exactly like queries against `example.com`.
- Absent or `false` means off. Unknown fields are rejected at load time (strict decoding), so a typo fails the startup or keeps the previous configuration on SIGHUP rather than silently disabling the feature.
- The flag participates in the normal [SIGHUP hot reload](../configuration/shadowdns-yaml.md#sighup-hot-reload): adding or removing it takes effect atomically with the rest of the configuration snapshot.

!!! warning "Rollback before downgrading"
    Older ShadowDNS binaries reject unknown YAML fields. To downgrade below the version that introduced this feature, remove `collapse_cname_chain` from `shadowdns.yaml` first, then SIGHUP, then downgrade.

---

## The unified collapse rule

When collapsing is enabled for the matched zone and a query's resolution starts a CNAME chain, the server consumes every hop that stays within the same zone and answers according to where the chain ends:

| Chain tail | Response |
|---|---|
| In-zone records of the queried type | **Only the terminal records.** Owner = the query name (preserving on-wire case), TTL = the minimum TTL across all consumed chain records including the terminals. No CNAME appears in the answer. |
| The chain leaves the zone (or the depth budget runs out) | **Exactly one synthesized CNAME**: owner = the query name, target = the first unresolved name, TTL = the chain minimum. No other in-zone name appears. |
| In-zone name without the queried type (including a dangling target) | **NODATA** — NOERROR with the zone SOA in the authority section. The consumed chain is not emitted, and the server does not fall back to wildcard synthesis for the query name. |

Worked TTL example — with these zone records:

```
www.example.com.     300  IN CNAME  lb.example.com.
lb.example.com.       60  IN CNAME  pool-a.example.com.
pool-a.example.com.  600  IN A      192.0.2.10
```

a query for `www.example.com. A` returns exactly:

```
www.example.com.  60  IN  A  192.0.2.10
```

(TTL = min(300, 60, 600); `lb` and `pool-a` are nowhere in the response). The same query with the flag off returns the full three-record chain.

The chain scope is a single zone: a target pointing at any other zone — even one ShadowDNS itself serves — counts as leaving the zone and produces the synthesized CNAME.

---

## Direct CNAME queries and intermediate names

- A query with `qtype=CNAME` never reveals the stored target. CNAME records are hops only under the unified rule, so the outcome is either the synthesized tail CNAME (chain leaves the zone) or NODATA (chain ends in-zone).
- Intermediate chain names remain directly queryable — their existence is not hidden — but their responses collapse under the same rule. Querying `lb.example.com. A` above returns `lb.example.com. 60 IN A 192.0.2.10`.
- Chains starting from (or passing through) wildcard-synthesized CNAMEs collapse identically.

---

## Backup-zone queries

For queries against a backup member, the collapsed answer carries the backup-namespace owner (the on-wire query name), and RDATA name fields still receive the group's existing rewrite rules:

- Terminal records get the in-bailiwick / `rewrite_rdata_labels` RDATA rewrite, exactly as un-collapsed answers do.
- A synthesized tail CNAME's target gets the same treatment a stored CNAME target gets today: the label-anywhere rewrite when the group sets `rewrite_rdata_labels: true` (templated CDN-style targets), the in-bailiwick suffix rule otherwise.

So with the example chain above, `www.example.net. A` answers `www.example.net. 60 IN A 192.0.2.10`.

---

## Edge cases

- **Depth budget**: a chase consumes at most 8 CNAME records (the same limit as un-collapsed chain following). A longer chain — or a CNAME loop in the zone data — synthesizes a CNAME pointing at the first unresolved name, letting the client continue resolution. For a loop this can produce a self-referential record (owner = target); that is a documented artifact of a mis-configured zone, terminated by the client resolver's own chase limit.
- **Meta-qtypes** such as `ANY` follow the same rule and end in NODATA at the chain tail.
- **Zone transfers are never collapsed**: AXFR carries the zone's stored records unchanged regardless of the flag, protected by the existing `allow-transfer` ACL.

---

## Testing with dig

```bash
# Collapsed terminal answer: one A record, no CNAMEs, chain-minimum TTL
dig @192.0.2.53 www.example.com A

# Backup member inherits the root's flag
dig @192.0.2.53 www.example.net A

# Chain tail lacks AAAA: NODATA (NOERROR + SOA in authority)
dig @192.0.2.53 www.example.com AAAA

# Direct CNAME query over an in-zone chain: NODATA, stored target not revealed
dig @192.0.2.53 www.example.com CNAME
```

---

## Operational notes

- Taking the chain-minimum TTL matches the convergent behavior of major hosted DNS providers and is the conservative choice: no consumer caches the answer longer than any link in the chain would have allowed.
- Collapsing trades response size for an extra in-memory lookup per chain hop at query time; the flag-off path is unchanged and benchmarked to be regression-free.
- See [shadowdns.yaml](../configuration/shadowdns-yaml.md) for the full `aliases` field reference and [How Zone Aliasing Works](zone-aliasing.md) for the rewrite pipeline that collapsed backup answers flow through.
