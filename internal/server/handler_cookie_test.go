package server

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net/netip"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/cookie"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// testClientCookie is a fixed 8-byte client cookie used across cookie tests
// (hex form: 2464c4abcf10c957, from RFC 9018 Appendix A).
func testClientCookie(t *testing.T) [cookie.ClientCookieLen]byte {
	t.Helper()
	var cc [cookie.ClientCookieLen]byte
	b, err := hex.DecodeString("2464c4abcf10c957")
	if err != nil {
		t.Fatalf("decode client cookie: %v", err)
	}
	copy(cc[:], b)
	return cc
}

// optTestState builds the standard one-zone test state used by cookie tests.
func optTestState() ServerState {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "192.0.2.1", 300))
	return ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}
}

// TestCookieSecret_SurvivesSwapState asserts the spec scenario "Secret
// survives SIGHUP": the hash segment for identical inputs is unchanged by a
// state swap (the secret lives on Server, not in the reload snapshot).
func TestCookieSecret_SurvivesSwapState(t *testing.T) {
	srv := NewServer(optTestState(), nil)
	srv.gcHook = func() {} // avoid forced GC in test

	cc := testClientCookie(t)
	ip := netip.MustParseAddr("198.51.100.100")
	const ts int64 = 1559731985

	before := srv.cookieGen.Generate(cc, ip, ts)
	srv.SwapState(optTestState())
	after := srv.cookieGen.Generate(cc, ip, ts)

	if !bytes.Equal(before[:], after[:]) {
		t.Errorf("server cookie changed across SwapState:\nbefore %x\nafter  %x", before, after)
	}
}

// TestCookieSecret_DiffersAcrossInstances asserts the spec scenario "Secret
// changes across restarts": two independent Server instances (simulating a
// process restart) hash the same inputs to different values.
func TestCookieSecret_DiffersAcrossInstances(t *testing.T) {
	srv1 := NewServer(optTestState(), nil)
	srv2 := NewServer(optTestState(), nil)

	cc := testClientCookie(t)
	ip := netip.MustParseAddr("198.51.100.100")
	const ts int64 = 1559731985

	c1 := srv1.cookieGen.Generate(cc, ip, ts)
	c2 := srv2.cookieGen.Generate(cc, ip, ts)

	if bytes.Equal(c1[16:24], c2[16:24]) {
		t.Errorf("hash segments identical across independent instances: %x", c1[16:24])
	}
}

// TestCookieSecret_StaleCookieAnsweredWithFreshCookie covers the spec example
// "stale cookie after restart": a query carrying a full cookie issued by a
// previous server instance is answered normally, and the response carries the
// echoed client cookie plus a fresh server cookie whose hash differs.
func TestCookieSecret_StaleCookieAnsweredWithFreshCookie(t *testing.T) {
	oldSrv := NewServer(optTestState(), nil)
	cc := testClientCookie(t)
	ip := netip.MustParseAddr("127.0.0.1")
	staleCookie := oldSrv.cookieGen.Generate(cc, ip, 1559731985)

	newSrv := NewServer(optTestState(), nil) // simulated restart: new secret
	udpAddr, _, cancel := startTestServer(t, newSrv)
	defer cancel()

	req := ednsQuery("www.root.com.", dns.TypeA, 4096)
	addCookie(req, hex.EncodeToString(staleCookie[:]))
	resp := exchange(t, "udp", udpAddr, req)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("stale-cookie query: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("stale-cookie query: expected 1 answer, got %d", len(resp.Answer))
	}

	got := responseCookie(t, resp)
	if len(got) != cookie.FullCookieLen {
		t.Fatalf("response COOKIE option length = %d, want %d", len(got), cookie.FullCookieLen)
	}
	if !bytes.Equal(got[:8], cc[:]) {
		t.Errorf("client cookie not echoed: got %x want %x", got[:8], cc)
	}
	if bytes.Equal(got[16:24], staleCookie[16:24]) {
		t.Errorf("hash segment identical to stale cookie; expected fresh server cookie")
	}
}

