package main

// Startup integration test for the DoH server wiring in run(): a doh section
// makes run() start the HTTPS and ACME HTTP-01 listeners, and cancelling the
// context stops them. The ACME directory points at a closed local port so the
// background certificate obtain fails fast (no cert is needed to prove the TCP
// listeners bind).

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func requireTCPDialable(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("address %s never became dialable", addr)
}

func TestRun_StartsAndStopsDoHListeners(t *testing.T) {
	dir := setupReloadTestDir(t)
	dohAddr := freePortAddr(t)
	http01Addr := freePortAddr(t)
	writeShadowYAML(t, dir, fmt.Sprintf(`aliases: {}
doh:
  listen: %q
  acme:
    directory_url: "https://127.0.0.1:1/dir"
    ip: "203.0.113.10"
    http01_listen: %q
`, dohAddr, http01Addr))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    freePortAddr(t),
		Logger:        zap.NewNop(),
		ReadyCh:       readyCh,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()
	waitReady(t, readyCh)

	// Both DoH listeners should bind shortly after startup, even though the
	// background ACME obtain fails (closed directory port).
	requireTCPDialable(t, dohAddr)
	requireTCPDialable(t, http01Addr)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit within 5s after cancel")
	}

	// The DoH server shuts down in its own goroutine (fire-and-forget like the
	// ephemeral API server), so poll for the listener to stop accepting within
	// the graceful-shutdown window.
	requireTCPClosed(t, dohAddr)
}

func requireTCPClosed(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("address %s still accepting connections after shutdown", addr)
}
