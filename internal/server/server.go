// Package server implements the ShadowDNS authoritative DNS server.
// It ties together the view matcher, alias resolver, and zone data to answer
// DNS queries via UDP and TCP.
package server

import (
	"crypto/rand"
	"runtime"
	"runtime/debug"
	"sync/atomic"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/cookie"
	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/querylog"
	"github.com/chenwei791129/ShadowDNS/internal/ratelimit"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// ServerState carries all pre-loaded state required to construct a Server.
// The caller (cmd/shadowdns/main.go or a test) is responsible for building this.
type ServerState struct {
	Matcher *view.Matcher
	Aliases config.AliasMap
	// AliasFlags is keyed by backup origin and reports whether RDATA name
	// fields for that alias group should be rewritten using the
	// label-anywhere rule. A missing key (or a nil map) is equivalent to
	// false (in-bailiwick suffix-only rewrite).
	AliasFlags config.AliasFlags
	// BackupOriginalCase maps the lookup-fold backup origin (lowercase, with
	// trailing dot) to the operator-authored original case that the alias
	// rewrite path emits on the wire. A missing key (or a nil map) is
	// equivalent to "no case-preserving override" — callers must fall back to
	// the lookup-fold form.
	BackupOriginalCase map[string]string
	// view name → zone origin → *zone.Zone (root zones)
	RootZones map[string]map[string]*zone.Zone
	// view name → zone origin → *zone.Zone (backup-override zones; may be empty or nil)
	BackupZones map[string]map[string]*zone.Zone
	// pre-computed: view name → []string of all loaded zone origins (root + backup)
	ZoneOrigins map[string][]string
	// AllowTransferACL controls which source IPs may request zone transfers.
	// A nil ACL or an empty ACL both deny all transfers (secure default).
	AllowTransferACL *transfer.ACL
	// Fingerprints stores the zone file fingerprints recorded during the last
	// successful BuildState call. Keyed by (view name, zone origin). Used by
	// the next reload to detect unchanged zones and reuse *zone.Zone pointers.
	Fingerprints map[string]map[string]zoneFingerprint
}

// AllOrigins returns a flat slice of every loaded zone origin across all
// views (root + backup). The same origin may appear more than once when it
// is present in multiple views; callers that require uniqueness MUST
// deduplicate. Origins are canonical (lowercase, trailing dot) because
// BuildState stores them that way.
func (s *ServerState) AllOrigins() []string {
	var out []string
	for _, origins := range s.ZoneOrigins {
		out = append(out, origins...)
	}
	return out
}

// ZoneCount returns the total number of loaded zones (root + backup) across
// all views.
func (s *ServerState) ZoneCount() int {
	n := 0
	for _, zones := range s.RootZones {
		n += len(zones)
	}
	for _, zones := range s.BackupZones {
		n += len(zones)
	}
	return n
}

// sanitize ensures all map fields are non-nil so callers never need nil checks.
func (s *ServerState) sanitize() {
	if s.RootZones == nil {
		s.RootZones = make(map[string]map[string]*zone.Zone)
	}
	if s.BackupZones == nil {
		s.BackupZones = make(map[string]map[string]*zone.Zone)
	}
	if s.ZoneOrigins == nil {
		s.ZoneOrigins = make(map[string][]string)
	}
	if s.Aliases == nil {
		s.Aliases = make(config.AliasMap)
	}
	if s.AliasFlags == nil {
		s.AliasFlags = make(config.AliasFlags)
	}
	if s.BackupOriginalCase == nil {
		s.BackupOriginalCase = make(map[string]string)
	}
	if s.Fingerprints == nil {
		s.Fingerprints = make(map[string]map[string]zoneFingerprint)
	}
}

// Server is the main DNS server object.  It implements dns.Handler so it can be
// passed directly to miekg's dns.Server.
//
// The server state is held behind an atomic pointer so that it can be replaced
// at runtime (e.g. on SIGHUP) without restarting listeners or blocking
// in-flight queries.
type Server struct {
	state  atomic.Pointer[ServerState]
	Logger *zap.Logger
	// Metrics enables Prometheus metrics collection when non-nil.
	// A nil value disables all instrumentation (safe for tests).
	Metrics *metrics.Metrics

	// RateLimiter applies BIND-compatible response rate limiting to UDP
	// responses when the stored pointer is non-nil. A nil value disables rate
	// limiting entirely (the wrapper is never installed, so the response path
	// has zero added cost). It is built at startup from the options-block
	// rate-limit config and rebuilt on SIGHUP reload; the atomic pointer lets
	// the reload goroutine swap it while handlers read it concurrently.
	RateLimiter atomic.Pointer[ratelimit.Limiter]

	// EphemeralStore is an in-memory store of TXT records created via the
	// ephemeral HTTP API (ACME DNS-01 challenges). It lives outside
	// ServerState so SIGHUP reload does not replace or clear it; the reload
	// handler calls Clear explicitly after a successful swap. When nil, no
	// ephemeral TXT lookups are performed.
	EphemeralStore *ephemeral.Store

	// QueryLog enables BIND9-compatible per-query logging when the stored
	// pointer is non-nil. A nil value disables all query logging (safe for
	// tests and when no logging{} block is configured). The hot path guards
	// every entry-build and Log call behind a single nil check so disabled
	// logging adds no overhead. The atomic pointer lets the SIGHUP reload
	// goroutine replace the logger while handlers read it concurrently.
	QueryLog atomic.Pointer[querylog.Logger]

	// listeners holds one entry per successfully bound address. Each entry
	// owns a UDP *dns.Server and a TCP *dns.Server sharing the same address.
	// Populated by Bind / BindMany; consumed by Serve.
	listeners []listenerPair

	// gcHook, when non-nil, is called instead of runtime.GC()+debug.FreeOSMemory()
	// after a successful SwapState. Used in tests to observe GC invocations.
	gcHook func()

	// cookieGen computes RFC 9018 server cookies (RFC 7873 answer-only
	// mode). It is keyed once at startup from crypto/rand and deliberately
	// lives outside ServerState so a SIGHUP reload never rotates the
	// secret — cookies issued before a reload stay valid. The secret is
	// held in memory only and changes on every process restart.
	cookieGen *cookie.Generator
}

// listenerPair bundles the UDP and TCP dns.Server instances for a single
// listen address. Both halves are bound as an atomic pair; if either fails
// the pair is discarded and neither is retained.
type listenerPair struct {
	addr string
	udp  *dns.Server
	tcp  *dns.Server
}

// NewServer constructs a Server from pre-loaded state and a logger.
// Neither state nor logger may be nil.
func NewServer(state ServerState, logger *zap.Logger) *Server {
	if logger == nil {
		logger = zap.NewNop()
	}
	state.sanitize()
	// crypto/rand.Read never returns an error on Go 1.24+ (the runtime
	// aborts the process on catastrophic entropy failure), so secret
	// generation needs no error-handling path.
	var secret [cookie.SecretLen]byte
	_, _ = rand.Read(secret[:])
	s := &Server{
		Logger:    logger,
		cookieGen: cookie.New(secret),
	}
	s.state.Store(&state)
	return s
}

// CurrentState returns a pointer to the server's current in-memory state.
func (s *Server) CurrentState() *ServerState {
	return s.state.Load()
}

// SwapState atomically replaces the server's in-memory state.
// In-flight requests that already loaded the old state will complete
// using their snapshot; new requests will see the replacement.
// After the swap, it triggers a GC cycle to reclaim memory held by the
// old state (zone records, matcher structures, etc.).
//
// Note: runtime.GC() and debug.FreeOSMemory() run synchronously in the
// caller's goroutine (typically the SIGHUP handler). At large zone counts
// this introduces a short stop-the-world pause that briefly stalls query
// handlers. The trade-off is intentional: returning memory to the OS
// promptly after reload matters more than absolute latency smoothness
// during an already-disruptive reload event.
func (s *Server) SwapState(state ServerState) {
	state.sanitize()
	s.state.Store(&state)
	s.updateZoneMetrics(&state)
	if s.gcHook != nil {
		s.gcHook()
	} else {
		runtime.GC()
		debug.FreeOSMemory()
	}
}

// updateZoneMetrics pushes per-view zone counts to the Prometheus gauges.
// No-op when s.Metrics is nil.
func (s *Server) updateZoneMetrics(st *ServerState) {
	if s.Metrics == nil {
		return
	}
	root := make(map[string]int, len(st.RootZones))
	for v, zones := range st.RootZones {
		root[v] = len(zones)
	}
	backup := make(map[string]int, len(st.BackupZones))
	for v, zones := range st.BackupZones {
		backup[v] = len(zones)
	}
	s.Metrics.SetZoneCounts(root, backup)
}
