package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix %q: %v", s, err)
	}
	return p
}

func singleAddrPrefix(t *testing.T, ip string) netip.Prefix {
	t.Helper()
	a, err := netip.ParseAddr(ip)
	if err != nil {
		t.Fatalf("ParseAddr %q: %v", ip, err)
	}
	bits := 32
	if a.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(a, bits)
}

func newTestServer(t *testing.T, token string, allow []netip.Prefix) (*Server, *ephemeral.Store) {
	t.Helper()
	store := ephemeral.NewStore()
	cfg := &shadowdnscfg.EphemeralAPIConfig{
		Listen: "127.0.0.1:0",
		Allow:  allow,
		Token:  token,
	}
	s := NewServer(cfg, store, zap.NewNop())
	return s, store
}

// doRequestFrom sets RemoteAddr on the request to simulate a specific source IP.
func doRequest(s *Server, method, target, body, remoteIP, authHeader string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.RemoteAddr = net.JoinHostPort(remoteIP, "54321")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	return w
}

// ---------- IP ACL ----------

func TestServer_IPACL_AllowedIPPasses(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "10.0.0.5")})
	body := `{"value":"v","ttl":60}`
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", body, "10.0.0.5", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.Lookup("foo.example.com."); !ok {
		t.Error("expected record to be stored")
	}
}

func TestServer_IPACL_DisallowedIPRejected(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "10.0.0.5")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":60}`, "192.168.99.1", "")
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestServer_IPACL_CIDRMatch(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{mustPrefix(t, "192.168.1.0/24")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":60}`, "192.168.1.50", "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (within CIDR)", w.Code)
	}
}

// ---------- Token auth ----------

func TestServer_Token_ValidAccepted(t *testing.T) {
	s, _ := newTestServer(t, "secret123", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":60}`, "127.0.0.1", "Bearer secret123")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestServer_Token_InvalidRejected(t *testing.T) {
	s, _ := newTestServer(t, "secret123", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":60}`, "127.0.0.1", "Bearer wrong")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestServer_Token_MissingHeaderRejected(t *testing.T) {
	s, _ := newTestServer(t, "secret123", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":60}`, "127.0.0.1", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestServer_Token_NotConfiguredSkipsValidation(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":60}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no token configured)", w.Code)
	}
}

// ---------- PUT handler ----------

type putResponse struct {
	Status string `json:"status"`
	FQDN   string `json:"fqdn"`
	TTL    int    `json:"ttl"`
	Count  int    `json:"count"`
}

func decodePutResponse(t *testing.T, w *httptest.ResponseRecorder) putResponse {
	t.Helper()
	var resp putResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	return resp
}

func TestServer_PUT_CreatesRecord(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	body := `{"value":"token123","ttl":120}`
	w := doRequest(s, "PUT", "/v1/txt/_acme-challenge.example.com", body, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	resp := decodePutResponse(t, w)
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if resp.FQDN != "_acme-challenge.example.com." {
		t.Errorf("FQDN = %q, want trailing-dot canonical form", resp.FQDN)
	}
	if resp.TTL != 120 {
		t.Errorf("TTL = %d, want 120", resp.TTL)
	}
	recs, ok := store.Lookup("_acme-challenge.example.com.")
	if !ok {
		t.Fatal("expected record to be stored")
	}
	if len(recs) != 1 || recs[0].Value != "token123" {
		t.Errorf("records = %+v, want single token123", recs)
	}
	if resp.Count != 1 {
		t.Errorf("response count = %d, want 1", resp.Count)
	}
}

func TestServer_PUT_CanonicalizesFQDN(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/FOO.EXAMPLE.COM", `{"value":"v","ttl":60}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if _, ok := store.Lookup("foo.example.com."); !ok {
		t.Error("expected stored under canonical FQDN")
	}
	resp := decodePutResponse(t, w)
	if resp.FQDN != "foo.example.com." {
		t.Errorf("response FQDN = %q, want foo.example.com.", resp.FQDN)
	}
}

