// All tests use loopback (127.0.0.1) as source IP, which is matched by
// view-other's "any" rule.  Per-view routing is covered by unit tests in
// internal/server/server_test.go.
package integration_test

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// TestQuery_A verifies that a query for www.example.com returns A 198.51.100.30
// (view-other's value).
func TestQuery_A(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "www.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasA(t, resp, "www.example.com.", "198.51.100.30")
}

// TestQuery_AAAA verifies that a query for www.example.com returns AAAA 2001:db8:1::30.
func TestQuery_AAAA(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "www.example.com.", dns.TypeAAAA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasAAAA(t, resp, "www.example.com.", "2001:db8:1::30")
}

// TestQuery_CNAME_InBailiwick verifies that api.example.com returns
// CNAME www.example.com (in-bailiwick).
func TestQuery_CNAME_InBailiwick(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "api.example.com.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasCNAME(t, resp, "api.example.com.", "www.example.com.")
}

// TestQuery_CNAME_ThirdParty verifies that cdn.example.com returns
// CNAME d222222abcdef8.cloudfront.net (third-party target preserved).
func TestQuery_CNAME_ThirdParty(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "cdn.example.com.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasCNAME(t, resp, "cdn.example.com.", "d222222abcdef8.cloudfront.net.")
}

// TestQuery_NS verifies that example.com returns (at least) 2 NS records.
func TestQuery_NS(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "example.com.", dns.TypeNS)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)

	nsCount := 0
	for _, rr := range resp.Answer {
		if rr.Header().Rrtype == dns.TypeNS {
			nsCount++
		}
	}
	if nsCount < 2 {
		t.Errorf("expected at least 2 NS records, got %d (answer: %v)", nsCount, resp.Answer)
	}
}

// TestQuery_MX verifies that example.com MX returns "10 mx1.example.com".
func TestQuery_MX(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "example.com.", dns.TypeMX)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	if len(resp.Answer) == 0 {
		t.Fatal("expected at least one MX record in answer")
	}

	found := false
	for _, rr := range resp.Answer {
		if mx, ok := rr.(*dns.MX); ok {
			if mx.Preference == 10 && strings.EqualFold(mx.Mx, "mx1.example.com.") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected MX 10 mx1.example.com.; got: %v", resp.Answer)
	}
}

// TestQuery_TXT verifies that example.com TXT returns the SPF record.
func TestQuery_TXT(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "example.com.", dns.TypeTXT)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	if len(resp.Answer) == 0 {
		t.Fatal("expected at least one TXT record in answer")
	}

	spfFound := false
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			for _, s := range txt.Txt {
				if strings.HasPrefix(s, "v=spf1") {
					spfFound = true
					break
				}
			}
		}
	}
	if !spfFound {
		t.Errorf("expected SPF TXT record; got: %v", resp.Answer)
	}
}

// TestQuery_Apex_CNAME_TXT_Coexist verifies Cloudflare-style coexistence at
// the zone apex: when example.com. has both a CNAME and other record types
// (TXT, A) at the apex, exact-match-first resolution applies — TXT and A
// queries return their static records, while a CNAME query returns the apex
// CNAME without following the target.
//
// Locks the dns-server spec scenario "Static zone record at the same owner as
// a CNAME wins over CNAME synthesis (Cloudflare-style coexistence)".
//
// Loopback queries hit view-other per the package header comment, so
// assertions use the view-other apex values (198.51.100.x).
func TestQuery_Apex_CNAME_TXT_Coexist(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	t.Run("TXT_returns_SPF", func(t *testing.T) {
		resp := queryUDP(t, addr, "example.com.", dns.TypeTXT)
		assertNoError(t, resp)
		assertAuthoritative(t, resp)
		if len(resp.Answer) == 0 {
			t.Fatal("expected at least one TXT record in answer")
		}
		spfFound := false
	loop:
		for _, rr := range resp.Answer {
			txt, ok := rr.(*dns.TXT)
			if !ok {
				continue
			}
			for _, s := range txt.Txt {
				if s == "v=spf1 ip4:198.51.100.0/24 -all" {
					spfFound = true
					break loop
				}
			}
		}
		if !spfFound {
			t.Errorf("expected exact SPF TXT %q at apex; got: %v",
				"v=spf1 ip4:198.51.100.0/24 -all", resp.Answer)
		}
		for _, rr := range resp.Answer {
			if _, ok := rr.(*dns.CNAME); ok {
				t.Errorf("apex TXT response must not contain CNAME RR; got: %v", resp.Answer)
			}
		}
	})

	t.Run("CNAME_returns_apex_CNAME", func(t *testing.T) {
		resp := queryUDP(t, addr, "example.com.", dns.TypeCNAME)
		assertNoError(t, resp)
		assertAuthoritative(t, resp)
		assertAnswerCount(t, resp, 1)
		assertHasCNAME(t, resp, "example.com.", "www.example.com.")
	})

	t.Run("A_returns_apex_A_no_CNAME", func(t *testing.T) {
		resp := queryUDP(t, addr, "example.com.", dns.TypeA)
		assertNoError(t, resp)
		assertAuthoritative(t, resp)
		assertHasA(t, resp, "example.com.", "198.51.100.10")
		for _, rr := range resp.Answer {
			if _, ok := rr.(*dns.CNAME); ok {
				t.Errorf("apex A response must not contain CNAME RR; got: %v", resp.Answer)
			}
		}
	})

	// AAAA at apex coexists with CNAME in this fixture, so exact-match-first
	// applies the same way as the A case. This locks the fixture's actual
	// apex-record-set behaviour. The spec table's "no apex AAAA → CNAME
	// synthesis fires" row would need a different fixture (no apex AAAA) and
	// is out of scope for this change.
	t.Run("AAAA_returns_apex_AAAA_no_CNAME", func(t *testing.T) {
		resp := queryUDP(t, addr, "example.com.", dns.TypeAAAA)
		assertNoError(t, resp)
		assertAuthoritative(t, resp)
		assertHasAAAA(t, resp, "example.com.", "2001:db8:1::10")
		for _, rr := range resp.Answer {
			if _, ok := rr.(*dns.CNAME); ok {
				t.Errorf("apex AAAA response must not contain CNAME RR; got: %v", resp.Answer)
			}
		}
	})
}

