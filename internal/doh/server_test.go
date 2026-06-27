package doh

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/httpserver"
)

// doGET issues a DoH GET with the base64url-encoded query and returns the
// recorder. clientIP (host:port) overrides the synthetic source.
func doGET(t *testing.T, h http.Handler, b64, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, dohPath+"?dns="+b64, nil)
	if clientIP != "" {
		req.RemoteAddr = clientIP
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// doPOST issues a DoH POST with the wire-format body.
func doPOST(t *testing.T, h http.Handler, body []byte, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, dohPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", dnsMediaType)
	if clientIP != "" {
		req.RemoteAddr = clientIP
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func unpackBody(t *testing.T, rec *httptest.ResponseRecorder) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	if err := m.Unpack(rec.Body.Bytes()); err != nil {
		t.Fatalf("unpack response body: %v", err)
	}
	return m
}

func b64query(name string, qtype uint16) string {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	packed, err := m.Pack()
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(packed)
}

// ---- Task 3.2: RFC 8484 GET/POST, 404, 405 ----

func TestHandler_POSTReturnsWireResponse(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doPOST(t, h, queryMsg("www.example.com."), "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != dnsMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, dnsMediaType)
	}
	resp := unpackBody(t, rec)
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(resp.Answer))
	}
}

func TestHandler_GETReturnsWireResponse(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doGET(t, h, b64query("www.example.com.", dns.TypeA), "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := unpackBody(t, rec)
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(resp.Answer))
	}
}

// TestHandler_RFC8484Example uses the canonical RFC 8484 §4.1.1 GET encoding
// and asserts it decodes to a www.example.com A query answered with 200.
func TestHandler_RFC8484Example(t *testing.T) {
	const example = "AAABAAABAAAAAAAAA3d3dwdleGFtcGxlA2NvbQAAAQAB"
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doGET(t, h, example, "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != dnsMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, dnsMediaType)
	}
	resp := unpackBody(t, rec)
	if len(resp.Question) != 1 {
		t.Fatalf("questions = %d, want 1", len(resp.Question))
	}
	q := resp.Question[0]
	if q.Name != "www.example.com." || q.Qtype != dns.TypeA {
		t.Errorf("decoded question = %s %s, want www.example.com. A", q.Name, dns.TypeToString[q.Qtype])
	}
}

func TestHandler_UnknownPathReturns404(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com."))
	h := newDoHHandler(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/resolve?dns="+b64query("www.example.com.", dns.TypeA), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_UnsupportedMethodReturns405(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com."))
	h := newDoHHandler(t, srv)

	req := httptest.NewRequest(http.MethodPut, dohPath, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// ---- Task 3.3: reuses authoritative path, non-recursive ----

func TestHandler_InZoneMatchesTCP(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))

	// Real TCP listener for the reference answer.
	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	tcpAddrStr := srv.TCPAddr().String()

	c := &dns.Client{Net: "tcp", Timeout: 2 * time.Second}
	q := new(dns.Msg)
	q.SetQuestion("www.example.com.", dns.TypeA)
	tcpResp, _, err := c.Exchange(q, tcpAddrStr)
	if err != nil {
		t.Fatalf("tcp exchange: %v", err)
	}

	h := newDoHHandler(t, srv)
	rec := doPOST(t, h, queryMsg("www.example.com."), "127.0.0.1:40000")
	dohResp := unpackBody(t, rec)

	if dohResp.Rcode != tcpResp.Rcode {
		t.Errorf("rcode DoH=%s TCP=%s", dns.RcodeToString[dohResp.Rcode], dns.RcodeToString[tcpResp.Rcode])
	}
	if len(dohResp.Answer) != len(tcpResp.Answer) || len(dohResp.Answer) != 1 {
		t.Fatalf("answer counts DoH=%d TCP=%d", len(dohResp.Answer), len(tcpResp.Answer))
	}
	if dohResp.Answer[0].String() != tcpResp.Answer[0].String() {
		t.Errorf("answer DoH=%q TCP=%q", dohResp.Answer[0].String(), tcpResp.Answer[0].String())
	}
}

func TestHandler_OutOfZoneRefused(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	rec := doPOST(t, h, queryMsg("www.example.net."), "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := unpackBody(t, rec)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("rcode = %s, want REFUSED", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("answers = %d, want 0 (no recursion)", len(resp.Answer))
	}
}

// TestHandler_XForwardedForIgnored asserts the connection source IP — not the
// X-Forwarded-For header — drives view selection.
func TestHandler_XForwardedForIgnored(t *testing.T) {
	srv := newTwoViewServer(t, "www.example.com.", "203.0.113.0/24", "203.0.113.20", "198.51.100.0/24", "198.51.100.20")
	h := newDoHHandler(t, srv)

	// Connection from view1; header claims a view2 address.
	req := httptest.NewRequest(http.MethodPost, dohPath, bytes.NewReader(queryMsg("www.example.com.")))
	req.RemoteAddr = "203.0.113.5:40000"
	req.Header.Set("X-Forwarded-For", "198.51.100.5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	resp := unpackBody(t, rec)
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(resp.Answer))
	}
	a := resp.Answer[0].(*dns.A)
	if a.A.String() != "203.0.113.20" {
		t.Errorf("answer IP = %s, want 203.0.113.20 (header must be ignored)", a.A.String())
	}
}

