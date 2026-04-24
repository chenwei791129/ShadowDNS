// Ephemeral TXT-overrides-exact-CNAME integration tests.
//
// Exercises the end-to-end DNS path over both UDP and TCP for the case
// where a zone has an exact CNAME at a qname (e.g. ACME DNS-01 delegation
// to an external acme-dns) and the ephemeral store holds a live TXT entry
// for the same qname. TXT queries must be answered from the ephemeral
// store; CNAME queries must still return the zone CNAME.
package integration_test

import (
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/server"
)

// TestEphemeralOverrideCNAME_TXTReturnsEphemeralOverBothTransports verifies
// that a TXT query at a qname with an exact zone CNAME returns the ephemeral
// TXT (and nothing else) on both UDP and TCP.
func TestEphemeralOverrideCNAME_TXTReturnsEphemeralOverBothTransports(t *testing.T) {
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.example.com.", "token-xyz", 120)
	srv, cancel := newTestServerWithEphemeral(t, store)
	defer cancel()

	cases := []struct {
		name    string
		doQuery func(t *testing.T, addr, qname string, qtype uint16) *dns.Msg
		addr    string
	}{
		{"udp", queryUDP, udpAddr(srv)},
		{"tcp", queryTCP, tcpAddr(srv)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := tc.doQuery(t, tc.addr, "_acme-challenge.foo.example.com.", dns.TypeTXT)

			assertNoError(t, resp)
			assertAuthoritative(t, resp)
			assertAnswerCount(t, resp, 1)

			txt, ok := resp.Answer[0].(*dns.TXT)
			if !ok {
				t.Fatalf("Answer[0] type = %T, want *dns.TXT (ephemeral must override exact CNAME)", resp.Answer[0])
			}
			if len(txt.Txt) != 1 || txt.Txt[0] != "token-xyz" {
				t.Errorf("TXT value = %v, want [token-xyz]", txt.Txt)
			}
			if txt.Hdr.Name != "_acme-challenge.foo.example.com." {
				t.Errorf("owner name = %q, want _acme-challenge.foo.example.com.", txt.Hdr.Name)
			}
			if txt.Hdr.Ttl != server.EphemeralResponseTTL {
				t.Errorf("RR TTL = %d, want %d (API ttl 120 must not leak into response)", txt.Hdr.Ttl, server.EphemeralResponseTTL)
			}
			for _, rr := range resp.Answer {
				if _, isCNAME := rr.(*dns.CNAME); isCNAME {
					t.Errorf("answer contains CNAME RR %v; CNAME must be suppressed when ephemeral TXT exists", rr)
				}
			}
		})
	}
}

// TestEphemeralOverrideCNAME_CNAMEQueryReturnsZoneCNAME verifies that an
// explicit CNAME query at the same qname still returns the zone CNAME on
// both UDP and TCP — the ephemeral overlay is scoped to TXT qtype.
func TestEphemeralOverrideCNAME_CNAMEQueryReturnsZoneCNAME(t *testing.T) {
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.example.com.", "token-xyz", 120)
	srv, cancel := newTestServerWithEphemeral(t, store)
	defer cancel()

	cases := []struct {
		name    string
		doQuery func(t *testing.T, addr, qname string, qtype uint16) *dns.Msg
		addr    string
	}{
		{"udp", queryUDP, udpAddr(srv)},
		{"tcp", queryTCP, tcpAddr(srv)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := tc.doQuery(t, tc.addr, "_acme-challenge.foo.example.com.", dns.TypeCNAME)

			assertNoError(t, resp)
			assertAuthoritative(t, resp)
			assertAnswerCount(t, resp, 1)
			assertHasCNAME(t, resp, "_acme-challenge.foo.example.com.", "acme-dns.external.net.")

			for _, rr := range resp.Answer {
				if _, isTXT := rr.(*dns.TXT); isTXT {
					t.Errorf("answer contains TXT RR %v; TXT must not leak into CNAME query", rr)
				}
			}
		})
	}
}
