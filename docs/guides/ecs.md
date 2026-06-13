# EDNS Client Subnet (ECS)

ShadowDNS supports EDNS Client Subnet (ECS, RFC 7871) as an opt-in enhancement to GeoIP view selection. When a query arrives through a public resolver (Google Public DNS, etc.), the source IP seen by the server is the resolver's address, not the end user's — so GeoIP view selection reflects where the resolver sits, not where the user is. ECS lets the resolver attach the user's subnet to the query; with ECS enabled, ShadowDNS uses that subnet for geo matching instead.

ECS is **disabled by default**, matching BIND (which removed its experimental ECS support in 9.13.0). With the flag off, query behavior is bit-identical to a build without the feature.

---

## Enabling

ECS processing is controlled by a single CLI flag:

```bash
shadowdns --config /etc/shadowdns/shadowdns.yaml --ecs-enable
```

- `--ecs-enable` defaults to `false`. It is read only at startup — SIGHUP does not change it.
- At startup the server logs an info-level entry stating whether ECS processing is enabled or disabled (also on `--dry-run`), so the active state is always auditable from the log.
- When disabled, ECS options in queries are ignored for all purposes — view selection, validation, response assembly — and responses never carry an ECS option (the RFC 7871 requirement for servers without ECS enabled).

---

## How ECS affects view selection

When ECS is enabled and a query carries a valid ECS option with a source prefix length greater than 0, the ECS address becomes the **geo lookup address** — but only for the rule types that consult GeoIP:

| `match-clients` rule type | Address evaluated |
|---------------------------|-------------------|
| `country` | ECS address |
| `asn` | ECS address |
| IP | real source IP |
| CIDR | real source IP |
| `any` | matches regardless |

IP and CIDR rules always evaluate the real transport source IP. This is a deliberate security boundary: ECS is client-supplied data, so **a forged ECS option can never select an ACL-protected view**. Use country/ASN rules for geo steering and IP/CIDR rules for access control; ECS only influences the former.

Additional rules:

- A query without an ECS option is processed exactly as if ECS were disabled.
- If the OPT record contains more than one ECS option, only the first is processed.
- If the GeoIP databases have no entry for the ECS address, country/ASN rules evaluate to no-match and matching proceeds to the next rule. There is **no fallback** to re-evaluating geo rules with the source IP.
- AXFR/IXFR view selection never uses ECS (a malformed ECS option on a transfer query still gets FORMERR like any other query).

---

## Without GeoIP databases

GeoIP loading is conditional (see [GeoIP Databases](../configuration/geoip.md#running-without-geoip)): a configuration with no `geoip-directory` and no geo rules runs without any mmdb file. In that state ECS has **no effect on view selection** — there are no country/ASN rules to evaluate, and IP/CIDR/`any` rules always use the real source IP — so the only ECS behavior that remains is the [response echo](#response-echo) below.

To keep this state auditable, ShadowDNS emits a **Warn** log when `--ecs-enable` is active but no GeoIP databases are loaded: once at startup, and again on any reload that ends in that state.

---

## Response echo

For a valid ECS query, the response OPT record carries exactly one ECS option that echoes the query's FAMILY, SOURCE PREFIX-LENGTH, and ADDRESS, with **SCOPE PREFIX-LENGTH set equal to the query's SOURCE PREFIX-LENGTH**. The echo applies to every response assembled by the standard answer path — NOERROR, NXDOMAIN, and the REFUSED returned to clients matching no view.

Responses produced before the ECS processing point are exempt: NOTIMP for unsupported opcodes, FORMERR for malformed question counts, BADVERS for unsupported EDNS versions, FORMERR for malformed COOKIE options, zone-transfer response streams, and panic-recovery SERVFAIL. A query without an ECS option never receives one in the response.

---

## Client opt-out

A well-formed ECS option with SOURCE PREFIX-LENGTH 0 is the RFC 7871 client opt-out ("do not use my subnet"). ShadowDNS honors it for FAMILY 0, 1, and 2 alike — including the FAMILY 0 form that `dig +subnet=0` sends:

- View selection uses the real source IP only.
- The response echoes the ECS option, preserving the query's FAMILY, with SCOPE PREFIX-LENGTH 0.

---

## Malformed ECS handling

With ECS enabled, a query whose ECS option violates RFC 7871 is rejected with FORMERR (the response carries an OPT record but no ECS option). The handler treats an option as malformed when:

- the query's SCOPE PREFIX-LENGTH is non-zero (RFC 7871 mandates 0 in queries), or
- address bits beyond the SOURCE PREFIX-LENGTH are non-zero (with prefix length 0, every address bit is beyond the prefix — so this check takes precedence over opt-out classification).

Independently of the flag, the DNS message library rejects grossly malformed options at unpack time (unknown FAMILY, prefix length beyond the family maximum of 32/128); those queries get FORMERR before the handler runs. With ECS disabled, the handler-reachable malformed forms above are silently ignored instead of triggering FORMERR.

---

## Testing with dig

```bash
# Carry a client subnet: geo rules evaluate 203.0.113.0/24 instead of your source IP
dig @192.0.2.53 www.example.com A +subnet=203.0.113.0/24

# Client opt-out: answered by source IP, echoed with scope 0
dig @192.0.2.53 www.example.com A +subnet=0
```

In the first response, the `CLIENT-SUBNET` pseudo-section shows the echoed option, e.g. `203.0.113.0/24/24` (address / source prefix / scope).

---

## Operational notes

- In practice roughly 90% of ECS traffic originates from Google Public DNS; Cloudflare's 1.1.1.1 never sends ECS on privacy grounds. Enabling ECS therefore mainly improves geo accuracy for queries arriving via Google DNS — it is an enhancement layer on top of source-IP GeoIP, not a replacement.
- See the [CLI Reference](../reference/cli.md) for the `--ecs-enable` flag summary and the [feature comparison table](../index.md#feature-comparison-with-bind) for how this compares to BIND.
