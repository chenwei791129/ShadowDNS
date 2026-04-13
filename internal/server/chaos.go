package server

import (
	"strings"

	"github.com/miekg/dns"
)

// chaosIdentityNames are CHAOS TXT names used by clients to probe server identity.
// We always REFUSE these to hide the implementation.
var chaosIdentityNames = map[string]bool{
	"version.bind.":  true,
	"hostname.bind.": true,
	"id.server.":     true,
}

// handleChaos replies REFUSED for all CHAOS-class queries.
// The server never reveals version, hostname, or any identity information.
func handleChaos(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Opcode = dns.OpcodeQuery
	m.RecursionAvailable = false
	m.Authoritative = false
	m.Rcode = dns.RcodeRefused

	// Ensure no answer/authority/additional data leaks identity.
	m.Answer = nil
	m.Ns = nil
	m.Extra = nil

	_ = w.WriteMsg(m)
}

// isChaosIdentityQuery returns true when the request is a CHAOS-class TXT query
// for one of the server-identity names (version.bind, hostname.bind, id.server).
func isChaosIdentityQuery(req *dns.Msg) bool {
	if len(req.Question) == 0 {
		return false
	}
	q := req.Question[0]
	return q.Qclass == dns.ClassCHAOS &&
		q.Qtype == dns.TypeTXT &&
		chaosIdentityNames[strings.ToLower(q.Name)]
}
