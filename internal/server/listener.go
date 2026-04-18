package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"

	"github.com/miekg/dns"
)

// Bind binds a single UDP/TCP address. It is a thin wrapper over BindMany
// kept for backward compatibility with existing tests that pass a single
// "host:port" string (e.g. "127.0.0.1:0"). New callers should use BindMany.
func (s *Server) Bind(listenAddr string) error {
	return s.BindMany([]string{listenAddr})
}

// BindMany attempts to bind a UDP + TCP listener pair for each address in
// addrs. Each address is treated as an atomic pair: if either the UDP or
// TCP half fails, the already-opened half is closed and the address is
// counted as failed (logged at WARN level); binding continues with the
// remaining addresses. BindMany returns nil if at least one address was
// successfully bound, or a fatal error if every address failed.
//
// After BindMany returns successfully, s.listeners holds one entry per
// successfully bound address, in the input order (with failures omitted).
// Serve can then be called to start accepting queries on every pair.
func (s *Server) BindMany(addrs []string) error {
	if len(addrs) == 0 {
		return errors.New("bind: no addresses provided")
	}

	bound := make([]listenerPair, 0, len(addrs))
	for _, addr := range addrs {
		pair, err := s.bindPair(addr)
		if err != nil {
			s.logBindFailure(addr, err)
			continue
		}
		bound = append(bound, pair)
		if s.Logger != nil {
			s.Logger.Sugar().Infow("listener bound", "addr", addr)
		}
	}

	if len(bound) == 0 {
		return fmt.Errorf("bind: no listeners bound (tried %d addresses)", len(addrs))
	}

	// Close any pre-existing listeners before replacing the slice. This
	// prevents a socket leak if BindMany (or the legacy single-addr Bind
	// shim) is called twice on the same Server instance.
	if len(s.listeners) > 0 {
		s.shutdownListeners()
	}
	s.listeners = bound
	return nil
}

// bindPair opens a UDP + TCP listener on addr, returning them as a
// listenerPair. If either half fails, the already-opened half is closed
// and an error is returned.
func (s *Server) bindPair(addr string) (listenerPair, error) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return listenerPair{}, fmt.Errorf("bind UDP %s: %w", addr, err)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = pc.Close()
		return listenerPair{}, fmt.Errorf("bind TCP %s: %w", addr, err)
	}

	pair := listenerPair{
		addr: addr,
		udp: &dns.Server{
			PacketConn:   pc,
			Net:          "udp",
			Handler:      s,
			UDPSize:      dns.DefaultMsgSize,
			ReadTimeout:  0,
			WriteTimeout: 0,
		},
		tcp: &dns.Server{
			Listener:     ln,
			Net:          "tcp",
			Handler:      s,
			ReadTimeout:  0,
			WriteTimeout: 0,
		},
	}
	return pair, nil
}

// logBindFailure emits the WARN log for a failed bind attempt, attaching a
// systemd-resolved hint when the failure is EADDRINUSE on a loopback address
// (typical of systemd-resolved's stub listener on 127.0.0.53 / 127.0.0.54).
func (s *Server) logBindFailure(addr string, err error) {
	if s.Logger == nil {
		return
	}
	args := []any{"addr", addr, "err", err}
	if isLoopbackAddrInUse(addr, err) {
		args = append(args,
			"hint",
			"likely systemd-resolved stub on loopback; set DNSStubListener=no in /etc/systemd/resolved.conf if this address is expected",
		)
	}
	s.Logger.Sugar().Warnw("listener bind failed; skipping address", args...)
}

// isLoopbackAddrInUse reports whether addr is in the IPv4 loopback range
// (127.0.0.0/8) and the underlying syscall error was EADDRINUSE.
func isLoopbackAddrInUse(addr string, err error) bool {
	if !errors.Is(err, syscall.EADDRINUSE) {
		return false
	}
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4[0] == 127
}

