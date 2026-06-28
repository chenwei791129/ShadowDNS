package doh

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
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

// TestServer_RunServesThenShutsDown starts the DoH server with a self-signed
// certificate (no ACME), confirms the HTTPS /dns-query endpoint answers, and
// asserts the listener stops accepting connections after the context is
// cancelled (spec: graceful shutdown on SIGTERM).
func TestServer_RunServesThenShutsDown(t *testing.T) {
	dnsSrv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.20", 300)))
	cfg := testDoHConfig()
	cfg.Listen = freeAddr(t)
	cfg.ACME.HTTP01Listen = freeAddr(t)
	s := NewServer(dnsSrv, cfg, nil, nil)

	cert := selfSigned(t, "doh-test", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cm := newCertManager(func(context.Context) (*tls.Certificate, error) { return cert, nil }, nil, nil)
	responder := newChallengeResponder(nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.runWith(ctx, responder, cm) }()

	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	url := "https://" + cfg.Listen + dohPath

	// Poll until the HTTPS server is up and serving (cert obtained, listener
	// accepting).
	var lastErr error
	var served bool
	for i := 0; i < 50; i++ {
		resp, err := client.Post(url, dnsMediaType, bytes.NewReader(queryMsg("www.example.com.")))
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && len(body) > 0 {
			served = true
			break
		}
		lastErr = nil
		time.Sleep(50 * time.Millisecond)
	}
	if !served {
		cancel()
		<-done
		t.Fatalf("DoH HTTPS endpoint never served a query; last error: %v", lastErr)
	}

	// Shut down and confirm the listener stops accepting.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runWith did not return after context cancellation")
	}

	if conn, err := net.DialTimeout("tcp", cfg.Listen, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Error("DoH listener still accepting connections after shutdown")
	}
}
