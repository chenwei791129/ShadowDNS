// Wildcard matching integration tests (RFC 4592).
//
// All queries use loopback (127.0.0.1) -> view-other.
// Zone fixtures include:
//   - *.example.com.     A     10.99.99.1
//   - *.sub.example.com. CNAME target.example.com.
//   - ent.example.com.   TXT   "blocker"  (ENT for blocking tests)
package integration_test

import (
	"testing"

	"github.com/miekg/dns"
)

// TestWildcard_A verifies that a query for a non-existent single-level
// subdomain matches the zone wildcard and returns the synthesized A record
// with the original qname as owner (not the "*" label).
func TestWildcard_A(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "anything.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 1)
	assertHasA(t, resp, "anything.example.com.", "10.99.99.1")
}

// TestWildcard_CNAME_Synthesis verifies that a wildcard CNAME is returned
// when querying a non-CNAME type for a name matching *.sub.example.com.,
// and that in-zone CNAME following resolves the target via wildcard A.
func TestWildcard_CNAME_Synthesis(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "foo.sub.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 2)
	assertHasCNAME(t, resp, "foo.sub.example.com.", "target.example.com.")
	// target.example.com. has no exact A; resolved via *.example.com. wildcard.
	assertHasA(t, resp, "target.example.com.", "10.99.99.1")
}

// TestWildcard_ENTBlocking verifies that an ENT (ent.example.com.) prevents
// wildcard matching for names underneath it, resulting in NXDOMAIN.
func TestWildcard_ENTBlocking(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "blocked.ent.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN (ENT blocking), got %s", dns.RcodeToString[resp.Rcode])
	}
	assertAuthoritySOA(t, resp, "example.com.")
}

// TestWildcard_BackupZone verifies that wildcard matching works through the
// backup (alias) zone, with the owner name rewritten to the backup namespace.
func TestWildcard_BackupZone(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "anything.backup.example.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 1)
	assertHasA(t, resp, "anything.backup.example.", "10.99.99.1")
}

// TestWildcard_ExactMatchPrecedence verifies that an exact record
// (www.example.com.) is returned instead of the wildcard.
func TestWildcard_ExactMatchPrecedence(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "www.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 1)
	// Must be the exact record (198.51.100.30), not the wildcard (10.99.99.1).
	assertHasA(t, resp, "www.example.com.", "198.51.100.30")
}

// TestWildcard_NODATA verifies that querying a wildcard-matched name for a
// type not present at the wildcard returns NODATA (NOERROR + empty answer).
func TestWildcard_NODATA(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	// *.example.com. has only A; querying AAAA -> NODATA.
	resp := queryUDP(t, addr, "anything.example.com.", dns.TypeAAAA)

	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR (wildcard NODATA), got %s", dns.RcodeToString[resp.Rcode])
	}
	assertAuthoritative(t, resp)
	if len(resp.Answer) != 0 {
		t.Errorf("expected empty answer for wildcard NODATA, got %d records", len(resp.Answer))
	}
	assertAuthoritySOA(t, resp, "example.com.")
}