// ---- Task 3.4: cache header bounded by min answer TTL ----

func TestHandler_CacheControlBoundedByMinTTL(t *testing.T) {
	// www -> app CNAME (TTL 300), app A (TTL 60): answer TTLs {300, 60}.
	z := buildRootZone("example.com.")
	z.AddRR(&dns.CNAME{
		Hdr:    dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: "app.example.com.",
	})
	z.AddRR(makeA("app.example.com.", "203.0.113.40", 60))
	// zero-ttl name and an empty-answer name.
	z.AddRR(makeA("zero.example.com.", "203.0.113.41", 0))
	srv := newAnyViewServer(t, z)
	h := newDoHHandler(t, srv)

	cases := []struct {
		name      string
		qname     string
		wantMaxLE uint32 // max-age must be <= this
	}{
		{"min of 300 and 60", "www.example.com.", 60},
		{"ttl zero", "zero.example.com.", 0},
		{"empty answer (NXDOMAIN)", "missing.example.com.", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doPOST(t, h, queryMsg(tc.qname), "203.0.113.5:40000")
			cc := rec.Header().Get("Cache-Control")
			const prefix = "max-age="
			if !strings.HasPrefix(cc, prefix) {
				t.Fatalf("Cache-Control = %q, want %s<n>", cc, prefix)
			}
			got, err := strconv.ParseUint(strings.TrimPrefix(cc, prefix), 10, 32)
			if err != nil {
				t.Fatalf("parse max-age: %v", err)
			}
			if uint32(got) > tc.wantMaxLE {
				t.Errorf("max-age = %d, want <= %d", got, tc.wantMaxLE)
			}
		})
	}
}

// ---- Task 3.5: malformed requests -> 400 ----

func TestHandler_MalformedReturns400(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com."))
	h := newDoHHandler(t, srv)

	t.Run("GET invalid base64url", func(t *testing.T) {
		rec := doGET(t, h, "!!!notbase64!!!", "203.0.113.5:40000")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
	t.Run("GET missing dns param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, dohPath, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
	t.Run("POST empty body", func(t *testing.T) {
		rec := doPOST(t, h, nil, "203.0.113.5:40000")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
	t.Run("GET undecodable DNS bytes", func(t *testing.T) {
		rec := doGET(t, h, b64RawURL([]byte{0x00, 0x01, 0x02}), "203.0.113.5:40000")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---- Task 3.6: request size and timeout limits ----

func TestHandler_OversizePOSTReturns413(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com."))
	h := newDoHHandler(t, srv)

	big := make([]byte, maxBodyBytes+1)
	rec := doPOST(t, h, big, "203.0.113.5:40000")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestHandler_OversizeGETReturns413(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com."))
	h := newDoHHandler(t, srv)

	// A base64url-encoded value whose decoded size exceeds maxBodyBytes.
	big := make([]byte, maxBodyBytes+1)
	rec := doGET(t, h, b64RawURL(big), "203.0.113.5:40000")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// TestHandler_ZoneTransferRefused asserts AXFR/IXFR over DoH is refused rather
// than dispatched to the transfer path (which the single-shot writer would
// corrupt).
func TestHandler_ZoneTransferRefused(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	h := newDoHHandler(t, srv)

	for _, qtype := range []uint16{dns.TypeAXFR, dns.TypeIXFR} {
		m := new(dns.Msg)
		m.SetQuestion("example.com.", qtype)
		body, err := m.Pack()
		if err != nil {
			t.Fatalf("pack: %v", err)
		}
		rec := doPOST(t, h, body, "203.0.113.5:40000")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", dns.TypeToString[qtype], rec.Code)
		}
		resp := unpackBody(t, rec)
		if resp.Rcode != dns.RcodeRefused {
			t.Errorf("%s: rcode = %s, want REFUSED", dns.TypeToString[qtype], dns.RcodeToString[resp.Rcode])
		}
		if len(resp.Answer) != 0 {
			t.Errorf("%s: answers = %d, want 0", dns.TypeToString[qtype], len(resp.Answer))
		}
	}
}

func TestHardenedServer_HasNonZeroTimeouts(t *testing.T) {
	s := httpserver.NewServer(":0", nil)
	if s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 || s.ReadHeaderTimeout == 0 {
		t.Errorf("timeouts must all be non-zero: read=%v write=%v idle=%v header=%v",
			s.ReadTimeout, s.WriteTimeout, s.IdleTimeout, s.ReadHeaderTimeout)
	}
}

// b64RawURL base64url-encodes raw bytes (no padding) for GET dns= params.
func b64RawURL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
