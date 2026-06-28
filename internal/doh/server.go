package doh

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/httpserver"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

const (
	// dohPath is the RFC 8484 query endpoint path.
	dohPath = "/dns-query"
	// dnsMediaType is the RFC 8484 request/response media type.
	dnsMediaType = "application/dns-message"
	// maxBodyBytes caps an accepted POST body. A DNS message cannot exceed
	// 65535 bytes (RFC 1035 §4.2.2 16-bit length prefix), so this is the
	// smallest spec-compliant cap; a larger body is rejected with 413 before
	// the query path runs.
	maxBodyBytes = 65535
)

// Server serves DNS-over-HTTPS (RFC 8484) by decoding HTTP requests and
// dispatching them through the shared authoritative query path. Construct with
// NewServer; obtain the HTTP handler with Handler (used by Run for the HTTPS
// listener and by tests via httptest).
type Server struct {
	// dns is the shared authoritative query path. It is held as the dns.Handler
	// interface (server.Server satisfies it) since only ServeDNS is invoked,
	// which also lets tests inject a stub to exercise the empty-capture guard.
	dns     dns.Handler
	cfg     *shadowdnscfg.DoHConfig
	metrics dohMetrics
	logger  *zap.Logger
}

// NewServer constructs a DoH Server. cfg nil (no doh section) returns nil so
// callers can skip starting the server, mirroring api.NewServer. A nil logger
// is replaced with a no-op. dnsHandler is the shared authoritative handler
// (server.Server); it MUST be non-nil when cfg is non-nil. m records
// certificate renewal metrics and may be nil.
//
// m is taken as the concrete *metrics.Metrics (not the dohMetrics interface)
// so a nil pointer becomes a true nil interface internally — passing a nil
// *metrics.Metrics straight into an interface field would be a non-nil
// interface holding a nil pointer (the typed-nil trap), defeating the nil
// checks in certManager and challengeResponder.
func NewServer(dnsHandler *server.Server, cfg *shadowdnscfg.DoHConfig, m *metrics.Metrics, logger *zap.Logger) *Server {
	if cfg == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	var dm dohMetrics
	if m != nil {
		dm = m
	}
	return &Server{dns: dnsHandler, cfg: cfg, metrics: dm, logger: logger}
}

// Run starts the DoH HTTPS server and the ACME HTTP-01 challenge listener,
// obtains and renews the TLS certificate in the background, and blocks until
// ctx is cancelled, then shuts both listeners down gracefully. It is intended
// to run in its own goroutine (like the ephemeral API server). A nil DoH
// Server (no doh section) makes this a no-op via the caller's nil check.
func (s *Server) Run(ctx context.Context) error {
	responder := newChallengeResponder(s.logger, s.metrics)
	obtain := newLazyLegoObtainer(s.cfg.ACME, responder)
	cm := newCertManager(obtain, s.metrics, s.logger)
	return s.runWith(ctx, responder, cm)
}