// Serve starts the serve loop on all bound listeners and blocks until ctx
// is cancelled or a listener encounters a fatal error. Bind or BindMany
// must be called before Serve.
func (s *Server) Serve(ctx context.Context) error {
	if len(s.listeners) == 0 {
		return errors.New("Serve: no listeners bound (call Bind or BindMany first)")
	}

	errCh := make(chan error, len(s.listeners)*2)
	var wg sync.WaitGroup

	for i := range s.listeners {
		pair := &s.listeners[i]
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := pair.udp.ActivateAndServe(); err != nil {
				errCh <- fmt.Errorf("UDP listener %s: %w", pair.addr, err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := pair.tcp.ActivateAndServe(); err != nil {
				errCh <- fmt.Errorf("TCP listener %s: %w", pair.addr, err)
			}
		}()
	}

	// Wait for ctx cancellation or a fatal listener error. Only the first
	// error is captured and returned; if ctx cancellation races with a
	// listener failure, additional errors from other listeners during
	// shutdown are intentionally dropped into the buffered errCh and then
	// garbage-collected with the local variable. The primary failure is
	// preserved as firstErr; secondary cascade failures are noise.
	var firstErr error
	select {
	case <-ctx.Done():
		// Graceful shutdown.
	case firstErr = <-errCh:
		// One listener died; shut down the rest.
	}

	s.shutdownListeners()
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// shutdownListeners closes every bound listener. Safe to call multiple
// times and safe on a Server with no listeners. Used by Serve on exit
// and when BindMany is invoked on a Server that already holds listeners.
//
// We close the underlying PacketConn / net.Listener directly *in addition
// to* calling dns.Server.Shutdown, because Shutdown only closes the socket
// if the server was previously started via ActivateAndServe. When BindMany
// is called twice in rapid succession (e.g. a misbehaving test or future
// reload-rebind attempt), the first batch was bound but never activated,
// so Shutdown is a no-op and the kernel socket leaks unless we close it
// here. Close() is idempotent enough — a second close returns an error
// that we discard.
func (s *Server) shutdownListeners() {
	var wg sync.WaitGroup
	for i := range s.listeners {
		pair := &s.listeners[i]
		if pair.udp != nil {
			wg.Add(1)
			go func(srv *dns.Server) {
				defer wg.Done()
				_ = srv.Shutdown()
				if srv.PacketConn != nil {
					_ = srv.PacketConn.Close()
				}
			}(pair.udp)
		}
		if pair.tcp != nil {
			wg.Add(1)
			go func(srv *dns.Server) {
				defer wg.Done()
				_ = srv.Shutdown()
				if srv.Listener != nil {
					_ = srv.Listener.Close()
				}
			}(pair.tcp)
		}
	}
	wg.Wait()
}

// Start is a convenience method that calls Bind followed by Serve.
func (s *Server) Start(ctx context.Context, listenAddr string) error {
	if err := s.Bind(listenAddr); err != nil {
		return err
	}
	return s.Serve(ctx)
}

// UDPAddrs returns the local addresses of every successfully bound UDP
// listener, in the order they were bound.
func (s *Server) UDPAddrs() []net.Addr {
	out := make([]net.Addr, 0, len(s.listeners))
	for i := range s.listeners {
		if s.listeners[i].udp != nil && s.listeners[i].udp.PacketConn != nil {
			out = append(out, s.listeners[i].udp.PacketConn.LocalAddr())
		}
	}
	return out
}

// TCPAddrs returns the local addresses of every successfully bound TCP
// listener, in the order they were bound.
func (s *Server) TCPAddrs() []net.Addr {
	out := make([]net.Addr, 0, len(s.listeners))
	for i := range s.listeners {
		if s.listeners[i].tcp != nil && s.listeners[i].tcp.Listener != nil {
			out = append(out, s.listeners[i].tcp.Listener.Addr())
		}
	}
	return out
}

// UDPAddr returns the first successfully bound UDP address, or nil. Retained
// for tests that assume a single listener; new code should use UDPAddrs.
func (s *Server) UDPAddr() net.Addr {
	addrs := s.UDPAddrs()
	if len(addrs) == 0 {
		return nil
	}
	return addrs[0]
}

// TCPAddr returns the first successfully bound TCP address, or nil. Retained
// for tests that assume a single listener; new code should use TCPAddrs.
func (s *Server) TCPAddr() net.Addr {
	addrs := s.TCPAddrs()
	if len(addrs) == 0 {
		return nil
	}
	return addrs[0]
}

// BoundAddrStrings returns the "host:port" string each successfully bound
// listener pair was asked to bind to, in the pre-kernel-assignment form
// exactly as passed to BindMany. One string per pair.
//
// Note: For addresses using an ephemeral port like "127.0.0.1:0", this
// returns "127.0.0.1:0" — NOT the OS-assigned port. Use UDPAddrs() or
// TCPAddrs() for the actual post-bind addresses. This method is intended
// for startup logging and for reload-time drift detection (comparing
// against ResolveListenAddresses output, which also uses the requested
// form).
func (s *Server) BoundAddrStrings() []string {
	out := make([]string, 0, len(s.listeners))
	for i := range s.listeners {
		out = append(out, s.listeners[i].addr)
	}
	return out
}
