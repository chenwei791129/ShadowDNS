package doh

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// All fixtures use RFC 2606 domains and RFC 5737 / RFC 3849 addresses.

// doJSON issues a DoH GET for the JSON path. query is the raw query string
// (without a leading "?"); accept overrides the Accept header (defaults to the
// JSON media type when empty); clientIP (host:port) overrides the synthetic
// source.
func doJSON(t *testing.T, h http.Handler, query, accept, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, dohPath+"?"+query, nil)
	if accept == "" {
		accept = dnsJSONMediaType
	}
	req.Header.Set("Accept", accept)
	if clientIP != "" {
		req.RemoteAddr = clientIP
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func parseJSONBody(t *testing.T, rec *httptest.ResponseRecorder) jsonResponse {
	t.Helper()
	var resp jsonResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal JSON body %q: %v", rec.Body.String(), err)
	}
	return resp
}

func makeAAAA(name, ip string, ttl uint32) *dns.AAAA {
	return &dns.AAAA{
		Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
		AAAA: net.ParseIP(ip),
	}
}

func makeTXT(name string, ttl uint32, txt ...string) *dns.TXT {
	return &dns.TXT{
		Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl},
		Txt: txt,
	}
}

func makeCNAME(name, target string, ttl uint32) *dns.CNAME {
	return &dns.CNAME{
		Hdr:    dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttl},
		Target: target,
	}
}

func makeMX(name, mx string, pref uint16, ttl uint32) *dns.MX {
	return &dns.MX{
		Hdr:        dns.RR_Header{Name: name, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: ttl},
		Preference: pref,
		Mx:         mx,
	}
}

// newECSEnabledAnyViewServer builds a single-view server with ECS processing
// enabled so the ECS injection / scope-echo behavior can be exercised.
func newECSEnabledAnyViewServer(t *testing.T, rootZ *zone.Zone) *server.Server {
	t.Helper()
	srv := newAnyViewServer(t, rootZ)
	srv.ECSEnabled = true
	return srv
}

// ---- Task 3.1: Accept negotiation and ?dns= precedence ----

func TestJSON_AcceptSelectsJSONFormat(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=www.example.com&type=A", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != dnsJSONMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, dnsJSONMediaType)
	}
	resp := parseJSONBody(t, rec)
	if len(resp.Answer) != 1 || resp.Answer[0].Data != "203.0.113.20" {
		t.Fatalf("Answer = %+v, want one A 203.0.113.20", resp.Answer)
	}
}

func TestJSON_DNSParamTakesPrecedence(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	// Both ?dns= and a JSON Accept header: the wire path must win.
	req := httptest.NewRequest(http.MethodGet, dohPath+"?dns="+b64query("www.example.com.", dns.TypeA), nil)
	req.Header.Set("Accept", dnsJSONMediaType)
	req.RemoteAddr = "203.0.113.5:40000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != dnsMediaType {
		t.Errorf("Content-Type = %q, want wire %q", ct, dnsMediaType)
	}
	// Body must be parseable as wire-format DNS, not JSON.
	m := new(dns.Msg)
	if err := m.Unpack(rec.Body.Bytes()); err != nil {
		t.Fatalf("body is not wire-format DNS: %v", err)
	}
}

func TestJSON_AbsentJSONAcceptRetainsWire(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	req := httptest.NewRequest(http.MethodGet, dohPath+"?dns="+b64query("www.example.com.", dns.TypeA), nil)
	req.Header.Set("Accept", "application/dns-message")
	req.RemoteAddr = "203.0.113.5:40000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != dnsMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, dnsMediaType)
	}
}

func TestJSON_POSTAlwaysWire(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	req := httptest.NewRequest(http.MethodPost, dohPath, strings.NewReader(string(queryMsg("www.example.com."))))
	req.Header.Set("Content-Type", dnsMediaType)
	req.Header.Set("Accept", dnsJSONMediaType)
	req.RemoteAddr = "203.0.113.5:40000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != dnsMediaType {
		t.Errorf("Content-Type = %q, want wire %q", ct, dnsMediaType)
	}
}

// ---- Task 1.1: query parsing (type default, case-insensitive, name case) ----

func TestJSON_TypeDefaultsToA(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=www.example.com", "", "203.0.113.5:40000")
	resp := parseJSONBody(t, rec)
	if len(resp.Question) != 1 || resp.Question[0].Type != dns.TypeA {
		t.Fatalf("Question = %+v, want one A question", resp.Question)
	}
	if len(resp.Answer) != 1 || resp.Answer[0].Type != dns.TypeA {
		t.Fatalf("Answer = %+v, want one A answer", resp.Answer)
	}
}