func TestServer_PUT_AppendsSecondValue(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"first","ttl":60}`, "127.0.0.1", "")
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"second","ttl":300}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	resp := decodePutResponse(t, w)
	if resp.Count != 2 {
		t.Errorf("response count = %d, want 2", resp.Count)
	}
	recs, _ := store.Lookup("foo.example.com.")
	if len(recs) != 2 {
		t.Fatalf("got %d stored records, want 2", len(recs))
	}
}

func TestServer_PUT_SameValueRefreshesNoDuplicate(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"same","ttl":60}`, "127.0.0.1", "")
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"same","ttl":300}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	resp := decodePutResponse(t, w)
	if resp.Count != 1 {
		t.Errorf("response count = %d, want 1 (same value must refresh, not append)", resp.Count)
	}
	recs, _ := store.Lookup("foo.example.com.")
	if len(recs) != 1 {
		t.Fatalf("got %d stored records, want 1", len(recs))
	}
}

func TestServer_PUT_TTLClampedToMin(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":0}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	resp := decodePutResponse(t, w)
	if resp.TTL != 1 {
		t.Errorf("TTL = %d, want 1 (clamped)", resp.TTL)
	}
}

func TestServer_PUT_TTLClampedToMax(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":7200}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	resp := decodePutResponse(t, w)
	if resp.TTL != 3600 {
		t.Errorf("TTL = %d, want 3600 (clamped)", resp.TTL)
	}
}

func TestServer_PUT_TTLNegativeClampedToMin(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"value":"v","ttl":-5}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	resp := decodePutResponse(t, w)
	if resp.TTL != 1 {
		t.Errorf("TTL = %d, want 1 (negative clamped to min)", resp.TTL)
	}
}

func TestServer_PUT_EmptyBodyRejected(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", "", "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestServer_PUT_InvalidJSONRejected(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{not json}`, "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestServer_PUT_MissingValueRejected(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", `{"ttl":60}`, "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ---------- DELETE handler ----------

func TestServer_DELETE_RemovesRecord(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "v", 60)
	w := doRequest(s, "DELETE", "/v1/txt/foo.example.com", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if _, ok := store.Lookup("foo.example.com."); ok {
		t.Error("expected record to be deleted")
	}
}

func TestServer_DELETE_CanonicalizesFQDN(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "v", 60)
	w := doRequest(s, "DELETE", "/v1/txt/FOO.EXAMPLE.COM", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if _, ok := store.Lookup("foo.example.com."); ok {
		t.Error("expected record deleted via canonical form")
	}
}

func TestServer_DELETE_NonExistentReturns200(t *testing.T) {
	s, _ := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	w := doRequest(s, "DELETE", "/v1/txt/missing.example.com", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (idempotent)", w.Code)
	}
}

// TestServer_DELETE_RemovesAllEntries verifies the whole-FQDN wipe semantics:
// DELETE clears every ephemeral value under the FQDN in a single call.
func TestServer_DELETE_RemovesAllEntries(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "a", 120)
	store.Put("foo.example.com.", "b", 120)
	store.Put("foo.example.com.", "c", 120)

	w := doRequest(s, "DELETE", "/v1/txt/foo.example.com", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if _, ok := store.Lookup("foo.example.com."); ok {
		t.Error("expected every entry to be deleted")
	}
}

// ---------- NewServer returns nil when config absent ----------

func TestNewServer_NilConfigReturnsNil(t *testing.T) {
	s := NewServer(nil, ephemeral.NewStore(), zap.NewNop())
	if s != nil {
		t.Errorf("expected nil server when cfg is nil, got %+v", s)
	}
}

// ---------- Graceful shutdown ----------

func TestServer_GracefulShutdown(t *testing.T) {
	store := ephemeral.NewStore()
	cfg := &shadowdnscfg.EphemeralAPIConfig{
		Listen: "127.0.0.1:0",
		Allow:  []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")},
	}
	s := NewServer(cfg, store, zap.NewNop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, ln)
	}()

	addr := ln.Addr().String()
	// Sanity-probe the server is up.
	resp, err := http.Post(fmt.Sprintf("http://%s/v1/txt/foo.example.com", addr), "application/json", bytes.NewReader([]byte(`{"value":"v","ttl":60}`)))
	if err == nil {
		resp.Body.Close()
	}

	cancel()

	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Serve returned unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return within 10s after context cancel")
	}
}
