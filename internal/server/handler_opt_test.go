package server

import (
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// addCookie appends an EDNS0_COOKIE option with the given hex payload to the
// request's OPT record (which must already exist).
func addCookie(req *dns.Msg, hexCookie string) {
	opt := req.IsEdns0()
	opt.Option = append(opt.Option, &dns.EDNS0_COOKIE{
		Code:   dns.EDNS0COOKIE,
		Cookie: hexCookie,
	})
}

func TestParseQueryOpt_NoOPT(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)

	got := parseQueryOpt(req)
	if got.present {
		t.Error("present = true for a query without OPT, want false")
	}
	if got.cookie != nil {
		t.Errorf("cookie = %v for a query without OPT, want nil", got.cookie)
	}
}

func TestParseQueryOpt_FieldsExtracted(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)
	req.SetEdns0(4096, true) // DO=1
	opt := req.IsEdns0()
	opt.SetVersion(1)

	got := parseQueryOpt(req)
	if !got.present {
		t.Fatal("present = false, want true")
	}
	if got.version != 1 {
		t.Errorf("version = %d, want 1", got.version)
	}
	if got.udpSize != 4096 {
		t.Errorf("udpSize = %d, want 4096", got.udpSize)
	}
	if !got.do {
		t.Error("do = false, want true")
	}
	if got.cookie != nil {
		t.Errorf("cookie = %v without COOKIE option, want nil", got.cookie)
	}
}

func TestParseQueryOpt_FirstCookieWins(t *testing.T) {
	// RFC 7873 §5.2: only the first COOKIE option (closest to the DNS
	// header) is processed; the rest are silently ignored.
	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)
	req.SetEdns0(1232, false)
	addCookie(req, "2464c4abcf10c957") // first: valid 8-byte
	addCookie(req, "fc93fc62807ddb")   // second: malformed 7-byte

	got := parseQueryOpt(req)
	if got.cookie == nil {
		t.Fatal("cookie = nil, want first COOKIE option")
	}
	if got.cookie.Cookie != "2464c4abcf10c957" {
		t.Errorf("cookie = %q, want first option %q", got.cookie.Cookie, "2464c4abcf10c957")
	}
}

// ---------------------------------------------------------------------------
// OPT echo integration tests (spec: Responses echo an EDNS0 OPT record when
// the query carries one)
// ---------------------------------------------------------------------------

// newOPTTestServer starts a server with the shared one-zone fixture
// (optTestState), returning udp/tcp addresses and cancel.
func newOPTTestServer(t *testing.T) (udpAddr, tcpAddr string, cancel func()) {
	t.Helper()
	return startTestServer(t, NewServer(optTestState(), nil))
}

// exchange sends req over the given network and returns the response.
func exchange(t *testing.T, network, addr string, req *dns.Msg) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: network, Timeout: 2 * time.Second}
	resp, _, err := c.Exchange(req, addr)
	if err != nil {
		t.Fatalf("exchange via %s to %s: %v", network, addr, err)
	}
	return resp
}

// ednsQuery builds a query for qname/qtype with an EDNS0 OPT record
// advertising the given buffer size.
func ednsQuery(qname string, qtype uint16, bufSize uint16) *dns.Msg {
	req := new(dns.Msg)
	req.SetQuestion(qname, qtype)
	req.RecursionDesired = false
	req.SetEdns0(bufSize, false)
	return req
}

// assertOPTEcho asserts the response contains exactly one OPT record with
// version 0 and UDP payload size 1232.
func assertOPTEcho(t *testing.T, resp *dns.Msg) *dns.OPT {
	t.Helper()
	var opts []*dns.OPT
	for _, rr := range resp.Extra {
		if o, ok := rr.(*dns.OPT); ok {
			opts = append(opts, o)
		}
	}
	if len(opts) != 1 {
		t.Fatalf("expected exactly one OPT record in response, got %d", len(opts))
	}
	opt := opts[0]
	if v := opt.Version(); v != 0 {
		t.Errorf("OPT version = %d, want 0", v)
	}
	if sz := opt.UDPSize(); sz != 1232 {
		t.Errorf("OPT UDP payload size = %d, want 1232", sz)
	}
	return opt
}

func TestOPTEcho_SuccessAnswer(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	resp := exchange(t, "udp", udpAddr, ednsQuery("www.root.com.", dns.TypeA, 4096))
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	assertOPTEcho(t, resp)
}

func TestOPTEcho_NegativeResponses(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	t.Run("NXDOMAIN", func(t *testing.T) {
		resp := exchange(t, "udp", udpAddr, ednsQuery("nx.root.com.", dns.TypeA, 4096))
		if resp.Rcode != dns.RcodeNameError {
			t.Fatalf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
		}
		if len(resp.Ns) == 0 {
			t.Error("expected SOA in authority section")
		}
		assertOPTEcho(t, resp)
	})

	t.Run("NODATA", func(t *testing.T) {
		resp := exchange(t, "udp", udpAddr, ednsQuery("www.root.com.", dns.TypeMX, 4096))
		if resp.Rcode != dns.RcodeSuccess {
			t.Fatalf("expected NOERROR (NODATA), got %s", dns.RcodeToString[resp.Rcode])
		}
		if len(resp.Answer) != 0 {
			t.Error("expected empty answer section")
		}
		assertOPTEcho(t, resp)
	})
}

