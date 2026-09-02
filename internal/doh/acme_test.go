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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

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
	}, nil, nil, 0)
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
	cm := newCertManager(failing, fm, nil, 0)
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
	cm := newCertManager(func(context.Context) (*tls.Certificate, error) { return nil, nil }, nil, nil, 0)
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

// newDelayedCertManager returns a certManager whose obtain path is an inert
// stub and whose startup delay is delay. logger may be nil (newCertManager
// substitutes a no-op). Shared by the startup-delay tests, which differ only in
// the delay and in whether they observe the log.
func newDelayedCertManager(delay time.Duration, logger *zap.Logger) *certManager {
	return newCertManager(func(context.Context) (*tls.Certificate, error) { return nil, nil }, nil, logger, delay)
}

// TestCertManager_WaitInitialDelay covers the cancellable wait that precedes
// the first obtain attempt of the process: zero delay proceeds immediately, a
// positive delay actually elapses, and a cancellation during the wait reports
// that the loop must not proceed. The positive case asserts only the lower
// bound on elapsed time (scheduling jitter makes an upper bound flaky) and uses
// a very short duration to keep the test fast.
func TestCertManager_WaitInitialDelay(t *testing.T) {
	t.Run("zero delay proceeds immediately", func(t *testing.T) {
		cm := newDelayedCertManager(0, nil)
		start := time.Now()
		if !cm.waitInitialDelay(context.Background()) {
			t.Fatal("waitInitialDelay = false, want true (zero delay must proceed)")
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Errorf("zero delay waited %v, want no wait", elapsed)
		}
	})

	t.Run("positive delay elapses before proceeding", func(t *testing.T) {
		const delay = 40 * time.Millisecond
		cm := newDelayedCertManager(delay, nil)
		start := time.Now()
		if !cm.waitInitialDelay(context.Background()) {
			t.Fatal("waitInitialDelay = false, want true (uncancelled wait must proceed)")
		}
		if elapsed := time.Since(start); elapsed < delay {
			t.Errorf("waited %v, want at least %v", elapsed, delay)
		}
	})

	t.Run("cancellation during the wait aborts", func(t *testing.T) {
		cm := newDelayedCertManager(time.Hour, nil)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		defer cancel()
		if cm.waitInitialDelay(ctx) {
			t.Error("waitInitialDelay = true after cancellation, want false")
		}
	})
}

// TestCertManager_InitialDelayLog asserts the observability of the startup
// wait: a positive delay emits exactly one informational entry carrying the
// configured duration (and no challenge or key material), while a zero delay
// stays silent.
func TestCertManager_InitialDelayLog(t *testing.T) {
	newCM := func(delay time.Duration) (*certManager, *observer.ObservedLogs) {
		core, obs := observer.New(zapcore.InfoLevel)
		return newDelayedCertManager(delay, zap.New(core)), obs
	}

	t.Run("positive delay logs the configured duration", func(t *testing.T) {
		const delay = 20 * time.Millisecond
		cm, obs := newCM(delay)
		cm.waitInitialDelay(context.Background())

		entries := obs.All()
		if len(entries) != 1 {
			t.Fatalf("logged %d entries, want 1: %+v", len(entries), entries)
		}
		e := entries[0]
		if e.Level != zapcore.InfoLevel {
			t.Errorf("level = %v, want info", e.Level)
		}
		if !strings.Contains(e.Message, "delaying initial ACME") {
			t.Errorf("message = %q, want it to state that initial ACME issuance is delayed", e.Message)
		}
		if got := e.ContextMap()["delay"]; got != delay {
			t.Errorf("delay field = %v, want %v", got, delay)
		}
	})

	t.Run("zero delay logs nothing", func(t *testing.T) {
		cm, obs := newCM(0)
		cm.waitInitialDelay(context.Background())
		if n := obs.Len(); n != 0 {
			t.Errorf("logged %d entries with zero delay, want 0: %+v", n, obs.All())
		}
	})
}

// TestCertManager_RunCancelledDuringInitialDelay drives the background loop
// with a long initial delay and cancels mid-wait. The loop must return without
// ever calling the obtain path, so shutdown records no renewal outcome at all —
// a cancelled startup window is not a certificate failure.
func TestCertManager_RunCancelledDuringInitialDelay(t *testing.T) {
	var calls atomic.Int64
	fm := &fakeCertMetrics{}
	cm := newCertManager(func(context.Context) (*tls.Certificate, error) {
		calls.Add(1)
		return nil, io.ErrUnexpectedEOF
	}, fm, nil, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cm.run(ctx)
	}()

	time.Sleep(10 * time.Millisecond) // let run enter the wait
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cm.run did not return after cancellation during the initial delay")
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("obtain called %d times during a cancelled initial delay, want 0", n)
	}
	if fm.failures != 0 {
		t.Errorf("recorded failures = %d, want 0 (shutdown is not a renewal failure)", fm.failures)
	}
	if fm.successes != 0 {
		t.Errorf("recorded successes = %d, want 0", fm.successes)
	}
}

// TestCertManager_InitialDelayDoesNotAffectRetryTiming drives the background
// loop through a failing first obtain and its retry, and asserts the initial
// delay gates only the first attempt: the retry is spaced by retryInterval,
// which is chosen far below the initial delay so a reapplied startup wait would
// be unmistakable. The renewal side is covered structurally — the wait lives
// outside the for loop — plus TestRenewDelay guarding the pure schedule
// function; asserting the real renewRetryInterval (10m) or minRenewInterval
// (1m) would mean actually waiting minutes, and those constants are out of
// scope for this change.
func TestCertManager_InitialDelayDoesNotAffectRetryTiming(t *testing.T) {
	const (
		initialDelay  = 200 * time.Millisecond
		retryInterval = 5 * time.Millisecond
	)
	var (
		mu    sync.Mutex
		times []time.Time
	)
	twice := make(chan struct{})
	cm := newCertManager(func(context.Context) (*tls.Certificate, error) {
		mu.Lock()
		times = append(times, time.Now())
		n := len(times)
		mu.Unlock()
		if n == 2 {
			close(twice)
		}
		return nil, io.ErrUnexpectedEOF
	}, nil, nil, initialDelay)
	cm.retryInterval = retryInterval

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		cm.run(ctx)
	}()

	select {
	case <-twice:
	case <-time.After(10 * time.Second):
		t.Fatal("obtain was not attempted twice")
	}
	cancel()
	<-done

	mu.Lock()
	first, second := times[0], times[1]
	mu.Unlock()

	if d := first.Sub(start); d < initialDelay {
		t.Errorf("first obtain at +%v, want at least the initial delay %v", d, initialDelay)
	}
	gap := second.Sub(first)
	if gap < retryInterval {
		t.Errorf("retry gap = %v, want at least the retry interval %v", gap, retryInterval)
	}
	if gap >= initialDelay {
		t.Errorf("retry gap = %v, want well under the initial delay %v: the initial delay must not be reapplied to retries", gap, initialDelay)
	}
}
