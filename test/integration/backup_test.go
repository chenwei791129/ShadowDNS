//
// All queries use loopback (127.0.0.1) → view-other.
// Tests verify:
//   - Owner names are rewritten to backup namespace
//   - In-bailiwick CNAME targets are rewritten
//   - Third-party CNAME targets are preserved
//   - TXT and MX use override values from the backup zone file
//   - Backup SOA is correctly synthesised
package integration_test

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// TestBackup_A verifies that www.backup.example returns A 198.51.100.30
// (inherited from root, owner rewritten to www.backup.example).
func TestBackup_A(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "www.backup.example.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	// Owner must be under the backup namespace.
	assertHasA(t, resp, "www.backup.example.", "198.51.100.30")
}

// TestBackup_CNAME_InBailiwick verifies that api.backup.example returns
// CNAME www.backup.example (in-bailiwick rewrite of www.example.com).
func TestBackup_CNAME_InBailiwick(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "api.backup.example.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	// Target must be rewritten to backup namespace.
	assertHasCNAME(t, resp, "api.backup.example.", "www.backup.example.")
}

// TestBackup_CNAME_ThirdParty verifies that cdn.backup.example returns
// CNAME d222222abcdef8.cloudfront.net (third-party target preserved, not rewritten).
func TestBackup_CNAME_ThirdParty(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "cdn.backup.example.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	// Third-party CDN target must not be rewritten.
	assertHasCNAME(t, resp, "cdn.backup.example.", "d222222abcdef8.cloudfront.net.")
}

// TestBackup_TXT_Override verifies that backup.example TXT returns the override
// record (google-site-verification=BACKUP_VIEW_OTHER_VERIFY_TOKEN), NOT the
// inherited SPF TXT from the root zone.
func TestBackup_TXT_Override(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "backup.example.", dns.TypeTXT)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	if len(resp.Answer) == 0 {
		t.Fatal("expected TXT records in answer")
	}

	overrideFound := false
	spfFound := false
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			for _, s := range txt.Txt {
				if strings.Contains(s, "BACKUP_VIEW_OTHER_VERIFY_TOKEN") {
					overrideFound = true
				}
				if strings.HasPrefix(s, "v=spf1") {
					spfFound = true
				}
			}
		}
	}
	if !overrideFound {
		t.Errorf("expected override TXT (BACKUP_VIEW_OTHER_VERIFY_TOKEN); got: %v", resp.Answer)
	}
	if spfFound {
		t.Errorf("expected root SPF TXT to be suppressed by override; got: %v", resp.Answer)
	}
}

// TestBackup_MX_Override verifies that backup.example MX returns the override
// record (MX 20 mx-backup.example.net.) from the backup zone file.
func TestBackup_MX_Override(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "backup.example.", dns.TypeMX)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	if len(resp.Answer) == 0 {
		t.Fatal("expected MX records in answer")
	}

	found := false
	for _, rr := range resp.Answer {
		if mx, ok := rr.(*dns.MX); ok {
			if mx.Preference == 20 && strings.EqualFold(mx.Mx, "mx-backup.example.net.") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected MX 20 mx-backup.example.net.; got: %v", resp.Answer)
	}
}

// TestBackup_SOA verifies that a SOA query for backup.example returns a
// synthesised SOA:
//   - Owner: backup.example.
//   - MNAME: ns1.backup.example. (rewritten from ns1.example.com.)
//   - Serial: 2024010101 (inherited from root zone)
func TestBackup_SOA(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "backup.example.", dns.TypeSOA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	if len(resp.Answer) == 0 {
		t.Fatal("expected SOA in answer section")
	}

	soaFound := false
	for _, rr := range resp.Answer {
		if soa, ok := rr.(*dns.SOA); ok {
			if !strings.EqualFold(soa.Hdr.Name, "backup.example.") {
				t.Errorf("expected SOA owner backup.example., got %s", soa.Hdr.Name)
			}
			if !strings.EqualFold(soa.Ns, "ns1.backup.example.") {
				t.Errorf("expected MNAME ns1.backup.example., got %s", soa.Ns)
			}
			// Serial is inherited from root zone (2024010101).
			if soa.Serial != 2024010101 {
				t.Errorf("expected serial 2024010101, got %d", soa.Serial)
			}
			soaFound = true
			break
		}
	}
	if !soaFound {
		t.Errorf("no SOA found in answer; got: %v", resp.Answer)
	}
}
