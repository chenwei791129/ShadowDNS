// Package httpserver is the shared run/drain lifecycle primitive for
// ShadowDNS's background HTTP servers (the Prometheus metrics server, the
// ephemeral TXT API server, and the DoH HTTPS/ACME servers). It owns the single
// graceful-shutdown deadline, the hardened connection-timeout defaults, and the
// serve -> drain -> filter-ErrServerClosed core, so the three servers cannot
// drift in their timeouts, shutdown deadline, or error reporting.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ShutdownTimeout is the single graceful-shutdown drain deadline shared by every
// background HTTP server (spec: up to 5s). Defining it once here is the source
// of truth; no background server defines its own.
const ShutdownTimeout = 5 * time.Second

// Connection hygiene timeouts applied to every background HTTP server so none is
// left with net/http's unbounded defaults. They bound slow-loris and
// idle-connection exposure; the source sets are firewall-restricted, so the
// values need not be tight.
const (
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 120 * time.Second
	readHeaderTimeout = 5 * time.Second
)

// Option customizes a server built by NewServer.
type Option func(*http.Server)

// WithWriteTimeout overrides the default write timeout. Pass WithWriteTimeout(0)
// to leave the write timeout unbounded for a server that hosts long-running
// streaming handlers (e.g. the metrics server's pprof /debug/pprof/profile and
// /debug/pprof/trace endpoints, which a fixed write timeout would truncate).
func WithWriteTimeout(d time.Duration) Option {
	return func(s *http.Server) { s.WriteTimeout = d }
}

// NewServer builds an *http.Server with hardened read, idle, and read-header
// timeouts plus a default write timeout, so no background server is left with
// net/http's unbounded defaults. addr may be empty when the caller serves on an
// already-bound listener. Apply options to override individual fields.
func NewServer(addr string, h http.Handler, opts ...Option) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	for _, opt := range opts {
		opt(srv)
	}
	return srv
}

// Serve starts srv via the caller-supplied start closure in a goroutine, blocks
// until ctx is cancelled or the server stops serving, and returns the first
// error that is not http.ErrServerClosed (nil on a normal shutdown).
//
// start MUST drive srv: it is one of srv.Serve(ln) / srv.ListenAndServe() /
// srv.ListenAndServeTLS(...). On ctx cancellation Serve drains srv through Drain
// (bounded by ShutdownTimeout, derived from a fresh context.Background() so an
// already-cancelled ctx cannot turn the drain into a no-op), then absorbs the
// resulting http.ErrServerClosed and returns nil.
func Serve(ctx context.Context, srv *http.Server, label string, start func() error, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	errCh := make(chan error, 1)
	go func() { errCh <- start() }()

	select {
	case <-ctx.Done():
		Drain(srv, label, logger)
		// Drain start()'s final error (http.ErrServerClosed on a clean stop).
		<-errCh
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Drain gracefully shuts srv down within ShutdownTimeout, logging a warning on
// failure. It is the single per-server drain primitive: Serve calls it on ctx
// cancellation, and the DoH subsystem's concurrent drain coordination calls it
// per listener, so the deadline and warning format cannot drift across callers.
// The drain context is derived from a fresh context.Background() so a parent ctx
// that is already cancelled cannot collapse the drain into an immediate no-op.
func Drain(srv *http.Server, label string, logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Sugar().Warnw(label+": graceful shutdown failed", "err", err)
	}
}