func TestOPTEcho_ErrorRcode(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	// CHAOS class → REFUSED via replyRcode.
	req := ednsQuery("version.bind.", dns.TypeTXT, 4096)
	req.Question[0].Qclass = dns.ClassCHAOS
	resp := exchange(t, "udp", udpAddr, req)
	if resp.Rcode != dns.RcodeRefused {
		t.Fatalf("expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
	assertOPTEcho(t, resp)
}

func TestOPTEcho_RefusedTransfer(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	// Nil allow-transfer ACL denies all → pre-transfer REFUSED via replyRcode.
	resp := exchange(t, "udp", udpAddr, ednsQuery("root.com.", dns.TypeAXFR, 4096))
	if resp.Rcode != dns.RcodeRefused {
		t.Fatalf("expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
	assertOPTEcho(t, resp)
}

func TestOPTEcho_NoOPTQueryGetsNoOPT(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)
	req.RecursionDesired = false
	resp := exchange(t, "udp", udpAddr, req)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if opt := resp.IsEdns0(); opt != nil {
		t.Errorf("response contains an OPT record for a non-EDNS query: %v", opt)
	}
}

func TestOPTEcho_TCPMatchesUDP(t *testing.T) {
	udpAddr, tcpAddr, cancel := newOPTTestServer(t)
	defer cancel()

	udpResp := exchange(t, "udp", udpAddr, ednsQuery("www.root.com.", dns.TypeA, 4096))
	tcpResp := exchange(t, "tcp", tcpAddr, ednsQuery("www.root.com.", dns.TypeA, 4096))

	udpOpt := assertOPTEcho(t, udpResp)
	tcpOpt := assertOPTEcho(t, tcpResp)
	if udpOpt.Version() != tcpOpt.Version() || udpOpt.UDPSize() != tcpOpt.UDPSize() {
		t.Errorf("TCP OPT (v=%d sz=%d) differs from UDP OPT (v=%d sz=%d)",
			tcpOpt.Version(), tcpOpt.UDPSize(), udpOpt.Version(), udpOpt.UDPSize())
	}
	if tcpResp.Truncated {
		t.Error("TCP response must not be truncated")
	}
}

func TestOPTEcho_PanicRecoverySERVFAIL(t *testing.T) {
	// A nil root SOA makes the backup SOA path panic (same fixture as
	// TestMalformed_PanicRecovery); the recovered SERVFAIL must echo OPT.
	brokenRootZ := &zone.Zone{Origin: "root.com.", Role: zone.RoleRoot, SOA: nil}
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "backup.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": brokenRootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {}},
		Aliases:     config.AliasMap{"backup.com.": "root.com."},
	}, nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := exchange(t, "udp", udpAddr, ednsQuery("backup.com.", dns.TypeSOA, 4096))
	if resp.Rcode != dns.RcodeServerFailure {
		t.Fatalf("expected SERVFAIL, got %s", dns.RcodeToString[resp.Rcode])
	}
	assertOPTEcho(t, resp)
}

// ---------------------------------------------------------------------------
// BADVERS tests (spec: Unsupported EDNS version receives BADVERS)
// ---------------------------------------------------------------------------

// hasCookieOption reports whether the response OPT record carries any
// COOKIE option.
func hasCookieOption(resp *dns.Msg) bool {
	opt := resp.IsEdns0()
	if opt == nil {
		return false
	}
	for _, o := range opt.Option {
		if _, ok := o.(*dns.EDNS0_COOKIE); ok {
			return true
		}
	}
	return false
}

func TestBADVERS_Version1Query(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	req := ednsQuery("www.root.com.", dns.TypeA, 4096)
	req.IsEdns0().SetVersion(1)
	resp := exchange(t, "udp", udpAddr, req)

	// miekg/dns merges the OPT extended rcode into resp.Rcode on unpack.
	if resp.Rcode != dns.RcodeBadVers {
		t.Fatalf("expected BADVERS, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Question) != 1 || resp.Question[0].Name != "www.root.com." {
		t.Errorf("question section not echoed: %v", resp.Question)
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected empty Answer section, got %d records", len(resp.Answer))
	}
	assertOPTEcho(t, resp)
}

func TestBADVERS_TakesPrecedenceOverCookie(t *testing.T) {
	udpAddr, _, cancel := newOPTTestServer(t)
	defer cancel()

	// Version 1 + malformed 7-byte COOKIE: BADVERS must win over FORMERR
	// and the response must not carry a COOKIE option.
	req := ednsQuery("www.root.com.", dns.TypeA, 4096)
	req.IsEdns0().SetVersion(1)
	addCookie(req, "2464c4abcf10c9") // 7 raw bytes
	resp := exchange(t, "udp", udpAddr, req)

	if resp.Rcode != dns.RcodeBadVers {
		t.Fatalf("expected BADVERS (not FORMERR), got %s", dns.RcodeToString[resp.Rcode])
	}
	if hasCookieOption(resp) {
		t.Error("BADVERS response must not contain a COOKIE option")
	}
}

// ---------------------------------------------------------------------------
// Truncation tests (spec: OPT record persists through UDP truncation and
// counts toward the size budget)
// ---------------------------------------------------------------------------

func TestTruncation_OPTPersistsAndCountsTowardBudget(t *testing.T) {
	// 48 × 10-byte TXT values overflow a 600-byte EDNS budget. The small
	// per-RR size (~23 bytes compressed) keeps the truncated message close
	// to the budget, so an implementation that excludes the 11-byte OPT
	// from the measured size overshoots the budget and fails this test
	// (mutation-checked).
	const owner = "_acme-challenge.example.com."
	const budget = 600
	answer := makeTXTsAtOwner(owner, 48, 10)
	req := buildTXTQuery(owner, budget)

	w := &recordingWriter{}
	replyWithAnswer(w, req, parseQueryOpt(req), answer)

	if got := len(w.Packed); got > budget {
		t.Errorf("packed wire size %d exceeds budget %d (OPT must count toward the budget)", got, budget)
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(w.Packed); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if !resp.Truncated {
		t.Error("expected TC=1 when answers are dropped to fit the budget")
	}
	assertOPTEcho(t, resp)
}
