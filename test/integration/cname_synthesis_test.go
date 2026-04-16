// CNAME synthesis integration tests (RFC 1034 §3.6.2).
//
// All queries use loopback (127.0.0.1) → view-other.
// These tests verify that querying a non-CNAME type for a name that only has a
// CNAME record returns the CNAME in the answer section instead of NODATA.
package integration_test

import (
	"testing"

	"github.com/miekg/dns"
)

// TestCNAMESynthesis_RootZone_A verifies that an A query for
// api.example.com (which has only a CNAME) returns the CNAME record
// followed by the in-zone target's A record (RFC 1034 §3.6.2).
func TestCNAMESynthesis_RootZone_A(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "api.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 2)
	assertHasCNAME(t, resp, "api.example.com.", "www.example.com.")
	assertHasA(t, resp, "www.example.com.", "198.51.100.30")
}

// TestCNAMESynthesis_RootZone_ExplicitCNAME verifies that an explicit CNAME
// query still works normally (regression guard).
func TestCNAMESynthesis_RootZone_ExplicitCNAME(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "api.example.com.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasCNAME(t, resp, "api.example.com.", "www.example.com.")
}

// TestCNAMESynthesis_BackupZone_A verifies that an A query for
// api.backup.example (backup of example.com) returns the CNAME with
// the owner name rewritten to the backup namespace, followed by the
// in-zone target's A record also rewritten.
func TestCNAMESynthesis_BackupZone_A(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "api.backup.example.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 2)
	// Owner rewritten to backup namespace; in-bailiwick target also rewritten.
	assertHasCNAME(t, resp, "api.backup.example.", "www.backup.example.")
	assertHasA(t, resp, "www.backup.example.", "198.51.100.30")
}
