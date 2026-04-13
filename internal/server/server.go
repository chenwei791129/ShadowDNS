// Package server implements the ShadowDNS authoritative DNS server.
// It ties together the view matcher, alias resolver, and zone data to answer
// DNS queries via UDP and TCP.
package server

import (
	"log/slog"

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

// Server is the main DNS server object.  It implements dns.Handler so it can be
// passed directly to miekg's dns.Server.
type Server struct {
	ServerState
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
	if state.RootZones == nil {
		state.RootZones = make(map[string]map[string]*zone.Zone)
	}
	if state.BackupZones == nil {
		state.BackupZones = make(map[string]map[string]*zone.Zone)
	}
	if state.ZoneOrigins == nil {
		state.ZoneOrigins = make(map[string][]string)
	}
	if state.Aliases == nil {
		state.Aliases = make(config.AliasMap)
	}
	return &Server{
		ServerState: state,
		Logger:      logger,
	}
}