// responseCookie extracts the raw bytes of the single COOKIE option in the
// response OPT record, fatally failing when absent or duplicated.
func responseCookie(t *testing.T, resp *dns.Msg) []byte {
	t.Helper()
	opt := resp.IsEdns0()
	if opt == nil {
		t.Fatal("response has no OPT record")
	}
	var cookies []*dns.EDNS0_COOKIE
	for _, o := range opt.Option {
		if c, ok := o.(*dns.EDNS0_COOKIE); ok {
			cookies = append(cookies, c)
		}
	}
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one COOKIE option in response, got %d", len(cookies))
	}
	raw, err := hex.DecodeString(cookies[0].Cookie)
	if err != nil {
		t.Fatalf("response cookie is not valid hex: %v", err)
	}
	return raw
}

// ---------------------------------------------------------------------------
// Cookie response assembly (spec: Answer queries carrying a well-formed
// COOKIE option with a complete server cookie)
// ---------------------------------------------------------------------------

// TestCookieResponse_WellFormedCookies asserts that both client-only and
// full-cookie queries receive a 24-byte COOKIE option echoing the client
// cookie, with rcode and Answer identical to a cookie-less EDNS query.
func TestCookieResponse_WellFormedCookies(t *testing.T) {
	srv := NewServer(optTestState(), nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	// Reference: same question with OPT but no COOKIE option.
	ref := exchange(t, "udp", udpAddr, ednsQuery("www.root.com.", dns.TypeA, 4096))

	cases := []struct {
		name      string
		hexCookie string
	}{
		{"client-only 8-byte", "2464c4abcf10c957"},
		{"full 24-byte", "2464c4abcf10c957010000005cf79f111f8130c3eee29480"},
		{"full 40-byte", "2464c4abcf10c957" + "0100000000000000000000000000000000000000000000000000000000000000"},
	}
	cc := testClientCookie(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ednsQuery("www.root.com.", dns.TypeA, 4096)
			addCookie(req, tc.hexCookie)
			resp := exchange(t, "udp", udpAddr, req)

			if resp.Rcode != ref.Rcode {
				t.Errorf("rcode = %s, want %s (same as cookie-less query)",
					dns.RcodeToString[resp.Rcode], dns.RcodeToString[ref.Rcode])
			}
			if len(resp.Answer) != len(ref.Answer) {
				t.Errorf("answer count = %d, want %d (same as cookie-less query)",
					len(resp.Answer), len(ref.Answer))
			}
			got := responseCookie(t, resp)
			if len(got) != cookie.FullCookieLen {
				t.Fatalf("COOKIE option length = %d, want %d", len(got), cookie.FullCookieLen)
			}
			if !bytes.Equal(got[:8], cc[:]) {
				t.Errorf("client cookie not echoed unmodified: got %x", got[:8])
			}
		})
	}
}

// TestCookieResponse_TwoCookieOptionsFirstWins asserts the spec scenario
// "Multiple COOKIE options — first wins": no FORMERR, exactly one COOKIE
// option built from the first client cookie.
func TestCookieResponse_TwoCookieOptionsFirstWins(t *testing.T) {
	srv := NewServer(optTestState(), nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	req := ednsQuery("www.root.com.", dns.TypeA, 4096)
	addCookie(req, "2464c4abcf10c957") // first: valid 8-byte
	addCookie(req, "fc93fc62807ddb")   // second: malformed 7-byte
	resp := exchange(t, "udp", udpAddr, req)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR (first cookie wins), got %s", dns.RcodeToString[resp.Rcode])
	}
	cc := testClientCookie(t)
	got := responseCookie(t, resp) // fatally fails unless exactly one COOKIE option
	if !bytes.Equal(got[:8], cc[:]) {
		t.Errorf("response cookie not built from first option: got client part %x", got[:8])
	}
}

