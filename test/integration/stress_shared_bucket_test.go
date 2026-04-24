package integration_test

// Stress reproducer for the cert-tool shared-bucket failure mode:
//
// The cert-tool DNS delegation is configured so every _acme-challenge.<apex>
// CNAMEs to a single shared bucket FQDN. Under parallelismLimit=8,
// 8 lineages × 6 SANs = 48 concurrent TXT PUTs land at one FQDN.
// Let's Encrypt validators then see 48 records and must find a per-lineage
// key-auth match. Lineage 9/10 (the two that did not fit in the first
// parallel batch) failed validation with "Incorrect TXT record X (and
// 47 more)" even though cert-tool's pre-check was green.
//
// This test reproduces the wire-level scenario against a real ShadowDNS
// in-process instance and checks whether the bucket ever drops or re-orders
// our values between PUT and the subsequent DNS query, under:
//
//   1. Simple baseline: 48 distinct PUTs, then UDP/TCP/EDNS0 queries.
//   2. Concurrent PUT + DeleteValue churn (mimicking lineage CleanUp races).
//
// Target FQDN is _acme-challenge.example.com. (example.com is authoritative
// in the integration fixture.) We write straight to the EphemeralStore, then
// issue DNS queries against the real bound port — this isolates the wire
// path without needing the HTTP API.

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
)

const (
	stressFQDN     = "_acme-challenge.example.com."
	stressValueTTL = uint32(3600) // matches cert-tool shadowdns.recordTTL
)

// challengeValue generates a 43-byte base64url string shaped like an ACME
// DNS-01 challenge key-auth sha256 digest.
func challengeValue(t testing.TB) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// queryEDNS sends an EDNS0 UDP query with the given buffer size and returns
// both the response and the packed wire size. bufferSize=0 means plain UDP
// with no OPT record.
func queryEDNS(t *testing.T, addr, qname string, qtype uint16, bufferSize uint16) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = false
	if bufferSize > 0 {
		m.SetEdns0(bufferSize, false)
	}
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("UDP EDNS0=%d query %s %s to %s: %v", bufferSize, qname, dns.TypeToString[qtype], addr, err)
	}
	return resp
}

// collectValues returns the TXT string values in resp's answer section.
func collectValues(resp *dns.Msg) []string {
	var out []string
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			out = append(out, strings.Join(txt.Txt, ""))
		}
	}
	return out
}

// containsAll reports whether haystack contains every string in needles.
// Returns the first missing value (empty string if none missing).
func containsAll(haystack, needles []string) (missing string) {
	set := make(map[string]struct{}, len(haystack))
	for _, v := range haystack {
		set[v] = struct{}{}
	}
	for _, want := range needles {
		if _, ok := set[want]; !ok {
			return want
		}
	}
	return ""
}

// TestStressSharedBucket_Baseline48Records is a sanity check: 48 distinct
// TXT records at a single FQDN, queried with UDP/UDP+EDNS0/TCP. Every query
// must return all 48 values. This is the simplest reproduction of the
// cert-tool shared-bucket state at peak.
func TestStressSharedBucket_Baseline48Records(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	values := make([]string, 48)
	for i := range values {
		values[i] = challengeValue(t)
		if n := store.Put(stressFQDN, values[i], stressValueTTL); n != i+1 {
			t.Fatalf("Put #%d returned count=%d, want %d", i, n, i+1)
		}
	}

	type probe struct {
		label   string
		network string
		buffer  uint16
	}
	probes := []probe{
		{"udp-plain-512", "udp", 0},
		{"udp-edns-1232", "udp", 1232},
		{"udp-edns-4096", "udp", 4096},
		{"tcp", "tcp", 0},
	}

	for _, p := range probes {
		t.Run(p.label, func(t *testing.T) {
			var resp *dns.Msg
			if p.network == "tcp" {
				resp = queryTCP(t, tcpAddr(srv), stressFQDN, dns.TypeTXT)
			} else {
				resp = queryEDNS(t, udpAddr(srv), stressFQDN, dns.TypeTXT, p.buffer)
			}
			got := collectValues(resp)
			t.Logf("answers=%d TC=%v rcode=%s", len(got), resp.Truncated, dns.RcodeToString[resp.Rcode])

			if p.network == "tcp" || p.buffer >= 4096 {
				// These must be authoritative-complete.
				if missing := containsAll(got, values); missing != "" {
					t.Errorf("missing value: %s (got %d answers, want 48)", missing, len(got))
				}
				if resp.Truncated {
					t.Errorf("TC=1 on %s with sufficient buffer — response should be complete", p.label)
				}
			} else {
				// Plain UDP / 1232 EDNS will truncate; TC MUST be set.
				if !resp.Truncated {
					t.Errorf("TC=0 on %s but answer count %d < 48 — retry-over-TCP would be broken", p.label, len(got))
				}
			}
		})
	}
}

