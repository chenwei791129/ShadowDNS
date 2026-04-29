# Canonicalize Callsite Audit (Task 1.4)

After redefining `Canonicalize` as case-preserving (trailing-dot only) and adding
`LookupKey` for lowercase fold, every existing call to `Canonicalize` was reviewed.

## Direct `dnsutil.Canonicalize` callsites

| File:Line | Use | Decision | Rationale |
|---|---|---|---|
| internal/ephemeral/store.go:66 | `Put` map key for `s.records[canonical]` | → `LookupKey` | Map key, RFC 4343 case-insensitive lookup |
| internal/ephemeral/store.go:92 | `Lookup` map key | → `LookupKey` | Map key |
| internal/ephemeral/store.go:118 | `Delete` map key | → `LookupKey` | Map key |
| internal/ephemeral/store.go:132 | `DeleteValue` map key | → `LookupKey` | Map key |
| internal/api/server.go:263 | `canonicalFQDN(r)` helper, feeds the four ephemeral map ops above | → `LookupKey` | Pure key derivation, no output use |
| internal/zone/parser.go:33 | `canonOrigin` for `IsInZone(ownerName, canonOrigin)` | → `LookupKey` | Comparison only |
| internal/config/aliases.go:81 | `normalizeDomain` produces alias map key + struct member | → `LookupKey` for the map key path; `AliasGroup.Members` and root yaml string keep original case (Task 3.1) | Map key needs fold; output path needs original case for backup-name preservation |

## Indirect `strings.ToLower(name)` callsites (related invariants)

| File:Line | Use | Decision | Rationale |
|---|---|---|---|
| internal/zone/zone.go:70 (`AddRR`) | `key := strings.ToLower(rr.Header().Name)` | Keep — add doc comment (Task 2.1) | Already correct: index key only, `rr` itself is stored unmutated |
| internal/zone/zone.go:244 (`FollowCNAME`) | `target := strings.ToLower(last.Target)` for `Lookup` | Keep | Lookup key path; equivalent to `LookupKey` modulo trailing dot (target is already FQDN from RR) |
| internal/zone/parser.go:56 | `ownerName := strings.ToLower(rr.Header().Name)` for `IsInZone` check | Keep — add doc comment (Task 2.2) | Local variable; never written back to `rr` |
| internal/server/handler.go:83 | `qname := strings.ToLower(q.Name)` | Split into `qname` (`LookupKey`) and `qnameOrig` (`q.Name`) (Task 5.1) | qname feeds zone matching / alias detect; qnameOrig feeds response owner / wildcard rewrite |
| internal/alias/detect.go:34 | `IsInZone(qname, z)` | Caller already lowercased | Both args lowercased per `IsInZone` contract |

## Summary

- **Map keys / comparisons (lookup-only)** → switch to `LookupKey`:
  ephemeral/store.go x4, api/server.go (canonicalFQDN), zone/parser.go:33,
  config/aliases.go (key path).
- **Storage / output** → keep `Canonicalize` (no callsite needed yet — alias
  config struct fields will keep yaml original case directly without going
  through `Canonicalize`).
- **Existing local `strings.ToLower(name)` patterns** → keep as-is; either
  already index-key-only (zone.go AddRR, zone parser line 56) or scheduled to
  be split into orig+key pair (handler.go).