// TestCookieResponse_LengthBoundaries is the parameterized test derived from
// the spec example "length boundary table" (raw byte lengths; the hex string
// handed to miekg/dns is twice as long).
func TestCookieResponse_LengthBoundaries(t *testing.T) {
	srv := NewServer(optTestState(), nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	cases := []struct {
		rawLen      int
		wantFORMERR bool
	}{
		{7, true},
		{8, false},
		{9, true},
		{15, true},
		{16, false},
		{40, false},
		{41, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("rawLen=%d", tc.rawLen), func(t *testing.T) {
			payload := bytes.Repeat([]byte{0xab}, tc.rawLen)
			req := ednsQuery("www.root.com.", dns.TypeA, 4096)
			addCookie(req, hex.EncodeToString(payload))
			resp := exchange(t, "udp", udpAddr, req)

			if tc.wantFORMERR {
				if resp.Rcode != dns.RcodeFormatError {
					t.Fatalf("expected FORMERR, got %s", dns.RcodeToString[resp.Rcode])
				}
				// FORMERR response must carry OPT but no COOKIE option.
				assertOPTEcho(t, resp)
				if hasCookieOption(resp) {
					t.Error("FORMERR response must not contain a COOKIE option")
				}
				return
			}
			if resp.Rcode != dns.RcodeSuccess {
				t.Fatalf("expected NOERROR with full cookie, got %s", dns.RcodeToString[resp.Rcode])
			}
			got := responseCookie(t, resp)
			if len(got) != cookie.FullCookieLen {
				t.Errorf("COOKIE option length = %d, want %d", len(got), cookie.FullCookieLen)
			}
			if !bytes.Equal(got[:8], payload[:8]) {
				t.Errorf("client cookie not echoed: got %x want %x", got[:8], payload[:8])
			}
		})
	}
}

// TestCookieless_QueriesUnchanged asserts the spec requirement "Queries
// without a COOKIE option are answered unchanged": no COOKIE option in the
// response, never BADCOOKIE, and (for the EDNS case) rcode/Answer/Authority
// identical to the same question without any OPT record.
func TestCookieless_QueriesUnchanged(t *testing.T) {
	srv := NewServer(optTestState(), nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	// Reference: same question without any OPT record.
	plainReq := new(dns.Msg)
	plainReq.SetQuestion("www.root.com.", dns.TypeA)
	plainReq.RecursionDesired = false
	plain := exchange(t, "udp", udpAddr, plainReq)

	t.Run("EDNS without COOKIE", func(t *testing.T) {
		resp := exchange(t, "udp", udpAddr, ednsQuery("www.root.com.", dns.TypeA, 4096))
		if hasCookieOption(resp) {
			t.Error("response contains a COOKIE option for a cookie-less query")
		}
		if resp.Rcode == dns.RcodeBadCookie {
			t.Error("server must never emit BADCOOKIE")
		}
		if resp.Rcode != plain.Rcode {
			t.Errorf("rcode = %s, want %s (same as no-OPT query)",
				dns.RcodeToString[resp.Rcode], dns.RcodeToString[plain.Rcode])
		}
		if len(resp.Answer) != len(plain.Answer) || resp.Answer[0].String() != plain.Answer[0].String() {
			t.Errorf("Answer differs from no-OPT query:\ngot  %v\nwant %v", resp.Answer, plain.Answer)
		}
		if len(resp.Ns) != len(plain.Ns) {
			t.Errorf("Authority section differs from no-OPT query: got %d records, want %d",
				len(resp.Ns), len(plain.Ns))
		}
	})

	t.Run("no EDNS at all", func(t *testing.T) {
		if hasCookieOption(plain) {
			t.Error("response contains a COOKIE option for a non-EDNS query")
		}
		if plain.Rcode == dns.RcodeBadCookie {
			t.Error("server must never emit BADCOOKIE")
		}
	})
}
