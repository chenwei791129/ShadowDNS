package server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/miekg/dns"
)

// Bind pre-binds UDP and TCP listeners on listenAddr without starting the
// serve loop. After Bind returns successfully, UDPAddr() and TCPAddr() are
// available. Call Serve to begin accepting queries.
func (s *Server) Bind(listenAddr string) error {
	pc, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("bind UDP %s: %w", listenAddr, err)
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("bind TCP %s: %w", listenAddr, err)
	}

	s.udp = &dns.Server{
		PacketConn:   pc,
		Net:          "udp",
		Handler:      s,
		UDPSize:      dns.DefaultMsgSize,
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	s.tcp = &dns.Server{
		Listener:     ln,
		Net:          "tcp",
		Handler:      s,
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	return nil
}

// Serve starts the serve loop on already-bound listeners and blocks until ctx
// is cancelled or a listener encounters a fatal error. Bind must be called
// before Serve.
func (s *Server) Serve(ctx context.Context) error {
	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := s.udp.ActivateAndServe(); err != nil {
			errCh <- fmt.Errorf("UDP listener: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := s.tcp.ActivateAndServe(); err != nil {
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

	_ = s.udp.Shutdown()
	_ = s.tcp.Shutdown()

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// Start is a convenience method that calls Bind followed by Serve.
func (s *Server) Start(ctx context.Context, listenAddr string) error {
	if err := s.Bind(listenAddr); err != nil {
		return err
	}
	return s.Serve(ctx)
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
