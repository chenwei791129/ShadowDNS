package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// newBareServer builds a minimal Server suitable for exercising BindMany /
// Serve mechanics. It has empty state so it answers no queries; these tests
// only care about socket binding.
func newBareServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(ServerState{}, silentLogger())
}

// preBindUDP occupies a UDP address so BindMany sees EADDRINUSE. The returned
// addr is the one that is now taken. t.Cleanup closes the socket.
func preBindUDP(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("preBindUDP: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return pc.LocalAddr().String()
}

// preBindTCPOnly occupies a TCP address without taking the matching UDP port.
// Used to test the UDP-succeeds-TCP-fails path. Returned addr is the occupied one.
func preBindTCPOnly(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("preBindTCPOnly: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// ---------------------------------------------------------------------------
// 3.1 BindMany two ephemeral addresses → both succeed, UDPAddrs returns both
// ---------------------------------------------------------------------------

func TestBindMany_TwoEphemeralAddrsBothSucceed(t *testing.T) {
	srv := newBareServer(t)
	err := srv.BindMany([]string{"127.0.0.1:0", "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("BindMany failed: %v", err)
	}
	defer srv.shutdownListeners()

	udp := srv.UDPAddrs()
	tcp := srv.TCPAddrs()
	if len(udp) != 2 {
		t.Errorf("expected 2 UDP listeners, got %d", len(udp))
	}
	if len(tcp) != 2 {
		t.Errorf("expected 2 TCP listeners, got %d", len(tcp))
	}
	// Sanity: UDPAddr() still returns the first one (test-compat path).
	if srv.UDPAddr() == nil {
		t.Error("UDPAddr() should return first listener")
	}
	if srv.TCPAddr() == nil {
		t.Error("TCPAddr() should return first listener")
	}
}

// ---------------------------------------------------------------------------
// 3.2 Partial failure: one address taken → WARN + continue, server starts
// ---------------------------------------------------------------------------

func TestBindMany_PartialFailureContinues(t *testing.T) {
	occupied := preBindUDP(t)
	logger, buf := newTestLogger()
	srv := NewServer(ServerState{}, logger)

	err := srv.BindMany([]string{occupied, "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("BindMany should have succeeded with partial bind: %v", err)
	}
	defer srv.shutdownListeners()

	udp := srv.UDPAddrs()
	if len(udp) != 1 {
		t.Fatalf("expected 1 successful UDP listener, got %d", len(udp))
	}
	logOut := buf.String()
	if !strings.Contains(logOut, "warn") && !strings.Contains(logOut, "WARN") {
		t.Errorf("expected WARN log for failed address, got: %s", logOut)
	}
	if !strings.Contains(logOut, occupied) {
		t.Errorf("WARN log should mention the failed address %q, got: %s", occupied, logOut)
	}
}

// ---------------------------------------------------------------------------
// 3.3 All addresses fail → fatal error with attempt count
// ---------------------------------------------------------------------------

func TestBindMany_AllFailReturnsFatalError(t *testing.T) {
	occupied1 := preBindUDP(t)
	occupied2 := preBindUDP(t)
	srv := newBareServer(t)

	err := srv.BindMany([]string{occupied1, occupied2})
	if err == nil {
		srv.shutdownListeners()
		t.Fatal("expected fatal error when all addresses fail, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2") {
		t.Errorf("error should mention the attempted count (2), got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// 3.4 UDP succeeds, TCP same address fails → UDP half closed, address counted failed
// ---------------------------------------------------------------------------

func TestBindMany_TCPFailAfterUDPSuccessClosesUDPHalf(t *testing.T) {
	// preBindTCPOnly occupies TCP on an ephemeral port without taking UDP.
	tcpOccupied := preBindTCPOnly(t)
	srv := newBareServer(t)

	err := srv.BindMany([]string{tcpOccupied})
	if err == nil {
		srv.shutdownListeners()
		t.Fatal("expected fatal error (only addr failed due to TCP conflict), got nil")
	}

	// BindMany should have Close()d the successfully-opened UDP half before
	// returning. We confirm by trying to bind UDP on the same address now —
	// it should succeed, meaning the UDP port was released.
	pc, err := net.ListenPacket("udp", tcpOccupied)
	if err != nil {
		t.Fatalf("UDP half was not closed after TCP failure: %v", err)
	}
	_ = pc.Close()
}

// ---------------------------------------------------------------------------
// 3.5 Loopback EADDRINUSE WARN contains systemd-resolved hint
// ---------------------------------------------------------------------------

func TestBindMany_LoopbackEADDRINUSEHasResolvedHint(t *testing.T) {
	occupied := preBindUDP(t) // always 127.0.0.1:XXXXX
	logger, buf := newTestLogger()
	srv := NewServer(ServerState{}, logger)

	// Add a second address that succeeds so BindMany doesn't return fatal.
	err := srv.BindMany([]string{occupied, "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("BindMany: %v", err)
	}
	defer srv.shutdownListeners()

	logOut := buf.String()
	if !strings.Contains(logOut, "DNSStubListener=no") {
		t.Errorf("loopback EADDRINUSE WARN should include systemd-resolved hint 'DNSStubListener=no', got: %s", logOut)
	}
}

// ---------------------------------------------------------------------------
// Regression: Bind(single) still works (legacy shim)
// ---------------------------------------------------------------------------

func TestBind_SingleAddrStillWorks(t *testing.T) {
	srv := newBareServer(t)
	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("single-addr Bind shim failed: %v", err)
	}
	defer srv.shutdownListeners()

	if srv.UDPAddr() == nil {
		t.Error("UDPAddr nil after single-addr Bind")
	}
	if len(srv.UDPAddrs()) != 1 {
		t.Errorf("UDPAddrs len = %d, want 1", len(srv.UDPAddrs()))
	}
}

// ---------------------------------------------------------------------------
// Serve() over multiple listeners shuts down cleanly on ctx cancel
// ---------------------------------------------------------------------------

func TestServe_MultipleListenersShutdownOnCtxCancel(t *testing.T) {
	srv := newBareServer(t)
	if err := srv.BindMany([]string{"127.0.0.1:0", "127.0.0.1:0"}); err != nil {
		t.Fatalf("BindMany: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	// Give Serve goroutines time to start.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Serve returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel within 2s")
	}
}
