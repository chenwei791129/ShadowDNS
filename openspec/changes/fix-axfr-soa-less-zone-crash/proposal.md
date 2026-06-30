## Problem

A zone file that contains records but no SOA is loaded and served as a normal root zone. A subsequent AXFR for that zone crashes the entire server process with a SIGSEGV: the zone's SOA pointer is nil, and the transfer handler hands that nil SOA to the streaming routine, where packing it dereferences a nil receiver inside the transfer goroutine — a fault that the per-query panic-recover does not cover, so the whole process dies and stops serving every zone.

## Root Cause

Nothing rejects a SOA-less root zone at load time: the zone parser does not require a SOA, and zone classification assigns the root role without inspecting the SOA, so `z.SOA` remains nil for a root zone whose file lacks a SOA. The zone-transfer handler (`internal/transfer/axfr.go`) then guards only for a nil zone, not a nil SOA — `HandleAXFR` passes `z.SOA` (nil) to the streaming routine, and `HandleAliasAXFR` passes `rootZone.SOA` (nil) into backup-SOA synthesis. Packing a typed-nil SOA pointer dereferences nil; because a typed-nil pointer boxed in an interface is not interface-nil, the library's nil guards do not catch it, and the panic occurs in a transfer goroutine outside any recover.

## Proposed Solution

Two layers:
1. Reject a SOA-less root zone at load time. During state build (`internal/server/build.go`), after a zone is classified as root, treat the absence of a usable apex SOA as a load error so the invalid zone never becomes servable. This follows the existing fail-soft reload model: a bad zone surfaces as a load error and the previously running state is retained.
2. Make the transfer handler defensive. In `HandleAXFR`, refuse (RCODE=REFUSED) when the zone has no SOA instead of streaming a nil SOA; in `HandleAliasAXFR`, refuse when the root zone has no SOA instead of calling backup-SOA synthesis with nil. This guarantees the transfer path can never pack a nil SOA even if a nil-SOA zone reaches it by another route.

## Non-Goals

- Adding the transfer-goroutine `recover()` defense-in-depth — that is delivered separately (it is the subject of the AXFR streamAXFR goroutine-leak change / PR for issue #10) and is not duplicated here.
- Changing AXFR success-path behavior, the allow-transfer ACL, or UDP refusal.
- Validating other zone invariants (apex NS, glue, etc.) beyond the apex SOA presence.

## Success Criteria

- A root zone whose file has no SOA record is rejected at load: startup fails with a clear error, and a SIGHUP reload that introduces such a zone retains the prior running state instead of serving the invalid zone.
- `HandleAXFR` returns RCODE=REFUSED (no crash) when invoked for a zone whose SOA is nil.
- `HandleAliasAXFR` returns RCODE=REFUSED (no crash) when the backing root zone's SOA is nil.
- A unit test drives `HandleAXFR` with a zone whose SOA is nil over a TCP-style writer and asserts a REFUSED reply with no panic / process abort; an equivalent test covers `HandleAliasAXFR`.
- Existing AXFR / alias-AXFR success-path tests continue to pass unchanged.

## Impact

- Affected specs: zone-transfer (modified), zone-parser (modified)
- Affected code:
  - Modified: internal/transfer/axfr.go
  - Modified: internal/server/build.go
  - New: internal/transfer/axfr_soa_test.go
