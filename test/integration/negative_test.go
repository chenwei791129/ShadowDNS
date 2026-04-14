// All queries use loopback (127.0.0.1) → view-other.
// Tests verify:
//   - NXDOMAIN contains SOA in authority section (RFC 2308)
//   - NODATA (RCODE=NOERROR, empty answer) contains SOA in authority section
//   - Backup zone NXDOMAIN returns SOA with backup origin
//   - Authority SOA TTL is min(SOA.Hdr.Ttl, SOA.Minttl)
package integration_test

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// TestNegative_NXDOMAIN_RootZone verifies that a query for a nonexistent name
// in the root zone returns RCODE=NXDOMAIN with SOA(example.com.) in authority.
func TestNegative_NXDOMAIN_RootZone(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "nonexistent.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected empty answer section for NXDOMAIN, got: %v", resp.Answer)
	}

	soa := assertAuthoritySOA(t, resp, "example.com.")
	if soa != nil {
		// Authority SOA TTL must be min(SOA.Hdr.Ttl, SOA.Minttl).
		// The fixture has $TTL 300 and Minttl 300, so capped TTL = 300.
		if soa.Hdr.Ttl > soa.Minttl {
			t.Errorf("authority SOA TTL %d exceeds Minttl %d; must be capped", soa.Hdr.Ttl, soa.Minttl)
		}
		expectedTTL := soa.Minttl
		if soa.Hdr.Ttl > expectedTTL {
			t.Errorf("expected authority SOA TTL ≤ %d, got %d", expectedTTL, soa.Hdr.Ttl)
		}
	}

	if resp.Authoritative != true {
		t.Error("expected AA=1 on NXDOMAIN")
	}
}

// TestNegative_NODATA_RootZone verifies that a query for ns1.example.com AAAA
// returns RCODE=NOERROR with empty answer and SOA in authority (ns1 has A but no AAAA).
func TestNegative_NODATA_RootZone(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "ns1.example.com.", dns.TypeAAAA)

	// NODATA: name exists (ns1 has A), but no AAAA record.
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR (NODATA), got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected empty answer section for NODATA, got: %v", resp.Answer)
	}

	soa := assertAuthoritySOA(t, resp, "example.com.")
	if soa != nil {
		// Capped TTL per RFC 2308.
		if soa.Hdr.Ttl > soa.Minttl {
			t.Errorf("authority SOA TTL %d exceeds Minttl %d; must be capped", soa.Hdr.Ttl, soa.Minttl)
		}
	}
}

// TestNegative_NXDOMAIN_BackupZone verifies that a query for a nonexistent name
// in the backup zone returns RCODE=NXDOMAIN with SOA(backup.example.) in authority.
func TestNegative_NXDOMAIN_BackupZone(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "nonexistent.backup.example.", dns.TypeA)

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected empty answer for NXDOMAIN, got: %v", resp.Answer)
	}

	// Authority SOA must have the backup.example. owner (rewritten).
	soa := assertAuthoritySOA(t, resp, "backup.example.")
	if soa != nil {
		// MNAME must also be rewritten.
		if !strings.EqualFold(soa.Ns, "ns1.backup.example.") {
			t.Errorf("expected MNAME ns1.backup.example., got %s", soa.Ns)
		}
		// TTL cap.
		if soa.Hdr.Ttl > soa.Minttl {
			t.Errorf("authority SOA TTL %d exceeds Minttl %d; must be capped", soa.Hdr.Ttl, soa.Minttl)
		}
	}

	if resp.Authoritative != true {
		t.Error("expected AA=1 on NXDOMAIN")
	}
}

// TestNegative_SOA_TTL_Capped verifies the RFC 2308 TTL cap on the authority SOA
// for a root-zone NXDOMAIN.  With $TTL 300 and SOA Minttl=300, the capped TTL is 300.
func TestNegative_SOA_TTL_Capped(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "no-such-name.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}

	soa := assertAuthoritySOA(t, resp, "example.com.")
	if soa == nil {
		return // error already reported by assertAuthoritySOA
	}

	// The fixture SOA has Hdr.Ttl=3600 and Minttl=300; the server caps to 300.
	const expectedCap = uint32(300)
	if soa.Hdr.Ttl > expectedCap {
		t.Errorf("authority SOA TTL should be capped to %d, got %d", expectedCap, soa.Hdr.Ttl)
	}
}
