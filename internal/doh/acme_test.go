package doh

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
)

// selfSigned builds a self-signed *tls.Certificate with the given common name
// and validity window, with Leaf populated. Used to exercise the cert manager
// without ACME.
func selfSigned(t *testing.T, cn string, notBefore, notAfter time.Time) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		IPAddresses:  []net.IP{net.ParseIP("203.0.113.10")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// fakeCertMetrics records renewal results and the last expiry set.
type fakeCertMetrics struct {
	mu        sync.Mutex
	successes int
	failures  int
	lastSet   time.Time
}

func (f *fakeCertMetrics) RecordDoHCertRenewal(result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if result == "success" {
		f.successes++
	} else {
		f.failures++
	}
}

func (f *fakeCertMetrics) SetDoHCertNotAfter(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSet = t
}

// ---- HTTP-01 challenge responder: nginx `return 444` semantics ----

// fakeDropMetrics records ACME HTTP-01 listener drop reasons so a handler test
// can assert which bounded reason was counted (and that valid traffic is not).
type fakeDropMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func (f *fakeDropMetrics) RecordDoHACMEDropped(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[reason]++
}

func (f *fakeDropMetrics) count(reason string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[reason]
}

func (f *fakeDropMetrics) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.counts {
		n += c
	}
	return n
}

// assertAborts invokes h with the given request and asserts it aborts the
// connection by panicking with http.ErrAbortHandler without writing any HTTP
// response — no status line, no headers, no body, and in particular no 301
// redirect (the ServeMux fingerprint this change removes).
func assertAborts(t *testing.T, h http.Handler, method, target string) {
	t.Helper()
	rec := httptest.NewRecorder()
	defer func() {
		r := recover()
		if r != http.ErrAbortHandler {
			t.Fatalf("%s %s: recover() = %v, want http.ErrAbortHandler", method, target, r)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("%s %s: wrote body %q, want none", method, target, rec.Body.String())
		}
		if len(rec.Header()) != 0 {
			t.Errorf("%s %s: wrote headers %v, want none", method, target, rec.Header())
		}
	}()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
}

func TestChallengeResponder_ServesKeyAuthAndAborts(t *testing.T) {
	drops := &fakeDropMetrics{}
	c := newChallengeResponder(nil, drops)
	if err := c.Present("203.0.113.10", "tok123", "keyauth-value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	h := c.Handler()

	t.Run("valid token GET returns key authorization and does not drop", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, acmeChallengeBasePath+"tok123", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != "keyauth-value" {
			t.Errorf("body = %q, want keyauth-value", got)
		}
		if drops.total() != 0 {
			t.Errorf("valid request incremented drop metric: %d", drops.total())
		}
	})

	t.Run("unknown path aborts with reason unknown_path", func(t *testing.T) {
		before := drops.count("unknown_path")
		assertAborts(t, h, http.MethodGet, "/")
		if got := drops.count("unknown_path"); got != before+1 {
			t.Errorf("unknown_path count = %d, want %d", got, before+1)
		}
	})

	t.Run("unknown token aborts with reason unknown_token", func(t *testing.T) {
		before := drops.count("unknown_token")
		assertAborts(t, h, http.MethodGet, acmeChallengeBasePath+"nope")
		if got := drops.count("unknown_token"); got != before+1 {
			t.Errorf("unknown_token count = %d, want %d", got, before+1)
		}
	})

	t.Run("empty token aborts with reason unknown_token", func(t *testing.T) {
		before := drops.count("unknown_token")
		assertAborts(t, h, http.MethodGet, acmeChallengeBasePath)
		if got := drops.count("unknown_token"); got != before+1 {
			t.Errorf("unknown_token count = %d, want %d", got, before+1)
		}
	})

	t.Run("trailing-slash-less base path aborts without redirect, reason unknown_token", func(t *testing.T) {
		before := drops.count("unknown_token")
		assertAborts(t, h, http.MethodGet, "/.well-known/acme-challenge")
		if got := drops.count("unknown_token"); got != before+1 {
			t.Errorf("unknown_token count = %d, want %d", got, before+1)
		}
	})

	t.Run("non-GET method on challenge path aborts with reason bad_method", func(t *testing.T) {
		before := drops.count("bad_method")
		assertAborts(t, h, http.MethodPost, acmeChallengeBasePath+"tok123")
		if got := drops.count("bad_method"); got != before+1 {
			t.Errorf("bad_method count = %d, want %d", got, before+1)
		}
	})

	// Self-contained: uses its own responder so it does not mutate the shared
	// fixture's tokens (which the other subtests rely on still being present).
	// This keeps the subtests order- and parallel-independent.
	t.Run("cleanup makes a previously-valid token abort", func(t *testing.T) {
		localDrops := &fakeDropMetrics{}
		lc := newChallengeResponder(nil, localDrops)
		if err := lc.Present("203.0.113.10", "tok123", "keyauth-value"); err != nil {
			t.Fatalf("Present: %v", err)
		}
		if err := lc.CleanUp("203.0.113.10", "tok123", "keyauth-value"); err != nil {
			t.Fatalf("CleanUp: %v", err)
		}
		assertAborts(t, lc.Handler(), http.MethodGet, acmeChallengeBasePath+"tok123")
		if got := localDrops.count("unknown_token"); got != 1 {
			t.Errorf("unknown_token count after cleanup = %d, want 1", got)
		}
	})
}