// runWith is the testable core of Run: it wires the port-80 challenge listener,
// the port-443 HTTPS server (TLS certificate supplied per-handshake by cm), and
// the certificate obtain/renew loop, then coordinates graceful shutdown. Tests
// inject a cm backed by a self-signed certificate to avoid real ACME.
func (s *Server) runWith(ctx context.Context, responder *challengeResponder, cm *certManager) error {
	// Derive a cancellable context so a listener bind/serve failure also stops
	// the certificate renewal loop. Without this, cm.run() only exits on the
	// parent ctx and wg.Wait() below would deadlock — leaking the goroutine and
	// hammering the ACME directory for an endpoint that never serves.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	challengeSrv := httpserver.NewServer(s.cfg.ACME.HTTP01Listen, responder.Handler())
	dohSrv := httpserver.NewServer(s.cfg.Listen, s.Handler())
	dohSrv.TLSConfig = &tls.Config{
		GetCertificate: cm.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cm.run(runCtx)
	}()

	// Each listener's serve + graceful drain runs through the shared httpserver
	// primitive. Both block on runCtx, so cancelling it (below) drives whichever
	// listener is still serving through its own bounded drain — this is how the
	// cross-listener failure propagation is preserved on top of the shared
	// primitive. Buffered for both serve goroutines so neither leaks if shutdown
	// wins the race. httpserver.Serve absorbs http.ErrServerClosed, so a value
	// here is either nil (clean stop) or a real bind/serve failure.
	errCh := make(chan error, 2)
	go func() {
		s.logger.Sugar().Infow("doh: ACME HTTP-01 listener starting", "listen", s.cfg.ACME.HTTP01Listen)
		errCh <- httpserver.Serve(runCtx, challengeSrv, "doh ACME HTTP-01", challengeSrv.ListenAndServe, s.logger)
	}()
	go func() {
		s.logger.Sugar().Infow("doh: HTTPS server starting", "listen", s.cfg.Listen)
		// Empty cert/key filenames: the certificate is supplied by
		// TLSConfig.GetCertificate (the ACME-managed atomic holder).
		errCh <- httpserver.Serve(runCtx, dohSrv, "doh HTTPS", func() error { return dohSrv.ListenAndServeTLS("", "") }, s.logger)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		// A listener stopped before shutdown was requested. Hand the result back
		// so the collector below remains the single site that logs serve errors
		// and captures the first one; cancel() then tears down the sibling and
		// the renewal loop. errCh has capacity 2 with at most one value
		// outstanding at this point, so the re-send never blocks.
		errCh <- err
	}

	// Cancel runCtx so the renewal loop stops and any still-serving listener
	// begins its graceful drain. Without this, wg.Wait() below could block on the
	// errCh-failure path (parent ctx still alive), leaking the renewal goroutine
	// and hammering the ACME directory for an endpoint that never serves.
	cancel()

	// Bound the renewal-loop wait. Start the deadline at cancel time so it elapses
	// concurrently with the listener drains below, keeping the worst-case graceful
	// shutdown within a single httpserver.ShutdownTimeout budget rather than
	// summing per-component deadlines (~10s, which would exceed main.go's deferred
	// drain and any outer systemd TimeoutStopSec). An in-flight lego obtain() does
	// not observe ctx cancellation (lego's ObtainForCSR takes no context), so
	// cm.run can stay blocked inside it — the goroutine is harmless once the
	// process exits.
	renewDone := make(chan struct{})
	go func() { wg.Wait(); close(renewDone) }()
	deadline := time.After(httpserver.ShutdownTimeout)

	// Collect both listener results. Each httpserver.Serve already performed its
	// server's bounded graceful drain (triggered by cancel), concurrently with the
	// other and with the renewal wait. This is the sole serve-error reporting site.
	var serveErr error
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			s.logger.Sugar().Errorw("doh: listener exited with error", "err", err)
			if serveErr == nil {
				serveErr = err
			}
		}
	}

	select {
	case <-renewDone:
	case <-deadline:
		s.logger.Warn("doh: certificate renewal loop still running at shutdown deadline; abandoning it")
	}

	return serveErr
}

// Handler returns the DoH HTTP handler. Registering method-qualified patterns
// makes the mux answer 404 for any path other than /dns-query and 405 for any
// method other than GET/POST on /dns-query (Go 1.22 ServeMux semantics).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+dohPath, s.handleGet)
	mux.HandleFunc("POST "+dohPath, s.handlePost)
	return mux
}

// handleGet serves either RFC 8484 wire-format or application/dns-json on the
// GET path. The `dns` parameter takes precedence: when present the request is
// always wire-format regardless of Accept, so a wire query is never misrouted
// to the JSON parser. With no `dns` parameter and an Accept header listing
// application/dns-json, the request is served as JSON; otherwise it falls back
// to the wire path (which rejects the missing `dns` parameter per RFC 8484).
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	encoded := query.Get("dns")
	if encoded == "" {
		if acceptsDNSJSON(r.Header.Get("Accept")) {
			s.serveJSON(w, r, query)
			return
		}
		http.Error(w, "missing dns query parameter", http.StatusBadRequest)
		return
	}
	// Bound the decode the same way the POST path bounds its body: a DNS
	// message cannot exceed maxBodyBytes, so reject an oversize dns= parameter
	// with 413 before allocating/decoding it (DecodedLen is the upper bound on
	// the decoded size).
	if base64.RawURLEncoding.DecodedLen(len(encoded)) > maxBodyBytes {
		http.Error(w, "dns query parameter exceeds maximum DNS message size", http.StatusRequestEntityTooLarge)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "invalid base64url dns parameter", http.StatusBadRequest)
		return
	}
	s.serve(w, r, raw)
}

// handlePost reads the wire-format request body, rejecting an oversize body
// with 413 before the query path runs.
func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	limited := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body exceeds maximum DNS message size", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "error reading request body", http.StatusBadRequest)
		return
	}
	if len(raw) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}
	s.serve(w, r, raw)
}

