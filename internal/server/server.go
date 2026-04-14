// Package server implements the ShadowDNS authoritative DNS server.
// It ties together the view matcher, alias resolver, and zone data to answer
// DNS queries via UDP and TCP.
package server

import (
	"log/slog"
	"sync/atomic"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// ServerState carries all pre-loaded state required to construct a Server.
// The caller (cmd/shadowdns/main.go or a test) is responsible for building this.
type ServerState struct {
	Matcher *view.Matcher
	Aliases config.AliasMap
	// view name → zone origin → *zone.Zone (root zones)
	RootZones map[string]map[string]*zone.Zone
	// view name → zone origin → *zone.Zone (backup-override zones; may be empty or nil)
	BackupZones map[string]map[string]*zone.Zone
	// pre-computed: view name → []string of all loaded zone origins (root + backup)
	ZoneOrigins map[string][]string
	// AllowTransferACL controls which source IPs may request zone transfers.
	// A nil ACL or an empty ACL both deny all transfers (secure default).
	AllowTransferACL *transfer.ACL
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
}

// Server is the main DNS server object.  It implements dns.Handler so it can be
// passed directly to miekg's dns.Server.
//
// The server state is held behind an atomic pointer so that it can be replaced
// at runtime (e.g. on SIGHUP) without restarting listeners or blocking
// in-flight queries.
type Server struct {
	state  atomic.Pointer[ServerState]
	Logger *slog.Logger

	udp *dns.Server
	tcp *dns.Server
}

// NewServer constructs a Server from pre-loaded state and a logger.
// Neither state nor logger may be nil.
func NewServer(state ServerState, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	state.sanitize()
	s := &Server{
		Logger: logger,
	}
	s.state.Store(&state)
	return s
}

// SwapState atomically replaces the server's in-memory state.
// In-flight requests that already loaded the old state will complete
// using their snapshot; new requests will see the replacement.
func (s *Server) SwapState(state ServerState) {
	state.sanitize()
	s.state.Store(&state)
}
