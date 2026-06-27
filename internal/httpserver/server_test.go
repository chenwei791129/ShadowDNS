package httpserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

// freeAddr returns a currently-free 127.0.0.1 host:port by binding and
// releasing an ephemeral port. There is a small reuse race, acceptable for a
// loopback test.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeAddr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// selfSignedCert builds a minimal self-signed certificate for the TLS
// closure-form test. No client verifies it, so the contents only need to be a
// structurally valid certificate that GetCertificate can return.
func selfSignedCert(t *testing.T) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "httpserver-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// TestServe_DrainsInflightOnCancel verifies the ctx-cancellation path runs a
// real graceful drain: an in-flight request started before cancellation is
// allowed to complete. If Serve mistakenly drained with the already-cancelled
// ctx, Shutdown would return immediately (context.Canceled) and the in-flight
// request would be cut, failing this test.
func TestServe_DrainsInflightOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	releaseHandler := make(chan struct{})
	handlerEntered := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(handlerEntered)
		<-releaseHandler
		_, _ = w.Write([]byte("ok"))
	})
	srv := NewServer("", mux)

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(ctx, srv, "test", func() error { return srv.Serve(ln) }, nil) }()

	// Fire an in-flight request and wait until the handler is executing.
	respDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, rerr := client.Get("http://" + ln.Addr().String() + "/slow")
		if rerr != nil {
			respDone <- rerr
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			respDone <- errors.New("unexpected status")
			return
		}
		respDone <- nil
	}()

	select {
	case <-handlerEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never entered")
	}

	// Cancel while the request is in flight, then let the handler finish. A
	// correct graceful drain (fresh-context Shutdown) keeps the connection open
	// until the handler returns.
	cancel()
	close(releaseHandler)

	if rerr := <-respDone; rerr != nil {
		t.Errorf("in-flight request was not allowed to complete: %v", rerr)
	}
	select {
	case serr := <-serveDone:
		if serr != nil {
			t.Errorf("Serve returned %v, want nil on graceful shutdown", serr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

// TestServe_AbsorbsErrServerClosed covers the normal-shutdown scenario: a
// start closure that returns http.ErrServerClosed is reported as success.
func TestServe_AbsorbsErrServerClosed(t *testing.T) {
	srv := NewServer("", nil)
	err := Serve(context.Background(), srv, "test", func() error { return http.ErrServerClosed }, nil)
	if err != nil {
		t.Errorf("Serve = %v, want nil when start returns ErrServerClosed", err)
	}
}

// TestServe_SurfacesServeError covers the bind/serve-failure scenario: a start
// closure that fails with an error other than ErrServerClosed has that error
// returned to the caller.
func TestServe_SurfacesServeError(t *testing.T) {
	srv := NewServer("", nil)
	sentinel := errors.New("boom")
	err := Serve(context.Background(), srv, "test", func() error { return sentinel }, nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("Serve = %v, want %v", err, sentinel)
	}
}

// TestServe_SupportsListenAndServe verifies the ListenAndServe (addr) closure
// form (the metrics server's start method) drives the helper and drains on
// cancellation.
func TestServe_SupportsListenAndServe(t *testing.T) {
	srv := NewServer(freeAddr(t), http.NewServeMux())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, "test", srv.ListenAndServe, nil) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

// TestServe_SupportsListenAndServeTLS verifies the ListenAndServeTLS closure
// form (the DoH HTTPS start method, certificate supplied via
// TLSConfig.GetCertificate) drives the helper and drains on cancellation.
func TestServe_SupportsListenAndServeTLS(t *testing.T) {
	cert := selfSignedCert(t)
	srv := NewServer(freeAddr(t), http.NewServeMux())
	srv.TLSConfig = &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return cert, nil },
		MinVersion:     tls.VersionTLS12,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, srv, "test", func() error { return srv.ListenAndServeTLS("", "") }, nil)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

// TestNewServer_HardenedTimeouts verifies the default-built server carries
// bounded read, write, idle, and read-header timeouts (no net/http unbounded
// defaults).
func TestNewServer_HardenedTimeouts(t *testing.T) {
	s := NewServer(":0", nil)
	if s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 || s.ReadHeaderTimeout == 0 {
		t.Errorf("timeouts must all be non-zero: read=%v write=%v idle=%v header=%v",
			s.ReadTimeout, s.WriteTimeout, s.IdleTimeout, s.ReadHeaderTimeout)
	}
}

// TestNewServer_WithWriteTimeoutZero verifies WithWriteTimeout(0) leaves the
// write timeout unbounded (for pprof streaming) while keeping the other
// hardened timeouts non-zero.
func TestNewServer_WithWriteTimeoutZero(t *testing.T) {
	s := NewServer(":0", nil, WithWriteTimeout(0))
	if s.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (unbounded for streaming)", s.WriteTimeout)
	}
	if s.ReadTimeout == 0 || s.IdleTimeout == 0 || s.ReadHeaderTimeout == 0 {
		t.Errorf("non-write timeouts must stay non-zero: read=%v idle=%v header=%v",
			s.ReadTimeout, s.IdleTimeout, s.ReadHeaderTimeout)
	}
}