// serve unpacks the raw DNS message, dispatches it through the authoritative
// query path via a synthetic writer carrying the HTTP connection's source IP,
// and writes the wire-format response with a TTL-bounded cache header.
func (s *Server) serve(w http.ResponseWriter, r *http.Request, raw []byte) {
	req := new(dns.Msg)
	if err := req.Unpack(raw); err != nil {
		http.Error(w, "malformed DNS message", http.StatusBadRequest)
		return
	}

	rw := newResponseWriter(remoteTCPAddr(r), localTCPAddr(r))
	s.dispatch(rw, req)

	if len(rw.packed) == 0 {
		// ServeDNS always writes a response on every path; an empty capture
		// means an internal failure (e.g. Pack error) rather than a client
		// error.
		s.logger.Warn("doh: query path produced no response")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", dnsMediaType)
	setCacheControl(h, rw.msg)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rw.packed)
}

// setCacheControl sets the TTL-bounded Cache-Control header shared by both DoH
// transports: max-age is the smallest Answer TTL (0 for an empty answer), so a
// response never advertises a lifetime beyond its shortest record. Centralizing
// it keeps the wire and JSON paths from drifting on this cache policy.
func setCacheControl(h http.Header, m *dns.Msg) {
	h.Set("Cache-Control", "max-age="+strconv.FormatUint(uint64(minAnswerTTL(m)), 10))
}

// dispatch runs req through the shared authoritative query path, capturing the
// response in rw. It is the single dispatch point for both the wire and JSON
// transports, so they share identical zone-transfer refusal semantics: AXFR/
// IXFR stream multiple messages over one connection, which has no meaning in a
// single request/response exchange (and the single-shot synthetic writer would
// capture only the last envelope), so they are refused at the DoH layer with a
// well-formed REFUSED response rather than entering the transfer path.
func (s *Server) dispatch(rw *responseWriter, req *dns.Msg) {
	if isZoneTransferQuery(req) {
		refused := new(dns.Msg)
		refused.SetReply(req)
		refused.RecursionAvailable = false
		refused.Rcode = dns.RcodeRefused
		_ = rw.WriteMsg(refused)
		return
	}
	s.dns.ServeDNS(rw, req)
}

// minAnswerTTL returns the smallest TTL among the response Answer records, or
// 0 when the Answer section is empty or the message is unavailable. The DoH
// cache header is set to this value so it never exceeds the smallest answer
// TTL and never advertises a positive lifetime for an empty Answer.
func minAnswerTTL(m *dns.Msg) uint32 {
	if m == nil || len(m.Answer) == 0 {
		return 0
	}
	min := m.Answer[0].Header().Ttl
	for _, rr := range m.Answer[1:] {
		if ttl := rr.Header().Ttl; ttl < min {
			min = ttl
		}
	}
	return min
}

// isZoneTransferQuery reports whether req is an AXFR or IXFR request. Such
// queries are refused over DoH (see serve): a zone transfer is a multi-message
// stream with no representation in a single RFC 8484 exchange.
func isZoneTransferQuery(req *dns.Msg) bool {
	if len(req.Question) != 1 {
		return false
	}
	switch req.Question[0].Qtype {
	case dns.TypeAXFR, dns.TypeIXFR:
		return true
	}
	return false
}

// remoteTCPAddr builds a *net.TCPAddr from the HTTP connection's peer address.
// Only the TCP connection is consulted — never X-Forwarded-For / Forwarded —
// so a client cannot select a view by forging a header. A best-effort zero
// address is returned on parse failure; the query path then refuses the query
// (it cannot derive a source IP), exactly as for an unparsable TCP peer.
// netip.ParseAddrPort is used over net.ResolveTCPAddr to avoid the latter's
// heavier resolution path and extra allocation on every request.
func remoteTCPAddr(r *http.Request) *net.TCPAddr {
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return net.TCPAddrFromAddrPort(ap)
	}
	return &net.TCPAddr{}
}

// localTCPAddr returns the server's local address for this connection (set by
// net/http via http.LocalAddrContextKey), or a placeholder when unavailable.
// It is always a *net.TCPAddr so dnsutil.IsUDP reports false.
func localTCPAddr(r *http.Request) *net.TCPAddr {
	if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if tcp, ok := la.(*net.TCPAddr); ok {
			return tcp
		}
	}
	return &net.TCPAddr{}
}
