package server

import (
	"net"
	"net/netip"
	"os"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/miekg/dns"
	"go4.org/netipx"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// addECS appends an EDNS0_SUBNET option to the request's OPT record (which
// must already exist).
func addECS(req *dns.Msg, family uint16, prefix, scope uint8, addr net.IP) {
	opt := req.IsEdns0()
	opt.Option = append(opt.Option, &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		Family:        family,
		SourceNetmask: prefix,
		SourceScope:   scope,
		Address:       addr,
	})
}

func TestParseQueryOpt_FirstECSWins(t *testing.T) {
	// Mirroring the COOKIE first-wins rule: only the first ECS option in the
	// OPT record is processed; the rest are silently ignored.
	req := ednsQuery("www.root.com.", dns.TypeA, 1232)
	addECS(req, 1, 24, 0, net.ParseIP("203.0.113.0"))
	addECS(req, 1, 32, 0, net.ParseIP("198.51.100.7"))

	got := parseQueryOpt(req)
	if got.ecs == nil {
		t.Fatal("ecs = nil, want first ECS option")
	}
	if got.ecs.SourceNetmask != 24 || !got.ecs.Address.Equal(net.ParseIP("203.0.113.0")) {
		t.Errorf("ecs = %+v, want first option (prefix 24, 203.0.113.0)", got.ecs)
	}
}

func TestParseQueryOpt_NoECS(t *testing.T) {
	req := ednsQuery("www.root.com.", dns.TypeA, 1232)

	got := parseQueryOpt(req)
	if got.ecs != nil {
		t.Errorf("ecs = %+v without ECS option, want nil", got.ecs)
	}
}

// ecsQuery builds an EDNS query for www.root.com./A carrying one ECS option.
func ecsQuery(family uint16, prefix, scope uint8, addr net.IP) *dns.Msg {
	req := ednsQuery("www.root.com.", dns.TypeA, 1232)
	addECS(req, family, prefix, scope, addr)
	return req
}

// serveECS runs req through ServeDNS on a one-zone test server with the given
// ECS flag state and returns the unpacked response.
func serveECS(t *testing.T, ecsEnabled bool, req *dns.Msg) *dns.Msg {
	t.Helper()
	return serveECSState(t, ecsEnabled, optTestState(), req)
}

// respECS extracts the ECS option from the response OPT record; nil when the
// response has no OPT or no ECS option.
func respECS(resp *dns.Msg) *dns.EDNS0_SUBNET {
	opt := resp.IsEdns0()
	if opt == nil {
		return nil
	}
	for _, o := range opt.Option {
		if e, ok := o.(*dns.EDNS0_SUBNET); ok {
			return e
		}
	}
	return nil
}

func TestParseQueryOpt_ECSAndCookieBothExtracted(t *testing.T) {
	// ECS extraction shares the single option iteration with COOKIE; the
	// presence of one must not stop extraction of the other.
	req := ednsQuery("www.root.com.", dns.TypeA, 1232)
	addCookie(req, "2464c4abcf10c957")
	addECS(req, 1, 24, 0, net.ParseIP("203.0.113.0"))

	got := parseQueryOpt(req)
	if got.cookie == nil {
		t.Error("cookie = nil, want COOKIE option extracted alongside ECS")
	}
	if got.ecs == nil {
		t.Error("ecs = nil, want ECS option extracted alongside COOKIE")
	}
}

// ---------------------------------------------------------------------------
// End-to-end matrix (spec: edns-client-subnet). Queries are constructed
// directly and fed to ServeDNS — EDNS0_SUBNET.pack rejects wire-level
// violations (unknown family, out-of-range prefix), so library-rejected rows
// of the validation matrix are out of scope here per the project testing
// principle.
// ---------------------------------------------------------------------------

// buildServerCountryMMDB creates a temporary GeoLite2-Country-compatible mmdb
// (same pattern as the view package's test helper):
//
//	127.0.0.2/32   → "TW" (the recordingWriter source IP)
//	203.0.113.0/32 → "TW"
//	198.51.100.1/32 → "JP"
func buildServerCountryMMDB(t testing.TB) string {
	t.Helper()

	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}
	insertCountry := func(cidr, code string) {
		record := mmdbtype.Map{
			"country": mmdbtype.Map{"iso_code": mmdbtype.String(code)},
		}
		if err := writer.Insert(netipx.PrefixIPNet(netip.MustParsePrefix(cidr)), record); err != nil {
			t.Fatalf("writer.Insert(%s): %v", cidr, err)
		}
	}
	insertCountry("127.0.0.2/32", "TW")
	insertCountry("203.0.113.0/32", "TW")
	insertCountry("198.51.100.1/32", "JP")

	path := t.TempDir() + "/GeoLite2-Country.mmdb"
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create mmdb file: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := writer.WriteTo(f); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return path
}