// TestStressSharedBucket_ConcurrentPutDeleteQuery models the cert-tool
// production race: 8 "lineage" goroutines each Put 6 values then DeleteValue
// them (mimicking lineage Present → ACME validation → CleanUp), a 9th "late
// lineage" starts after a brief delay and Puts its own 6 values, and a
// continuous-query goroutine polls the FQDN via UDP+EDNS0 4096 throughout.
//
// The assertion is: once the late lineage's 6 values are Put, every
// subsequent query until the late lineage itself calls DeleteValue MUST
// include all 6 of those values. If any query returns a set missing one of
// them, ShadowDNS dropped a record visible to the outside world while still
// acknowledging the Put internally — which is exactly the cert-tool symptom.
func TestStressSharedBucket_ConcurrentPutDeleteQuery(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	const (
		earlyLineages    = 8
		valuesPerLineage = 6
		queryRounds      = 200
		earlyChurnRounds = 3 // how many Put→Delete cycles each early lineage runs
	)

	// Pre-generate the late lineage's 6 values.
	lateValues := make([]string, valuesPerLineage)
	for i := range lateValues {
		lateValues[i] = challengeValue(t)
	}

	ctx := make(chan struct{})
	var (
		earlyDone sync.WaitGroup
		lateDone  sync.WaitGroup
	)

	// Early lineages: Put 6 → sleep → Delete 6, repeated N times.
	for i := 0; i < earlyLineages; i++ {
		earlyDone.Add(1)
		go func(id int) {
			defer earlyDone.Done()
			for round := 0; round < earlyChurnRounds; round++ {
				vals := make([]string, valuesPerLineage)
				for j := range vals {
					vals[j] = challengeValue(t)
					store.Put(stressFQDN, vals[j], stressValueTTL)
				}
				time.Sleep(5 * time.Millisecond)
				for _, v := range vals {
					store.DeleteValue(stressFQDN, v)
				}
			}
		}(i)
	}

	// Late lineage: Put 6 values after a short delay, signal "ready", hold
	// the values in the store for the duration of the query loop, then Delete.
	lateReady := make(chan struct{})
	lateTeardown := make(chan struct{})
	lateDone.Add(1)
	go func() {
		defer lateDone.Done()
		time.Sleep(2 * time.Millisecond)
		for _, v := range lateValues {
			store.Put(stressFQDN, v, stressValueTTL)
		}
		close(lateReady)
		<-lateTeardown
		for _, v := range lateValues {
			store.DeleteValue(stressFQDN, v)
		}
	}()

	// Wait until the late lineage has finished putting its values.
	<-lateReady

	// Query loop: EDNS0 4096 (what Let's Encrypt uses), continuously.
	var (
		missedRound atomic.Int64
		totalRounds atomic.Int64
		firstMiss   atomic.Value // stores string
	)
	missedRound.Store(-1)

	queriesDone := make(chan struct{})
	go func() {
		defer close(queriesDone)
		for r := 0; r < queryRounds; r++ {
			totalRounds.Add(1)
			resp := queryEDNS(t, udpAddr(srv), stressFQDN, dns.TypeTXT, 4096)
			got := collectValues(resp)
			if missing := containsAll(got, lateValues); missing != "" {
				if missedRound.Load() == -1 {
					missedRound.Store(int64(r))
					firstMiss.Store(fmt.Sprintf(
						"round=%d answers=%d TC=%v missing=%q",
						r, len(got), resp.Truncated, missing))
				}
			}
		}
	}()

	<-queriesDone
	close(lateTeardown)
	lateDone.Wait()
	earlyDone.Wait()
	close(ctx)

	if mr := missedRound.Load(); mr >= 0 {
		miss, _ := firstMiss.Load().(string)
		t.Errorf("late lineage value missing in %d/%d query rounds; first miss: %s",
			/* count unknown without extra state but direction is enough */ 1, totalRounds.Load(), miss)
		t.Logf("PRODUCTION SYMPTOM REPRODUCED: shared-bucket loses late writes under concurrent churn")
	} else {
		t.Logf("all %d query rounds saw all 6 late-lineage values; bucket consistent",
			totalRounds.Load())
	}
}

