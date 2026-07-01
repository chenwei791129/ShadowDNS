## Problem

The AXFR zone-transfer handler can permanently leak a goroutine and the full in-memory copy of a zone's records when a transfer peer aborts the connection mid-stream. Under repeated aborts the process accumulates leaked goroutines and retained zone allocations with no upper bound, degrading toward goroutine/memory exhaustion. The transfer path has no concurrency cap and the shipped service unit sets no task/memory ceiling, so nothing else bounds the leak.

## Root Cause

The streaming helper in the zone-transfer subsystem (`internal/transfer/axfr.go`, `streamAXFR`, shared by both `HandleAXFR` and `HandleAliasAXFR`) starts a goroutine that runs the DNS library's transfer-out routine, reading envelopes from an unbuffered channel, while the calling goroutine performs three blocking sends on that channel (SOA, records, SOA). The library's transfer-out routine returns on the first write error to the peer and does not drain the channel. When a peer aborts after the first envelope, the consumer goroutine has already exited, so the next blocking send by the producer has no receiver and blocks forever; the handler's `wg.Wait()` never returns, stranding the goroutine and the referenced zone-record slice for the lifetime of the process.

## Proposed Solution

Decouple the producer from consumer exit so a send can never block after the transfer-out routine has returned. Use a buffered channel sized to the maximum number of envelopes the producer sends (3: SOA, records, SOA) so all sends complete without a live receiver; capture the transfer-out routine's error via a buffered result channel and join on it. As defense in depth, recover any panic inside the transfer goroutine so a packing failure cannot crash the process. Add a regression test against our own `streamAXFR` using a fake `dns.ResponseWriter` that returns an error on its second `WriteMsg`: the call MUST return promptly rather than hang.

## Non-Goals

- Adding an AXFR concurrency limit / semaphore (separate hardening; not required to stop this leak).
- Adding systemd `TasksMax`/`MemoryMax` bounds to the packaging unit (separate hardening).
- Adding per-envelope or whole-stream write deadlines for slow-read peers (related but separately tracked hardening).
- Testing the third-party DNS library's transfer-out behavior itself; tests cover only our `streamAXFR`.

## Success Criteria

- When a transfer peer aborts after the first envelope (its `WriteMsg` errors mid-stream), `streamAXFR` returns promptly and leaks neither the producer goroutine nor the zone-record slice.
- A regression test drives `streamAXFR` with a fake `dns.ResponseWriter` that errors on the second `WriteMsg` and asserts the call completes within a short timeout (does not hang).
- A panic raised inside the transfer goroutine is recovered and does not terminate the process.
- Existing AXFR and alias-AXFR behavior (SOA → records → SOA ordering, UDP refusal, ACL gating) is unchanged for the success path.

## Impact

- Affected specs: zone-transfer (modified)
- Affected code:
  - Modified: internal/transfer/axfr.go
  - New: internal/transfer/axfr_stream_test.go