// counterValue returns the value of the series in the named metric family whose
// labels match want exactly, and whether such a series exists.
func counterValue(t *testing.T, m *metrics.Metrics, name string, want map[string]string) (float64, bool) {
	t.Helper()
	mfs, err := m.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, lp := range metric.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			match := true
			for k, v := range want {
				if labels[k] != v {
					match = false
					break
				}
			}
			if match {
				return metric.GetCounter().GetValue(), true
			}
		}
	}
	return 0, false
}

// TestChallengeResponder_AbortDoesNotIncrementPanics drives the handler through
// a real net/http server (so net/http's per-request recover handles the
// ErrAbortHandler panic, as in production) with real metrics injected, and
// proves the abort path increments shadowdns_doh_acme_dropped_total but never
// shadowdns_panics_total — the responder is not on ShadowDNS's ServeDNS recover.
func TestChallengeResponder_AbortDoesNotIncrementPanics(t *testing.T) {
	m := metrics.New()
	c := newChallengeResponder(nil, m)
	srv := httptest.NewServer(c.Handler())
	defer srv.Close()

	// An unknown-path GET: net/http closes the connection without a response,
	// so the client observes a transport error rather than a status code.
	resp, err := http.Get(srv.URL + "/")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("expected aborted connection (transport error), got status %d", resp.StatusCode)
	}

	if v, ok := counterValue(t, m, "shadowdns_doh_acme_dropped_total", map[string]string{"reason": "unknown_path"}); !ok || v < 1 {
		t.Errorf("dropped_total{reason=unknown_path} = %v (found=%v), want >= 1", v, ok)
	}
	if v, ok := counterValue(t, m, "shadowdns_panics_total", nil); !ok || v != 0 {
		t.Errorf("shadowdns_panics_total = %v (found=%v), want 0 (abort must not count as a ShadowDNS panic)", v, ok)
	}
}

// ---- Task 4.3: hot-swap and renewal-failure handling ----

