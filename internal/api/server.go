// Package api implements the HTTP API server for ephemeral TXT records.
// The server exposes PUT and DELETE endpoints under /v1/txt/{fqdn} for
// creating and removing ephemeral TXT records. Access control is performed
// by a source-IP ACL and, optionally, a pre-shared bearer token.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

// TTL clamp bounds and shutdown timeout are fixed by spec.
const (
	MinTTL          = 1
	MaxTTL          = 3600
	ShutdownTimeout = 5 * time.Second
	// MaxValueBytes is the RFC 1035 TXT character-string length limit.
	MaxValueBytes = 255
)

// Server wraps the ephemeral TXT API. Construct with NewServer; start with
// Run (which binds the listener) or Serve (for an already-bound listener).
type Server struct {
	cfg     *shadowdnscfg.EphemeralAPIConfig
	store   *ephemeral.Store
	logger  *zap.Logger
	handler http.Handler
}

// NewServer constructs a Server from an API config, ephemeral store, and
// logger. If cfg is nil (no ephemeral_api section), NewServer returns nil
// so callers can simply check and skip starting the server.
func NewServer(cfg *shadowdnscfg.EphemeralAPIConfig, store *ephemeral.Store, logger *zap.Logger) *Server {
	if cfg == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Server{
		cfg:    cfg,
		store:  store,
		logger: logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/txt/{fqdn}", s.handlePut)
	mux.HandleFunc("DELETE /v1/txt/{fqdn}", s.handleDelete)
	s.handler = s.ipACLMiddleware(s.tokenMiddleware(mux))
	return s
}

// Run binds to cfg.Listen and serves until ctx is cancelled, then shuts down
// gracefully with a 5-second deadline for in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("api: listen %q: %w", s.cfg.Listen, err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves on the supplied listener until ctx is cancelled, then shuts
// down gracefully. The listener is closed by http.Server.Shutdown.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	httpServer := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Warn("api: graceful shutdown failed", zap.Error(err))
			return err
		}
		// Drain Serve's final error (http.ErrServerClosed).
		<-errCh
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// ---------- middleware ----------

func (s *Server) ipACLMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, ok := remoteIP(r)
		if !ok {
			writeError(w, http.StatusForbidden, "unable to parse remote address")
			return
		}
		if !allowContains(s.cfg.Allow, addr) {
			writeError(w, http.StatusForbidden, "source IP not in allow list")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}
		supplied := auth[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(s.cfg.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- handlers ----------

type putRequest struct {
	Value *string `json:"value"`
	TTL   int     `json:"ttl"`
}

type putResponseBody struct {
	Status string `json:"status"`
	FQDN   string `json:"fqdn"`
	TTL    int    `json:"ttl"`
	// Count is the total number of ephemeral entries currently held for
	// the affected FQDN (including the entry just added/refreshed). Lets
	// ACME clients detect whether their value coexists with a concurrent
	// challenge's value under the same name.
	Count int `json:"count"`
}

type okResponseBody struct {
	Status string `json:"status"`
	FQDN   string `json:"fqdn"`
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	fqdn, ok := canonicalFQDN(w, r)
	if !ok {
		return
	}

	var req putRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}
	if req.Value == nil {
		writeError(w, http.StatusBadRequest, "missing required field: value")
		return
	}

	if err := validateValue(*req.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ttl := clampTTL(req.TTL)
	count := s.store.Put(fqdn, *req.Value, uint32(ttl))
	writeJSON(w, http.StatusOK, putResponseBody{Status: "ok", FQDN: fqdn, TTL: ttl, Count: count})
}

// handleDelete removes ephemeral entries for the FQDN. When the ?value=
// query parameter is absent, every entry under the FQDN is wiped. When
// present, only the matching entry is removed (idempotent: non-matching
// returns 200). A present-but-empty ?value= is rejected with 400 so it
// cannot be confused with the wipe-all path. Zone file records are never
// touched.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	fqdn, ok := canonicalFQDN(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if !q.Has("value") {
		s.store.Delete(fqdn)
		writeJSON(w, http.StatusOK, okResponseBody{Status: "ok", FQDN: fqdn})
		return
	}
	value := q.Get("value")
	if value == "" {
		writeError(w, http.StatusBadRequest, "empty value query parameter")
		return
	}
	if err := validateValue(value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.store.DeleteValue(fqdn, value)
	writeJSON(w, http.StatusOK, okResponseBody{Status: "ok", FQDN: fqdn})
}

// canonicalFQDN extracts and canonicalizes the {fqdn} path parameter. It
// writes a 400 response and returns ok=false when the parameter is empty.
func canonicalFQDN(w http.ResponseWriter, r *http.Request) (string, bool) {
	fqdn := dnsutil.Canonicalize(r.PathValue("fqdn"))
	if fqdn == "" {
		writeError(w, http.StatusBadRequest, "missing FQDN")
		return "", false
	}
	return fqdn, true
}

// ---------- helpers ----------

// validateValue enforces the RFC 1035 TXT character-string 255-byte limit.
// Empty values pass this check. PUT accepts them (no ACME use case needs
// "" but rejecting it is out of this change's scope); DELETE's ?value=
// handler rejects present-but-empty values before calling validateValue.
func validateValue(v string) error {
	if len(v) > MaxValueBytes {
		return fmt.Errorf("value exceeds %d-byte limit (got %d)", MaxValueBytes, len(v))
	}
	return nil
}

func clampTTL(ttl int) int {
	switch {
	case ttl < MinTTL:
		return MinTTL
	case ttl > MaxTTL:
		return MaxTTL
	default:
		return ttl
	}
}

func remoteIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may already be just a host in odd test harnesses.
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func allowContains(allow []netip.Prefix, addr netip.Addr) bool {
	for _, p := range allow {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

type errorBody struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorBody{Status: "error", Error: msg})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
