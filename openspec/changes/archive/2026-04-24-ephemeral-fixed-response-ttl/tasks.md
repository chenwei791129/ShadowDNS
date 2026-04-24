## 1. Store layer — drop remaining-TTL output

- [x] 1.1 Update `internal/ephemeral/store_test.go` tests for the "Store ephemeral TXT records in memory with expiration" requirement: change assertions so `Record` only exposes `Value`, remove expectations that `Lookup` returns a remaining-TTL field. Keep the expiration scenarios for the "Expired records are not returned on lookup" requirement (61s-elapsed returns empty; per-entry 30s-vs-300s at T+31 returns only the 300s entry). Delete the old "TTL in response is dynamically computed" test (now REMOVED). Tests SHALL fail until step 1.2 lands.
- [x] 1.2 Modify `internal/ephemeral/store.go`: remove `Record.TTL` field so `Record` holds only `Value`; delete the remaining-seconds math and the `ttl<1 → 1` floor in `Lookup`; keep expiry filtering untouched. Adjust comments to state Store returns values only. Run `go test ./internal/ephemeral/...` green.

## 2. DNS handler — fixed 30s response TTL

- [x] 2.1 [P] Add a failing unit test in `internal/server/handler_ephemeral_test.go` covering the "Listen for DNS queries on UDP and TCP port 53" requirement's new fixed-TTL rule: insert one ephemeral entry with a long Store TTL (e.g. 3600s), issue the TXT query at two different times (T+0 and a later sample), assert both response RRs carry `Hdr.Ttl == 30`. Also add a case for multiple entries under one FQDN asserting both RRs carry TTL 30 (not their different remaining lifetimes).
- [x] 2.2 In `internal/server/handler.go` introduce `const EphemeralResponseTTL uint32 = 30` and change `lookupEphemeralTXT` to write that constant into `dns.RR_Header.Ttl` instead of the per-record value. Drop the now-unused `rec.TTL` read. Adjust the doc-comment on `lookupEphemeralTXT` to state the TTL is fixed. Run `go test ./internal/server/...` green.

## 3. API + integration test updates

- [x] 3.1 [P] Update `internal/api/server_test.go` and any callers that asserted DNS response TTL tracked the API `ttl` for the "PUT endpoint adds or refreshes an ephemeral TXT value" requirement: the PUT response body MUST still return the clamped Store-side `ttl` (1, 300, 1, 3600 in existing scenarios); if any test inspects DNS RR TTL via the handler, it MUST now assert 30. Re-cast the "same value refreshes" assertion to verify the entry is still live at T+31 with a Store TTL of 300 (rather than asserting DNS TTL ≈ 300).
- [x] 3.2 [P] Update `test/integration/ephemeral_overrides_cname_test.go` and `test/integration/cname_following_test.go` — both currently assert ephemeral DNS response TTL reflects the API TTL. Change to assert the fixed 30s for RR TTL, covering the "Ephemeral TXT entries override exact CNAME at the same qname for TXT queries" requirement. Record-liveness assertions (entry still present vs. expired) remain unchanged.
- [x] 3.3 Run full `make test` and `make lint` and confirm no references to `Record.TTL`, no remaining "per-entry remaining TTL" language in code comments, and no test still asserts decrementing TTLs.