// ecsViewState builds a test state with the given matcher and one root.com.
// zone per view, each answering www.root.com./A with a distinct address so
// tests can assert which view served the response.
func ecsViewState(matcher *view.Matcher, viewAnswers map[string]string) ServerState {
	st := ServerState{
		Matcher:     matcher,
		ZoneOrigins: map[string][]string{},
		RootZones:   map[string]map[string]*zone.Zone{},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}
	for viewName, answer := range viewAnswers {
		rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", answer, 300))
		st.ZoneOrigins[viewName] = []string{"root.com."}
		st.RootZones[viewName] = map[string]*zone.Zone{"root.com.": rootZ}
	}
	return st
}

// openTestCountryDB opens the test mmdb and registers cleanup.
func openTestCountryDB(t *testing.T) *view.CountryDB {
	t.Helper()
	cdb, err := view.OpenCountryDB(buildServerCountryMMDB(t))
	if err != nil {
		t.Fatalf("OpenCountryDB: %v", err)
	}
	t.Cleanup(func() { _ = cdb.Close() })
	return cdb
}

// answeredA extracts the single A-record answer address, failing the test
// when the response is not a one-A-record NOERROR answer.
func answeredA(t *testing.T, resp *dns.Msg) string {
	t.Helper()
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("rcode = %s, want NOERROR", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("answer count = %d, want 1", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer = %T, want *dns.A", resp.Answer[0])
	}
	return a.A.String()
}

// serveECSState is serveECS with a caller-supplied state.
func serveECSState(t *testing.T, ecsEnabled bool, st ServerState, req *dns.Msg) *dns.Msg {
	t.Helper()
	srv := NewServer(st, nil)
	srv.ECSEnabled = ecsEnabled
	w := &recordingWriter{}
	srv.ServeDNS(w, req)
	resp := new(dns.Msg)
	if err := resp.Unpack(w.Packed); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	return resp
}

// TestServeDNS_ECSMatrix covers enabled/disabled × every handler-reachable
// query shape from the spec validation matrix: response rcode, presence of
// the ECS echo, and its family/prefix/scope values.
func TestServeDNS_ECSMatrix(t *testing.T) {
	type echoWant struct {
		family uint16
		prefix uint8
		scope  uint8
		addr   string // non-empty: assert the echoed ADDRESS equals this IP
	}
	cases := []struct {
		name  string
		build func() *dns.Msg
		// enabled expectations
		wantRcode int
		wantEcho  *echoWant // nil = no ECS option expected
	}{
		{
			name: "no ECS",
			build: func() *dns.Msg {
				req := new(dns.Msg)
				req.SetQuestion("www.root.com.", dns.TypeA)
				req.SetEdns0(1232, false)
				return req
			},
			wantRcode: dns.RcodeSuccess,
			wantEcho:  nil,
		},
		{
			name:      "valid IPv4",
			build:     func() *dns.Msg { return ecsQuery(1, 24, 0, net.ParseIP("203.0.113.0")) },
			wantRcode: dns.RcodeSuccess,
			wantEcho:  &echoWant{family: 1, prefix: 24, scope: 24, addr: "203.0.113.0"},
		},
		{
			name:      "valid IPv6",
			build:     func() *dns.Msg { return ecsQuery(2, 56, 0, net.ParseIP("2001:db8:ab::")) },
			wantRcode: dns.RcodeSuccess,
			wantEcho:  &echoWant{family: 2, prefix: 56, scope: 56, addr: "2001:db8:ab::"},
		},
		{
			name:      "opt-out FAMILY 1",
			build:     func() *dns.Msg { return ecsQuery(1, 0, 0, nil) },
			wantRcode: dns.RcodeSuccess,
			wantEcho:  &echoWant{family: 1, prefix: 0, scope: 0},
		},
		{
			name:      "opt-out FAMILY 0",
			build:     func() *dns.Msg { return ecsQuery(0, 0, 0, nil) },
			wantRcode: dns.RcodeSuccess,
			wantEcho:  &echoWant{family: 0, prefix: 0, scope: 0},
		},
		{
			name:      "non-zero query scope",
			build:     func() *dns.Msg { return ecsQuery(1, 24, 24, net.ParseIP("203.0.113.0")) },
			wantRcode: dns.RcodeFormatError,
			wantEcho:  nil,
		},
		{
			name:      "non-zero bits beyond prefix",
			build:     func() *dns.Msg { return ecsQuery(1, 24, 0, net.ParseIP("203.0.113.9")) },
			wantRcode: dns.RcodeFormatError,
			wantEcho:  nil,
		},
		{
			name:      "prefix 0 with non-zero address (malformed beats opt-out)",
			build:     func() *dns.Msg { return ecsQuery(1, 0, 0, net.ParseIP("203.0.113.9")) },
			wantRcode: dns.RcodeFormatError,
			wantEcho:  nil,
		},
	}

	for _, tc := range cases {
		t.Run("enabled/"+tc.name, func(t *testing.T) {
			resp := serveECS(t, true, tc.build())
			if resp.Rcode != tc.wantRcode {
				t.Fatalf("rcode = %s, want %s",
					dns.RcodeToString[resp.Rcode], dns.RcodeToString[tc.wantRcode])
			}
			if tc.wantRcode == dns.RcodeFormatError {
				assertOPTEcho(t, resp)
			}
			e := respECS(resp)
			if tc.wantEcho == nil {
				if e != nil {
					t.Fatalf("response carries ECS option %+v, want none", e)
				}
				return
			}
			if e == nil {
				t.Fatal("response has no ECS option, want echo")
			}
			if e.Family != tc.wantEcho.family || e.SourceNetmask != tc.wantEcho.prefix || e.SourceScope != tc.wantEcho.scope {
				t.Errorf("echo = family %d prefix %d scope %d, want %d/%d/%d",
					e.Family, e.SourceNetmask, e.SourceScope,
					tc.wantEcho.family, tc.wantEcho.prefix, tc.wantEcho.scope)
			}
			if tc.wantEcho.addr != "" && !e.Address.Equal(net.ParseIP(tc.wantEcho.addr)) {
				t.Errorf("echo address = %v, want %s", e.Address, tc.wantEcho.addr)
			}
		})

		t.Run("disabled/"+tc.name, func(t *testing.T) {
			// With ECS disabled every shape — including handler-reachable
			// malformed ones — is answered normally and never echoed.
			resp := serveECS(t, false, tc.build())
			if resp.Rcode != dns.RcodeSuccess {
				t.Fatalf("rcode = %s, want NOERROR (ECS ignored when disabled)",
					dns.RcodeToString[resp.Rcode])
			}
			if e := respECS(resp); e != nil {
				t.Errorf("response carries ECS option %+v with ECS disabled, want none", e)
			}
		})
	}
}

// TestServeDNS_ECSGeoOverride covers the spec scenario "ECS address overrides
// source IP for geo rules": view-asia (country TW) is declared before
// view-global (any); the source IP 127.0.0.2 maps to TW in the test mmdb, but
// the deciding input must be the ECS address.
func TestServeDNS_ECSGeoOverride(t *testing.T) {
	matcher := &view.Matcher{
		Views: []view.NamedRuleSet{
			{Name: "view-asia", Rules: []config.MatchRule{config.CountryRule{Code: "TW"}}},
			{Name: "view-global", Rules: []config.MatchRule{config.AnyRule{}}},
		},
		Country: openTestCountryDB(t),
	}
	st := ecsViewState(matcher, map[string]string{
		"view-asia":   "192.0.2.10",
		"view-global": "192.0.2.20",
	})

	t.Run("ECS address selects the geo view", func(t *testing.T) {
		// ECS 203.0.113.0 → TW → view-asia.
		resp := serveECSState(t, true, st, ecsQuery(1, 24, 0, net.ParseIP("203.0.113.0")))
		if got := answeredA(t, resp); got != "192.0.2.10" {
			t.Errorf("answer = %s, want view-asia's 192.0.2.10", got)
		}
	})

	t.Run("geo-absent ECS address is no-match without source fallback", func(t *testing.T) {
		// Spec scenario "ECS address absent from geo databases is a geo
		// no-match": ECS 203.0.113.99 has no mmdb entry. The source IP
		// 127.0.0.2 maps to TW, so a buggy source-IP fallback would answer
		// from view-asia; the correct behavior falls through to view-global.
		resp := serveECSState(t, true, st, ecsQuery(1, 32, 0, net.ParseIP("203.0.113.99")))
		if got := answeredA(t, resp); got != "192.0.2.20" {
			t.Errorf("answer = %s, want view-global's 192.0.2.20 (no source-IP geo fallback)", got)
		}
	})

	t.Run("opt-out keeps source IP for geo rules", func(t *testing.T) {
		// Opt-out: view selection stays on the source IP 127.0.0.2 → TW →
		// view-asia, echo scope 0.
		resp := serveECSState(t, true, st, ecsQuery(1, 0, 0, nil))
		if got := answeredA(t, resp); got != "192.0.2.10" {
			t.Errorf("answer = %s, want view-asia's 192.0.2.10 (source IP drives opt-out)", got)
		}
		if e := respECS(resp); e == nil || e.SourceScope != 0 {
			t.Errorf("opt-out echo = %+v, want scope 0", e)
		}
	})

	t.Run("disabled ignores ECS for geo selection", func(t *testing.T) {
		// Source IP TW → view-asia even though ECS 198.51.100.1 maps to JP.
		resp := serveECSState(t, false, st, ecsQuery(1, 32, 0, net.ParseIP("198.51.100.1")))
		if got := answeredA(t, resp); got != "192.0.2.10" {
			t.Errorf("answer = %s, want view-asia's 192.0.2.10 (ECS ignored when disabled)", got)
		}
	})
}

// TestServeDNS_ForgedECSCannotSelectACLView is the load-bearing guard against
// swapped Resolve arguments (spec scenario "Forged ECS cannot select an
// ACL-protected view"). DO NOT DELETE: if a future refactor passes the ECS
// address into ACL evaluation, this is the test that catches it.
func TestServeDNS_ForgedECSCannotSelectACLView(t *testing.T) {
	matcher := &view.Matcher{
		Views: []view.NamedRuleSet{
			{Name: "view-internal", Rules: []config.MatchRule{
				config.CIDRRule{Prefix: netip.MustParsePrefix("192.0.2.0/24")},
			}},
			{Name: "view-global", Rules: []config.MatchRule{config.AnyRule{}}},
		},
	}
	st := ecsViewState(matcher, map[string]string{
		"view-internal": "192.0.2.10",
		"view-global":   "192.0.2.20",
	})

	// Source IP is 127.0.0.2 (outside the CIDR); the forged ECS address
	// 192.0.2.5 lies inside it and must not be consulted by the CIDR rule.
	resp := serveECSState(t, true, st, ecsQuery(1, 32, 0, net.ParseIP("192.0.2.5")))
	if got := answeredA(t, resp); got != "192.0.2.20" {
		t.Errorf("answer = %s, want view-global's 192.0.2.20 (forged ECS must not hit the ACL view)", got)
	}
	if e := respECS(resp); e == nil || e.SourceScope != 32 {
		t.Errorf("echo = %+v, want scope 32 (valid ECS is still echoed)", e)
	}
}

// TestServeDNS_ECSRefusedEcho asserts the no-view REFUSED response still
// echoes the ECS option (spec: echo applies to every response assembled at or
// after the ECS processing point, REFUSED included).
func TestServeDNS_ECSRefusedEcho(t *testing.T) {
	// One CIDR-only view that the 127.0.0.2 source never matches → REFUSED.
	st := ecsViewState(makeMatcher("192.0.2.0/24", "view-internal"), map[string]string{
		"view-internal": "192.0.2.10",
	})

	resp := serveECSState(t, true, st, ecsQuery(1, 24, 0, net.ParseIP("203.0.113.0")))
	if resp.Rcode != dns.RcodeRefused {
		t.Fatalf("rcode = %s, want REFUSED", dns.RcodeToString[resp.Rcode])
	}
	e := respECS(resp)
	if e == nil {
		t.Fatal("REFUSED response has no ECS option, want echo")
	}
	if e.SourceScope != 24 {
		t.Errorf("echo scope = %d, want 24", e.SourceScope)
	}
}

// TestServeDNS_ECSMalformedAppliesToAXFR asserts the spec requirement that
// ECS validation runs before the zone-transfer dispatch: a transfer query
// carrying a handler-detectable malformed ECS option receives FORMERR.
func TestServeDNS_ECSMalformedAppliesToAXFR(t *testing.T) {
	req := ednsQuery("root.com.", dns.TypeAXFR, 1232)
	addECS(req, 1, 24, 24, net.ParseIP("203.0.113.0")) // non-zero query scope

	resp := serveECS(t, true, req)
	if resp.Rcode != dns.RcodeFormatError {
		t.Fatalf("rcode = %s, want FORMERR (ECS validation precedes transfer dispatch)",
			dns.RcodeToString[resp.Rcode])
	}
	if respECS(resp) != nil {
		t.Error("FORMERR response carries an ECS option, want none")
	}
}