func TestParseQType(t *testing.T) {
	tests := []struct {
		in   string
		want uint16
		ok   bool
	}{
		{"A", dns.TypeA, true},
		{"1", dns.TypeA, true},
		{"TXT", dns.TypeTXT, true},
		{"txt", dns.TypeTXT, true},
		{"Txt", dns.TypeTXT, true},
		{"16", dns.TypeTXT, true},
		{"AAAA", dns.TypeAAAA, true},
		{"", dns.TypeA, true},
		{"65537", 0, false},
		{"notatype", 0, false},
		{"-1", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseQType(tt.in)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Fatalf("parseQType(%q) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestJSON_NameCasePreserved(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=ExAmple.COM&type=SOA", "", "203.0.113.5:40000")
	resp := parseJSONBody(t, rec)
	if len(resp.Question) != 1 || resp.Question[0].Name != "ExAmple.COM." {
		t.Fatalf("Question name = %q, want preserved case ExAmple.COM.", resp.Question[0].Name)
	}
}

// ---- Task 1.2: zone-transfer refusal ----

func TestJSON_AXFRRefused(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=example.com&type=AXFR", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := parseJSONBody(t, rec)
	if resp.Status != dns.RcodeRefused {
		t.Errorf("Status = %d, want %d (REFUSED)", resp.Status, dns.RcodeRefused)
	}
	if len(resp.Answer) != 0 {
		t.Errorf("Answer = %+v, want empty", resp.Answer)
	}
}

// ---- Task 1.3 + 2.1: ECS injection, host-bit masking, scope echo ----

func TestJSON_ECSHostBitsMaskedAndScopeEchoed(t *testing.T) {
	srv := newECSEnabledAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	// Host bits set beyond /24 must be masked, not rejected as FORMERR.
	rec := doJSON(t, h, "name=www.example.com&type=A&edns_client_subnet=198.51.100.5/24", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := parseJSONBody(t, rec)
	if resp.Status != dns.RcodeSuccess {
		t.Fatalf("Status = %d, want 0 (no FORMERR from masked host bits)", resp.Status)
	}
	// Server echoes scope == source for an authoritative answer.
	if resp.EDNSClientSubnet != "198.51.100.0/24/24" {
		t.Errorf("edns_client_subnet = %q, want 198.51.100.0/24/24", resp.EDNSClientSubnet)
	}
}

func TestJSON_ECSDisabledIgnoresParameter(t *testing.T) {
	// newAnyViewServer leaves ECS disabled.
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=www.example.com&type=A&edns_client_subnet=198.51.100.0/24", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := parseJSONBody(t, rec)
	if resp.Status != dns.RcodeSuccess {
		t.Fatalf("Status = %d, want 0", resp.Status)
	}
	if resp.EDNSClientSubnet != "" {
		t.Errorf("edns_client_subnet = %q, want empty when ECS disabled", resp.EDNSClientSubnet)
	}
}

// ---- Task 2.1: schema serialization across record types ----

func TestJSON_RDATASerialization(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeA("a.example.com.", "203.0.113.20", 300),
		makeAAAA("aaaa.example.com.", "2001:db8::1", 300),
		makeTXT("txt.example.com.", 300, "hello-data"),
		makeCNAME("cname.example.com.", "target.example.com.", 300),
		makeMX("mx.example.com.", "mail.example.com.", 10, 300),
	)
	srv := newAnyViewServer(t, rootZ)
	h := newDoHHandler(t, srv)

	tests := []struct {
		qname string
		qtype string
		want  string
	}{
		{"a.example.com", "A", "203.0.113.20"},
		{"aaaa.example.com", "AAAA", "2001:db8::1"},
		{"txt.example.com", "TXT", `"hello-data"`},
		{"cname.example.com", "CNAME", "target.example.com."},
		{"mx.example.com", "MX", "10 mail.example.com."},
		{"example.com", "SOA", "ns1.example.com. hostmaster.example.com. 1 3600 600 86400 300"},
	}
	for _, tt := range tests {
		t.Run(tt.qtype, func(t *testing.T) {
			rec := doJSON(t, h, "name="+tt.qname+"&type="+tt.qtype, "", "203.0.113.5:40000")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			resp := parseJSONBody(t, rec)
			if !resp.RD {
				t.Error("RD = false, want true")
			}
			if resp.CD {
				t.Error("CD = true, want false")
			}
			if len(resp.Answer) != 1 {
				t.Fatalf("Answer = %+v, want exactly one", resp.Answer)
			}
			if resp.Answer[0].Data != tt.want {
				t.Errorf("data = %q, want %q", resp.Answer[0].Data, tt.want)
			}
		})
	}
}

// TestJSON_TXTExample asserts the spec's single-TXT example field-for-field
// (field order not significant) plus the TTL-bounded Cache-Control header.
func TestJSON_TXTExample(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeTXT("_ephemeral-doh-check.example.com.", 120, "hello"),
	)
	srv := newAnyViewServer(t, rootZ)
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=_ephemeral-doh-check.example.com&type=TXT", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "max-age=120" {
		t.Errorf("Cache-Control = %q, want max-age=120", cc)
	}
	resp := parseJSONBody(t, rec)
	want := jsonResponse{
		Status:   0,
		TC:       false,
		RD:       true,
		RA:       false,
		AD:       false,
		CD:       false,
		Question: []jsonQuestion{{Name: "_ephemeral-doh-check.example.com.", Type: 16}},
		Answer:   []jsonAnswer{{Name: "_ephemeral-doh-check.example.com.", Type: 16, TTL: 120, Data: `"hello"`}},
	}
	if resp.Status != want.Status || resp.TC != want.TC || resp.RD != want.RD ||
		resp.RA != want.RA || resp.AD != want.AD || resp.CD != want.CD {
		t.Errorf("header fields = %+v, want %+v", resp, want)
	}
	if len(resp.Question) != 1 || resp.Question[0] != want.Question[0] {
		t.Errorf("Question = %+v, want %+v", resp.Question, want.Question)
	}
	if len(resp.Answer) != 1 || resp.Answer[0] != want.Answer[0] {
		t.Errorf("Answer = %+v, want %+v", resp.Answer, want.Answer)
	}
	if resp.EDNSClientSubnet != "" {
		t.Errorf("edns_client_subnet = %q, want empty", resp.EDNSClientSubnet)
	}
}

