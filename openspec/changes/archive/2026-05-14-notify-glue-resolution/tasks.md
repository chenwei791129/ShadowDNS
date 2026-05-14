## 1. Failing unit tests for glue-based NOTIFY target resolution

- [x] 1.1 Add test in [internal/transfer/notify_test.go](internal/transfer/notify_test.go) asserting `NotifyTargets` returns `NotifyTarget` struct with in-zone A glue IP populated — covers `Send NOTIFY on zone content change` scenario "NOTIFY sent to each in-zone glue IP of an NS target" and exercises the `NotifyTargets()` 回傳型別改為結構 decision
- [x] 1.2 Add test for `多 IP glue 的處理：每個 IP 各發一次` decision: NS target with (A + AAAA) and NS target with multiple A records both return all IPs in `NotifyTarget.IPs` — scenario "NOTIFY sent to every glue IP when multiple exist"
- [x] 1.3 Add test for `無 glue 時 skip，**不** fallback 到系統 resolver` decision: NS target pointing to out-of-bailiwick hostname returns `NotifyTarget` with empty `IPs` slice and no system-resolver call — scenario "NS target without in-zone glue is skipped"
- [x] 1.4 Add test for `Glue 查找同 zone 優先，不跨 zone` decision: glue lookup for a zone only consults its own `*zone.Zone` record map, not other loaded zones
- [x] 1.5 Update existing MNAME-exclusion test to cover `Send NOTIFY on zone content change` scenario "NOTIFY not sent to SOA MNAME" under the new return type (MNAME excluded even when its own glue is present)

## 2. Implement glue-based NotifyTargets

- [x] 2.1 Define `NotifyTarget` struct (`Host string`, `IPs []netip.Addr`) in [internal/transfer/notify.go](internal/transfer/notify.go) per the `NotifyTargets()` 回傳型別改為結構 decision
- [x] 2.2 Implement in-zone A/AAAA glue lookup helper using `z.Lookup(strings.ToLower(dns.Fqdn(host)), dns.TypeA)` and `z.Lookup(..., dns.TypeAAAA)`, converting `*dns.A` / `*dns.AAAA` RDATA (`net.IP`) to `netip.Addr` via `netip.AddrFromSlice` (enforces `Glue 查找同 zone 優先，不跨 zone`; respects the lowercase owner-key invariant in `internal/zone/zone.go`)
- [x] 2.3 Change `NotifyTargets` signature to `func NotifyTargets(z *zone.Zone) []NotifyTarget`, skip SOA MNAME as before, populate `IPs` via the helper from 2.2
- [x] 2.4 Run tests from section 1 and confirm all pass

## 3. Update dispatch layer to use glue IPs

- [x] 3.1 In [cmd/shadowdns/main.go](cmd/shadowdns/main.go) `dispatchNotifies()`, expand de-dup key from `(origin, target)` to `(origin, host, ip)` per the De-dup key 從 `(origin, target)` 擴為 `(origin, host, ip)` decision
- [x] 3.2 For each `NotifyTarget` with empty `IPs`, emit a debug log (`logger.Sugar().Debugw`, per the zap migration) carrying `zone`, `target`, and `source="skipped-no-glue"`; spawn no goroutine — matches `Send NOTIFY on zone content change` scenario "NS target without in-zone glue is skipped"
- [x] 3.3 For each `(host, ip)` pair, spawn a goroutine calling `SendNOTIFY` against `net.JoinHostPort(ip.String(), "53")` instead of the hostname (realises the `多 IP glue 的處理：每個 IP 各發一次` decision on the dispatch side)
- [x] 3.4 Thread `source="glue"` into both the final failure `logger.Sugar().Warnw` in `dispatchNotifies` and the per-attempt `Warnw` emitted by `sendNotifyWithBackoff` in [internal/transfer/notify.go](internal/transfer/notify.go) — implements the Log 新增 `source` 欄位 decision; adjust `SendNOTIFY` signature or logger-attribute passing as needed (logger is `*zap.Logger`, not `*slog.Logger`)

## 4. Dispatch-layer tests

- [x] 4.1 Add unit test in [cmd/shadowdns/main_test.go](cmd/shadowdns/main_test.go) for `dispatchNotifies` verifying: (a) no goroutine spawned for zero-IP targets, (b) one goroutine per IP for multi-IP targets, (c) `source="skipped-no-glue"` debug log for no-glue cases — ties back to `Send NOTIFY on zone content change` scenarios
- [x] 4.2 Add unit test asserting cross-view deduplication by `(origin, host, ip)`: load the same zone under two views and confirm exactly one NOTIFY send per `(zone, host, ip)` tuple — covers scenario "Cross-view deduplication by zone-host-IP tuple"

## 5. Documentation

- [x] 5.1 [P] Update [README.md](README.md) NOTIFY section to document glue-only resolution, the skip-on-missing-glue behavior, the `source` log field, and the out-of-bailiwick NS limitation; reference that `also-notify` is future work for explicit IP targets
