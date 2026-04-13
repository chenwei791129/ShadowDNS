package server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/miekg/dns"
)

// Start begins serving DNS on listenAddr over both UDP and TCP.
// It returns when ctx is cancelled or either listener experiences a fatal error.
//
// The function pre-binds both listeners before spawning goroutines so that the
// listening ports are available immediately after Start returns (useful in tests
// that need to know the actual bound address).
func (s *Server) Start(ctx context.Context, listenAddr string) error {
	// Pre-bind UDP so callers can discover the actual port immediately.
	pc, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("bind UDP %s: %w", listenAddr, err)
	}

	// Pre-bind TCP listener.
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("bind TCP %s: %w", listenAddr, err)
	}

	udpServer := &dns.Server{
		PacketConn:   pc,
		Net:          "udp",
		Handler:      s,
		UDPSize:      dns.DefaultMsgSize,
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	tcpServer := &dns.Server{
		Listener:     ln,
		Net:          "tcp",
		Handler:      s,
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	s.udp = udpServer
	s.tcp = tcpServer

	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := udpServer.ActivateAndServe(); err != nil {
			errCh <- fmt.Errorf("UDP listener: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := tcpServer.ActivateAndServe(); err != nil {
			errCh <- fmt.Errorf("TCP listener: %w", err)
		}
	}()

	// Wait for context cancellation or a fatal listener error.
	var firstErr error
	select {
	case <-ctx.Done():
		// Graceful shutdown.
	case firstErr = <-errCh:
		// One listener died; shut down the other.
	}

	_ = udpServer.Shutdown()
	_ = tcpServer.Shutdown()

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// UDPAddr returns the local UDP address the server is bound to.
// Returns nil if the server has not been started.
func (s *Server) UDPAddr() net.Addr {
	if s.udp == nil || s.udp.PacketConn == nil {
		return nil
	}
	return s.udp.PacketConn.LocalAddr()
}

// TCPAddr returns the local TCP address the server is bound to.
// Returns nil if the server has not been started.
func (s *Server) TCPAddr() net.Addr {
	if s.tcp == nil || s.tcp.Listener == nil {
		return nil
	}
	return s.tcp.Listener.Addr()
}
