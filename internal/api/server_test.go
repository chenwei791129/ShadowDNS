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

// newTestServer builds a Server for tests. The variadic zones argument
// supplies the canonical zone origins the lister will return; when omitted,
// the default {"example.com.", "backup.com."} covers the FQDNs the legacy
// suite writes through PUT.
func newTestServer(t *testing.T, token string, allow []netip.Prefix, zones ...string) (*Server, *ephemeral.Store) {
	t.Helper()
	if len(zones) == 0 {
		zones = []string{"example.com.", "backup.com."}
	}
	store := ephemeral.NewStore()
	cfg := &shadowdnscfg.EphemeralAPIConfig{
		Listen: "127.0.0.1:0",
		Allow:  allow,
		Token:  token,
	}
	lister := func() []string { return zones }
	s := NewServer(cfg, store, lister, zap.NewNop())
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

// TestServer_DELETE_WithValueSelectorRemovesOnlyMatching covers the
// per-value delete: ?value=token-A must remove only that entry and keep
// any other values under the same FQDN.
func TestServer_DELETE_WithValueSelectorRemovesOnlyMatching(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("_acme-challenge.example.com.", "token-A", 120)
	store.Put("_acme-challenge.example.com.", "token-B", 120)

	w := doRequest(s, "DELETE", "/v1/txt/_acme-challenge.example.com?value=token-A", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	recs, ok := store.Lookup("_acme-challenge.example.com.")
	if !ok || len(recs) != 1 || recs[0].Value != "token-B" {
		t.Errorf("after ?value=token-A delete: recs=%+v ok=%v, want single token-B", recs, ok)
	}
}

// TestServer_DELETE_WithValueSelectorNoMatchReturns200 verifies idempotent
// semantics: deleting a non-matching value is a no-op but still returns 200.
func TestServer_DELETE_WithValueSelectorNoMatchReturns200(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "token-A", 120)

	w := doRequest(s, "DELETE", "/v1/txt/foo.example.com?value=token-X", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (idempotent)", w.Code)
	}
	recs, ok := store.Lookup("foo.example.com.")
	if !ok || len(recs) != 1 || recs[0].Value != "token-A" {
		t.Errorf("store changed after non-matching delete: recs=%+v ok=%v", recs, ok)
	}
}

// TestServer_DELETE_EmptyValueSelectorReturns400 verifies that a present-but-empty
// ?value= query must not be conflated with the wipe-all semantics (?value= absent).
func TestServer_DELETE_EmptyValueSelectorReturns400(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "a", 120)
	store.Put("foo.example.com.", "b", 120)

	w := doRequest(s, "DELETE", "/v1/txt/foo.example.com?value=", "", "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty ?value=", w.Code)
	}
	recs, _ := store.Lookup("foo.example.com.")
	if len(recs) != 2 {
		t.Errorf("store mutated on 400 response: got %d entries, want 2", len(recs))
	}
}

// TestServer_DELETE_OversizeValueSelectorReturns400 verifies RFC 1035 limit
// is enforced before touching the store.
func TestServer_DELETE_OversizeValueSelectorReturns400(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "a", 120)

	oversize := strings.Repeat("x", 256)
	w := doRequest(s, "DELETE", "/v1/txt/foo.example.com?value="+oversize, "", "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversize ?value=", w.Code)
	}
	recs, _ := store.Lookup("foo.example.com.")
	if len(recs) != 1 {
		t.Errorf("store mutated on oversize 400: got %d entries, want 1", len(recs))
	}
}

// TestServer_DELETE_NoValueSelectorWipesAll is a regression check that the
// legacy wipe-all semantics still hold when ?value= is absent.
func TestServer_DELETE_NoValueSelectorWipesAll(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})
	store.Put("foo.example.com.", "a", 120)
	store.Put("foo.example.com.", "b", 120)
	store.Put("foo.example.com.", "c", 120)

	w := doRequest(s, "DELETE", "/v1/txt/foo.example.com", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if _, ok := store.Lookup("foo.example.com."); ok {
		t.Error("wipe-all DELETE (no ?value=) must remove every entry")
	}
}