// TestQuery_A_TCP verifies that the same A query delivered over TCP
// returns the expected record — exercises the TCP listener end-to-end
// for non-AXFR traffic (spec dns-server Requirement: serve over UDP and TCP).
func TestQuery_A_TCP(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := tcpAddr(srv)

	resp := queryTCP(t, addr, "www.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertAnswerCount(t, resp, 1)
	assertHasA(t, resp, "www.example.com.", "198.51.100.30")
	if resp.Truncated {
		t.Error("TCP response must not have TC=1")
	}
}

// TestQuery_NS_TCP verifies an NS query over TCP returns the expected
// answer set.  assertAnswerCount guards against accidental glue leaking
// into the answer section.
func TestQuery_NS_TCP(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := tcpAddr(srv)

	resp := queryTCP(t, addr, "example.com.", dns.TypeNS)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)

	nsCount := 0
	for _, rr := range resp.Answer {
		if rr.Header().Rrtype == dns.TypeNS {
			nsCount++
		}
	}
	if nsCount < 2 {
		t.Errorf("expected at least 2 NS records over TCP, got %d", nsCount)
	}
	// Every answer record must be NS — no extra types mixed in.
	assertAnswerCount(t, resp, nsCount)
}

// TestQuery_QuotedInclude_LoadsCNAME verifies the BIND-compatible
// `$include "path"` directive end-to-end: a zone file using the quoted
// form pulls in a CNAME fragment, and the included record is queryable
// over DNS through the running server.
//
// Fixture wiring:
//   - testdata/integration/db.include-test.example has both quoted
//     and bare $include directives pointing at the same fragment
//   - testdata/integration/cnames/db.example.com.cname provides
//     three CNAMEs (alias, mail, help) under origin include-test.example.
//   - testdata/integration/named.conf.local registers the zone in view-other
//     so loopback queries reach it
func TestQuery_QuotedInclude_LoadsCNAME(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "alias.include-test.example.", dns.TypeCNAME)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasCNAME(t, resp, "alias.include-test.example.", "www.example.com.")
}

// TestQuery_SOA verifies that an explicit SOA query returns the SOA in the
// answer section with AA=1.
func TestQuery_SOA(t *testing.T) {
	srv, cancel := newTestServer(t)
	defer cancel()
	addr := udpAddr(srv)

	resp := queryUDP(t, addr, "example.com.", dns.TypeSOA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	if len(resp.Answer) == 0 {
		t.Fatal("expected SOA in answer section")
	}

	soaFound := false
	for _, rr := range resp.Answer {
		if soa, ok := rr.(*dns.SOA); ok {
			if strings.EqualFold(soa.Hdr.Name, "example.com.") {
				soaFound = true
				break
			}
		}
	}
	if !soaFound {
		t.Errorf("expected SOA for example.com. in answer section; got: %v", resp.Answer)
	}
}