// TestStressSharedBucket_WireSizeAt48 confirms the wire-level size of the
// UDP response carrying 48 TXT records. Documents the measured margin versus
// the 4096-byte EDNS0 buffer so future regressions are obvious.
func TestStressSharedBucket_WireSizeAt48(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	for i := 0; i < 48; i++ {
		store.Put(stressFQDN, challengeValue(t), stressValueTTL)
	}

	resp := queryEDNS(t, udpAddr(srv), stressFQDN, dns.TypeTXT, 4096)
	packed, err := resp.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	t.Logf("48 records → EDNS0 4096 UDP response: %d bytes packed, answers=%d, TC=%v",
		len(packed), len(resp.Answer), resp.Truncated)

	if len(resp.Answer) != 48 {
		t.Errorf("expected 48 answers, got %d", len(resp.Answer))
	}
	if resp.Truncated {
		t.Errorf("TC=1 unexpected at n=48 with 4096 buffer")
	}
	if len(packed) >= 4096 {
		t.Errorf("packed size %d >= 4096; truncation imminent", len(packed))
	}
}

// TestStressSharedBucket_TruncationBreakpoint walks n from 40 upward until
// UDP EDNS0 4096 starts setting TC=1, reporting the exact breakpoint and
// wire size at each step. Answers the question "how many records before
// production hits truncation?"
func TestStressSharedBucket_TruncationBreakpoint(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	// Grow the store one record at a time; check the UDP EDNS0 4096 response
	// after each Put. Stop after the first TC=1 or n=80, whichever comes first.
	for n := 1; n <= 80; n++ {
		store.Put(stressFQDN, challengeValue(t), stressValueTTL)
		resp := queryEDNS(t, udpAddr(srv), stressFQDN, dns.TypeTXT, 4096)
		packed, err := resp.Pack()
		if err != nil {
			t.Fatalf("Pack at n=%d: %v", n, err)
		}
		// Log every 4 steps and near the breakpoint.
		if n <= 4 || n%4 == 0 || resp.Truncated {
			t.Logf("n=%d packed=%d answers=%d TC=%v",
				n, len(packed), len(resp.Answer), resp.Truncated)
		}
		if resp.Truncated {
			t.Logf("→ TRUNCATION BREAKPOINT: n=%d (packed=%d, answers=%d)",
				n, len(packed), len(resp.Answer))
			return
		}
	}
	t.Logf("no truncation observed up to n=80")
}

// TestStressSharedBucket_ProductionFQDNLength repeats the wire-size check
// using a longer shared-bucket FQDN to verify whether the extra label bytes
// versus the test fixture shift the truncation breakpoint.
func TestStressSharedBucket_ProductionFQDNLength(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	// shared-bucket.test is NOT in the fixture; the test exercises only the
	// ephemeral overlay + wire packing, so the zone-lookup path is
	// irrelevant — the ephemeral store returns records for any qname with
	// entries, regardless of zone membership. (See lookupEphemeralTXT.)
	const prodFQDN = "_acme-challenge.shared-bucket.test."

	for n := 1; n <= 80; n++ {
		store.Put(prodFQDN, challengeValue(t), stressValueTTL)
		resp := queryEDNS(t, udpAddr(srv), prodFQDN, dns.TypeTXT, 4096)
		packed, err := resp.Pack()
		if err != nil {
			t.Fatalf("Pack at n=%d: %v", n, err)
		}
		if n <= 4 || n%4 == 0 || resp.Truncated {
			t.Logf("prod-fqdn n=%d packed=%d answers=%d TC=%v",
				n, len(packed), len(resp.Answer), resp.Truncated)
		}
		if resp.Truncated {
			t.Logf("→ PRODUCTION-FQDN TRUNCATION BREAKPOINT: n=%d", n)
			return
		}
	}
	t.Logf("prod-fqdn no truncation up to n=80")
}