// TestServer_PUT_OversizeValueRejected verifies RFC 1035 255-byte cap on PUT.
func TestServer_PUT_OversizeValueRejected(t *testing.T) {
	s, store := newTestServer(t, "", []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")})

	oversize := strings.Repeat("x", 256)
	body := fmt.Sprintf(`{"value":%q,"ttl":120}`, oversize)
	w := doRequest(s, "PUT", "/v1/txt/foo.example.com", body, "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversize value", w.Code)
	}
	if _, ok := store.Lookup("foo.example.com."); ok {
		t.Error("store must not be modified when PUT is rejected")
	}
}

// ---------- Zone-membership guard (reject unknown-zone PUTs) ----------

// newTestServerWithZones returns a Server whose ZoneLister reports exactly
// the supplied origins (including the empty set for "no zones loaded" cases).
// Distinct from newTestServer whose variadic default injects convenient
// example/backup zones for the legacy test suite.
func newTestServerWithZones(t *testing.T, allow []netip.Prefix, zones []string) (*Server, *ephemeral.Store) {
	t.Helper()
	store := ephemeral.NewStore()
	cfg := &shadowdnscfg.EphemeralAPIConfig{
		Listen: "127.0.0.1:0",
		Allow:  allow,
	}
	lister := func() []string { return zones }
	return NewServer(cfg, store, lister, zap.NewNop()), store
}

// TestServer_PUT_OutOfBailiwickReturns422 covers the typo-rejection scenario
// from the spec: only example.com. is loaded, so PUT on _acme-challenge.exmaple.com
// SHALL be rejected with 422 and leave the store untouched.
func TestServer_PUT_OutOfBailiwickReturns422(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	s, store := newTestServerWithZones(t, allow, []string{"example.com."})

	w := doRequest(s, "PUT", "/v1/txt/_acme-challenge.exmaple.com",
		`{"value":"test123","ttl":30}`, "127.0.0.1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.Lookup("_acme-challenge.exmaple.com."); ok {
		t.Error("store must not be modified on 422 response")
	}

	// Error body MUST use the existing {"status":"error", ...} shape and
	// name the rejected FQDN for debuggability.
	var errBody struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Status != "error" {
		t.Errorf("Status = %q, want \"error\"", errBody.Status)
	}
	if !strings.Contains(errBody.Error, "_acme-challenge.exmaple.com.") {
		t.Errorf("error message %q should name the rejected FQDN", errBody.Error)
	}
}

// TestServer_PUT_InBailiwickRootZoneReturns200 is the green-path companion.
func TestServer_PUT_InBailiwickRootZoneReturns200(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	s, store := newTestServerWithZones(t, allow, []string{"example.com."})

	w := doRequest(s, "PUT", "/v1/txt/_acme-challenge.example.com",
		`{"value":"token123","ttl":120}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.Lookup("_acme-challenge.example.com."); !ok {
		t.Error("expected record to be stored on 200 response")
	}
}

// TestServer_PUT_InBailiwickBackupZoneReturns200 verifies backup zones are
// counted the same as root zones by the membership check.
func TestServer_PUT_InBailiwickBackupZoneReturns200(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	s, store := newTestServerWithZones(t, allow, []string{"backup.com."})

	w := doRequest(s, "PUT", "/v1/txt/_acme-challenge.foo.backup.com",
		`{"value":"token-B","ttl":120}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.Lookup("_acme-challenge.foo.backup.com."); !ok {
		t.Error("expected record to be stored under backup-zone FQDN")
	}
}

// TestServer_PUT_ApexIsInBailiwick verifies a PUT on the zone origin itself
// (e.g. example.com equals zone origin example.com.) is accepted.
func TestServer_PUT_ApexIsInBailiwick(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	s, store := newTestServerWithZones(t, allow, []string{"example.com."})

	w := doRequest(s, "PUT", "/v1/txt/example.com",
		`{"value":"apex","ttl":120}`, "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.Lookup("example.com."); !ok {
		t.Error("expected record to be stored at the apex")
	}
}

// TestServer_DELETE_OutOfBailiwickStillReturns200 protects the spec rule that
// DELETE MUST remain idempotent regardless of zone membership.
func TestServer_DELETE_OutOfBailiwickStillReturns200(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	s, _ := newTestServerWithZones(t, allow, []string{"example.com."})

	w := doRequest(s, "DELETE", "/v1/txt/_acme-challenge.exmaple.com", "", "127.0.0.1", "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (DELETE must be idempotent regardless of zone membership)", w.Code)
	}
}

