// In-zone CNAME following integration tests (RFC 1034 §3.6.2).
//
// All queries use loopback (127.0.0.1) → view-other.
package integration_test

import (
	"testing"

	"github.com/miekg/dns"
)

// TestCNAMEFollowing_RootZone_SingleHop verifies that an A query for a
// name with an in-zone CNAME target returns both the CNAME and the A record.
func TestCNAMEFollowing_RootZone_SingleHop(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	// mail.example.com. CNAME mx1.example.com. ; mx1 has A 198.51.100.20
	resp := queryUDP(t, addr, "mail.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 2)
	assertHasCNAME(t, resp, "mail.example.com.", "mx1.example.com.")
	assertHasA(t, resp, "mx1.example.com.", "198.51.100.20")
}

// TestCNAMEFollowing_RootZone_Chain verifies that a multi-hop CNAME chain
// within the zone is fully followed.
func TestCNAMEFollowing_RootZone_Chain(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	// hop1 → hop2 → hop3 (A 198.51.100.50)
	resp := queryUDP(t, addr, "hop1.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 3)
	assertHasCNAME(t, resp, "hop1.example.com.", "hop2.example.com.")
	assertHasCNAME(t, resp, "hop2.example.com.", "hop3.example.com.")
	assertHasA(t, resp, "hop3.example.com.", "198.51.100.50")
}

// TestCNAMEFollowing_RootZone_OutOfBailiwick verifies that an out-of-zone
// CNAME target is NOT followed (only the CNAME is returned).
func TestCNAMEFollowing_RootZone_OutOfBailiwick(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	// cdn.example.com. CNAME d222222abcdef8.cloudfront.net. (out-of-bailiwick)
	resp := queryUDP(t, addr, "cdn.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 1)
	assertHasCNAME(t, resp, "cdn.example.com.", "d222222abcdef8.cloudfront.net.")
}

// TestCNAMEFollowing_RootZone_ExplicitCNAME verifies that an explicit
// CNAME query does NOT follow the target.
func TestCNAMEFollowing_RootZone_ExplicitCNAME(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "mail.example.com.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 1)
	assertHasCNAME(t, resp, "mail.example.com.", "mx1.example.com.")
}

// TestCNAMEFollowing_BackupZone verifies that in-zone CNAME following
// works in the backup zone path with correct namespace rewriting.
func TestCNAMEFollowing_BackupZone(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	// mail.backup.example. → mx1.backup.example. (rewritten from root zone)
	resp := queryUDP(t, addr, "mail.backup.example.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 2)
	assertHasCNAME(t, resp, "mail.backup.example.", "mx1.backup.example.")
	assertHasA(t, resp, "mx1.backup.example.", "198.51.100.20")
}

// TestCNAMEFollowing_BackupZone_Chain verifies multi-hop CNAME chain
// following in backup zone with rewriting.
func TestCNAMEFollowing_BackupZone_Chain(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "hop1.backup.example.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 3)
	assertHasCNAME(t, resp, "hop1.backup.example.", "hop2.backup.example.")
	assertHasCNAME(t, resp, "hop2.backup.example.", "hop3.backup.example.")
	assertHasA(t, resp, "hop3.backup.example.", "198.51.100.50")
}

// TestCNAMEFollowing_WildcardCNAME verifies that a wildcard CNAME with
// an in-zone target is followed.
func TestCNAMEFollowing_WildcardCNAME(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	// *.sub.example.com. CNAME target.example.com.
	// target.example.com. has no exact A → wildcard *.example.com. A 10.99.99.1
	resp := queryUDP(t, addr, "bar.sub.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 2)
	assertHasCNAME(t, resp, "bar.sub.example.com.", "target.example.com.")
	assertHasA(t, resp, "target.example.com.", "10.99.99.1")
}