func TestJSON_OutOfZoneRefused(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=www.example.net&type=A", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := parseJSONBody(t, rec)
	if resp.Status != dns.RcodeRefused {
		t.Errorf("Status = %d, want %d (REFUSED)", resp.Status, dns.RcodeRefused)
	}
	if len(resp.Answer) != 0 {
		t.Errorf("Answer = %+v, want empty", resp.Answer)
	}
}

// ---- Task 3.2: HTTP error semantics ----

func TestJSON_MissingNameReturns400(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	for _, q := range []string{"type=A", "name=&type=A"} {
		rec := doJSON(t, h, q, "", "203.0.113.5:40000")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestJSON_OverlongNameReturns400(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	// A label over 63 octets is malformed input and must be a 400, not a 500
	// from a downstream pack failure.
	overlong := strings.Repeat("b", 70) + ".example.com"
	rec := doJSON(t, h, "name="+overlong+"&type=A", "", "203.0.113.5:40000")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestJSON_UnparseableTypeReturns400(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	for _, q := range []string{"name=www.example.com&type=65537", "name=www.example.com&type=notatype"} {
		rec := doJSON(t, h, q, "", "203.0.113.5:40000")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestJSON_UnparseableECSReturns400(t *testing.T) {
	srv := newECSEnabledAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	for _, ecs := range []string{
		"notanip",
		// v4-mapped IPv6 prefix wider than the IPv4 family: must be rejected
		// rather than injected as a malformed option the server discards.
		"::ffff:198.51.100.0/120",
	} {
		rec := doJSON(t, h, "name=www.example.com&type=A&edns_client_subnet="+ecs, "", "203.0.113.5:40000")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ecs %q: status = %d, want 400", ecs, rec.Code)
		}
	}
}

// stubNoWriteHandler is a dns.Handler that never writes a response, modeling
// an internal failure so the empty-capture 500 guard can be exercised.
type stubNoWriteHandler struct{}

func (stubNoWriteHandler) ServeDNS(dns.ResponseWriter, *dns.Msg) {}

func TestJSON_EmptyCaptureReturns500(t *testing.T) {
	s := &Server{dns: stubNoWriteHandler{}, cfg: testDoHConfig(), logger: zap.NewNop()}
	h := s.Handler()

	rec := doJSON(t, h, "name=www.example.com&type=A", "", "203.0.113.5:40000")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// ---- Task 3.3: cd tolerated and ignored ----

func TestJSON_CDToleratedAndIgnored(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doJSON(t, h, "name=www.example.com&type=A&cd=1&do=1&ct=application/dns-json", "", "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (cd/do/ct tolerated)", rec.Code)
	}
	resp := parseJSONBody(t, rec)
	if resp.CD {
		t.Error("CD = true, want false (cd parameter must not set the CD bit)")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer = %+v, want one (resolved normally)", resp.Answer)
	}
}

func TestAcceptsDNSJSON(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"application/dns-json", true},
		{"application/dns-message, application/dns-json", true},
		{"application/dns-json; q=0.9", true},
		{"APPLICATION/DNS-JSON", true},
		{"application/dns-message", false},
		{"*/*", false},
		{"application/*", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			if got := acceptsDNSJSON(tt.accept); got != tt.want {
				t.Errorf("acceptsDNSJSON(%q) = %v, want %v", tt.accept, got, tt.want)
			}
		})
	}
}