// TestServer_PUT_OversizeValuePrecedesZoneCheck verifies validation order:
// oversize value returns 400 before the zone-membership check runs, so
// callers never see 422 for malformed bodies.
func TestServer_PUT_OversizeValuePrecedesZoneCheck(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	// Deliberately load no zones — the FQDN is out-of-bailiwick, but the
	// oversize value SHALL trigger 400 first.
	s, store := newTestServerWithZones(t, allow, nil)

	oversize := strings.Repeat("x", 256)
	body := fmt.Sprintf(`{"value":%q,"ttl":120}`, oversize)
	w := doRequest(s, "PUT", "/v1/txt/foo.unknown.com", body, "127.0.0.1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (oversize value precedes zone check)", w.Code)
	}
	if _, ok := store.Lookup("foo.unknown.com."); ok {
		t.Error("store must not be modified on 400 response")
	}
}

// TestServer_PUT_IPACLPrecedesZoneCheck verifies the IP ACL rejects requests
// before the zone check runs: a source IP not on the allow list returns 403
// even when the FQDN is out of every loaded zone.
func TestServer_PUT_IPACLPrecedesZoneCheck(t *testing.T) {
	allow := []netip.Prefix{singleAddrPrefix(t, "10.0.0.5")}
	s, _ := newTestServerWithZones(t, allow, nil)

	w := doRequest(s, "PUT", "/v1/txt/foo.unknown.com",
		`{"value":"v","ttl":60}`, "192.168.99.1", "")
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (IP ACL precedes zone check)", w.Code)
	}
}

// TestServer_PUT_TokenAuthPrecedesZoneCheck verifies token auth rejects
// requests before the zone check: an invalid token returns 401 even when
// the FQDN is out of every loaded zone. Covers the spec's validation-order
// clause ("after IP ACL, token authentication, ...").
func TestServer_PUT_TokenAuthPrecedesZoneCheck(t *testing.T) {
	// Enable token + source IP passes ACL, but lister returns no zones.
	allow := []netip.Prefix{singleAddrPrefix(t, "127.0.0.1")}
	store := ephemeral.NewStore()
	cfg := &shadowdnscfg.EphemeralAPIConfig{
		Listen: "127.0.0.1:0",
		Allow:  allow,
		Token:  "secret123",
	}
	s := NewServer(cfg, store, func() []string { return nil }, zap.NewNop())

	w := doRequest(s, "PUT", "/v1/txt/foo.unknown.com",
		`{"value":"v","ttl":60}`, "127.0.0.1", "Bearer wrong")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (token auth precedes zone check)", w.Code)
	}
}

// ---------- NewServer returns nil when config absent ----------

func TestNewServer_NilConfigReturnsNil(t *testing.T) {
	s := NewServer(nil, ephemeral.NewStore(), func() []string { return nil }, zap.NewNop())
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
	s := NewServer(cfg, store, func() []string { return []string{"example.com."} }, zap.NewNop())

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
		_ = resp.Body.Close()
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