// TestCertManager_HotSwapWithoutRestart proves the GetCertificate callback
// serves a renewed certificate on the next handshake over the SAME listener,
// with no restart.
func TestCertManager_HotSwapWithoutRestart(t *testing.T) {
	now := time.Now()
	certA := selfSigned(t, "certA", now.Add(-time.Hour), now.Add(time.Hour))
	certB := selfSigned(t, "certB", now.Add(-time.Hour), now.Add(2*time.Hour))

	var mu sync.Mutex
	next := certA
	cm := newCertManager(func(context.Context) (*tls.Certificate, error) {
		mu.Lock()
		defer mu.Unlock()
		return next, nil
	}, nil, nil)
	if _, err := cm.obtainAndStore(context.Background()); err != nil {
		t.Fatalf("initial obtain: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{GetCertificate: cm.GetCertificate})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go acceptLoop(ln)

	if cn := handshakeCN(t, ln.Addr().String()); cn != "certA" {
		t.Fatalf("first handshake CN = %q, want certA", cn)
	}

	// Renew to cert B and obtain again on the running manager.
	mu.Lock()
	next = certB
	mu.Unlock()
	if _, err := cm.obtainAndStore(context.Background()); err != nil {
		t.Fatalf("renew obtain: %v", err)
	}

	// Same listener, next handshake must present the renewed certificate.
	if cn := handshakeCN(t, ln.Addr().String()); cn != "certB" {
		t.Errorf("post-renewal handshake CN = %q, want certB (listener must not restart)", cn)
	}
}

func TestCertManager_RenewalFailureRetainsCert(t *testing.T) {
	now := time.Now()
	certA := selfSigned(t, "certA", now.Add(-time.Hour), now.Add(time.Hour))

	fm := &fakeCertMetrics{}
	failing := func(context.Context) (*tls.Certificate, error) { return nil, io.ErrUnexpectedEOF }
	cm := newCertManager(failing, fm, nil)
	cm.cert.Store(certA) // pretend a previous obtain succeeded

	_, err := cm.obtainAndStore(context.Background())
	if err == nil {
		t.Fatal("obtainAndStore succeeded, want error")
	}
	got, gerr := cm.GetCertificate(nil)
	if gerr != nil {
		t.Fatalf("GetCertificate after failed renew: %v", gerr)
	}
	if got != certA {
		t.Error("current certificate was replaced despite renewal failure")
	}
	if fm.failures != 1 {
		t.Errorf("recorded failures = %d, want 1", fm.failures)
	}
}

func TestCertManager_GetCertificateBeforeObtain(t *testing.T) {
	cm := newCertManager(func(context.Context) (*tls.Certificate, error) { return nil, nil }, nil, nil)
	if _, err := cm.GetCertificate(nil); err == nil {
		t.Error("GetCertificate returned nil error before any obtain, want error")
	}
}

func TestRenewDelay(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 6-day lifetime; renew lead = 2 days before expiry.
	cert := selfSigned(t, "x", base, base.Add(6*24*time.Hour))
	// At issuance, renewal should be ~4 days away (6 - 2).
	if d := renewDelay(cert, base); d < 3*24*time.Hour || d > 5*24*time.Hour {
		t.Errorf("renewDelay at issuance = %v, want ~4d", d)
	}
	// Past the lead time, delay is 0 (renew now).
	if d := renewDelay(cert, base.Add(5*24*time.Hour)); d != 0 {
		t.Errorf("renewDelay past lead = %v, want 0", d)
	}
	// Nil cert renews immediately.
	if d := renewDelay(nil, base); d != 0 {
		t.Errorf("renewDelay(nil) = %v, want 0", d)
	}
}

// ---- test helpers for the TLS handshake ----

func acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			if tc, ok := c.(*tls.Conn); ok {
				_ = tc.Handshake()
			}
			_ = c.Close()
		}(conn)
	}
}

func handshakeCN(t *testing.T, addr string) string {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("no peer certificates")
	}
	return certs[0].Subject.CommonName
}

// TestBuildIPCSR_NoIPInCommonName guards the RFC 8738 / Let's Encrypt
// requirement that an IP-address certificate's CSR carries the IP in the
// SubjectAltName only, never in the Common Name. The pebble integration test
// cannot catch a regression here because pebble does not enforce the rule;
// real Let's Encrypt rejects an IP-in-CN CSR with badCSR at finalize.
func TestBuildIPCSR_NoIPInCommonName(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.10")
	csr, key, err := buildIPCSR(ip)
	if err != nil {
		t.Fatalf("buildIPCSR: %v", err)
	}
	if key == nil {
		t.Fatal("buildIPCSR returned nil private key")
	}
	if csr.Subject.CommonName != "" {
		t.Errorf("CommonName = %q, want empty (IP must not appear in CN)", csr.Subject.CommonName)
	}
	if len(csr.IPAddresses) != 1 || !csr.IPAddresses[0].Equal(net.IP(ip.AsSlice())) {
		t.Errorf("IPAddresses = %v, want [%s] in SAN", csr.IPAddresses, ip)
	}
	if len(csr.DNSNames) != 0 {
		t.Errorf("DNSNames = %v, want none for an IP certificate", csr.DNSNames)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR signature invalid: %v", err)
	}
}
