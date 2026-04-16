package server

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/alias"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// ServeDNS implements dns.Handler. It is the entry point for every DNS query.
func (s *Server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	var mw *metrics.ResponseWriter
	var clientIP netip.Addr
	viewLabel := "refused"

	if s.Metrics != nil {
		origW := w
		mw = metrics.NewResponseWriter(w, s.Metrics, "refused", time.Now())
		w = mw

		defer func() {
			proto := "tcp"
			if dnsutil.IsUDP(origW) {
				proto = "udp"
			}
			qtypeStr := "unknown"
			if len(req.Question) == 1 {
				if name, ok := dns.TypeToString[req.Question[0].Qtype]; ok {
					qtypeStr = name
				}
			}
			s.Metrics.RecordRequest(proto, addrFamily(clientIP), qtypeStr, viewLabel)
		}()
	}

	// Recover from any panic so the server never crashes.
	defer func() {
		if r := recover(); r != nil {
			if s.Metrics != nil {
				s.Metrics.RecordPanic()
			}
			s.Logger.Error("panic in DNS handler; recovering",
				"panic", fmt.Sprintf("%v", r),
			)
			m := new(dns.Msg)
			m.SetReply(req)
			m.RecursionAvailable = false
			m.Rcode = dns.RcodeServerFailure
			_ = w.WriteMsg(m)
		}
	}()

	// Unsupported opcodes → NOTIMP.
	if req.Opcode != dns.OpcodeQuery {
		s.Logger.Info("unsupported opcode; replying NOTIMP",
			"opcode", req.Opcode,
			"remote", w.RemoteAddr().String(),
		)
		replyRcode(w, req, dns.RcodeNotImplemented)
		return
	}

	// Malformed query (wrong question count) → FORMERR.
	if len(req.Question) != 1 {
		s.Logger.Info("malformed query: question count != 1; replying FORMERR",
			"questions", len(req.Question),
			"remote", w.RemoteAddr().String(),
		)
		replyRcode(w, req, dns.RcodeFormatError)
		return
	}

	q := req.Question[0]
	qname := strings.ToLower(q.Name)
	qtype := q.Qtype
	qclass := q.Qclass

	// CHAOS class — refuse all to hide identity.
	if qclass == dns.ClassCHAOS {
		s.Logger.Info("CHAOS class query; replying REFUSED",
			"qname", qname,
			"remote", w.RemoteAddr().String(),
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	// Load a consistent state snapshot for this request. All subsequent
	// lookups within the same request use this snapshot, so a concurrent
	// reload cannot produce a half-old / half-new response.
	st := s.state.Load()

	// Zone transfer requests (AXFR / IXFR) are handled separately.
	// Note: transfers are counted under view="refused" in metrics because
	// clientIP and viewName are resolved inside handleTransfer, not here.
	if qtype == dns.TypeAXFR || qtype == dns.TypeIXFR {
		s.handleTransfer(w, req, qname, st)
		return
	}

	// Extract client IP for view resolution.
	var err error
	clientIP, err = addrFromRemote(w)
	if err != nil {
		s.Logger.Warn("cannot parse client IP; replying REFUSED",
			"remote", w.RemoteAddr().String(),
			"err", err,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	// Determine view from client IP.
	viewName := st.Matcher.Resolve(clientIP)
	if viewName == "" {
		s.Logger.Info("no view matched client; replying REFUSED",
			"client", clientIP.String(),
			"qname", qname,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	// View resolved — update metrics labels.
	viewLabel = viewName
	if mw != nil {
		mw.SetView(viewName)
	}

	// Determine zone using longest-suffix match + alias map.
	origins := st.ZoneOrigins[viewName]
	match := alias.Detect(qname, origins, st.Aliases)
	if match.MatchedZone == "" {
		s.Logger.Info("query outside all loaded zones; replying REFUSED",
			"view", viewName,
			"qname", qname,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	if match.IsBackup {
		s.handleBackupQuery(w, req, viewName, qname, qtype, match, st)
	} else {
		s.handleRootQuery(w, req, viewName, qname, qtype, match, st)
	}
}

// handleRootQuery answers a query from a root (non-alias) zone.
func (s *Server) handleRootQuery(
	w dns.ResponseWriter,
	req *dns.Msg,
	viewName, qname string,
	qtype uint16,
	match alias.Match,
	st *ServerState,
) {
	rootZone := st.RootZones[viewName][match.MatchedZone]
	if rootZone == nil {
		s.Logger.Error("root zone missing for matched origin; replying SERVFAIL",
			"view", viewName,
			"zone", match.MatchedZone,
		)
		replyRcode(w, req, dns.RcodeServerFailure)
		return
	}

	// SOA at apex short-circuit.
	if qtype == dns.TypeSOA && qname == rootZone.Origin {
		if rootZone.SOA != nil {
			replyWithAnswer(w, req, []dns.RR{rootZone.SOA})
		} else {
			s.negativeReply(w, req, rootZone, nil, match, qname, rootZone.SOA)
		}
		return
	}

	records := rootZone.Lookup(qname, qtype)
	if len(records) > 0 {
		replyWithAnswer(w, req, records)
		return
	}

	// CNAME fallback per RFC 1034 §3.6.2.
	if qtype != dns.TypeCNAME {
		if cnames := rootZone.Lookup(qname, dns.TypeCNAME); len(cnames) > 0 {
			replyWithAnswer(w, req, cnames)
			return
		}
	}

	s.negativeReply(w, req, rootZone, nil, match, qname, rootZone.SOA)
}

// handleBackupQuery answers a query from a backup (alias) zone.
func (s *Server) handleBackupQuery(
	w dns.ResponseWriter,
	req *dns.Msg,
	viewName, qname string,
	qtype uint16,
	match alias.Match,
	st *ServerState,
) {
	rootZone := st.RootZones[viewName][match.RootZone]
	if rootZone == nil {
		s.Logger.Error("root zone missing for backup alias; replying SERVFAIL",
			"view", viewName,
			"backup", match.MatchedZone,
			"root", match.RootZone,
		)
		replyRcode(w, req, dns.RcodeServerFailure)
		return
	}

	backupZone := st.BackupZones[viewName][match.MatchedZone] // may be nil

	// Precompute the backup SOA once; used for both the apex short-circuit and
	// the authority section of negative replies.
	backupSOA := alias.BackupSOA(rootZone.SOA, rootZone.Origin, match.MatchedZone)

	// SOA at backup apex.
	if qtype == dns.TypeSOA && qname == match.MatchedZone {
		replyWithAnswer(w, req, []dns.RR{backupSOA})
		return
	}

	records := alias.Resolve(qname, qtype, match.MatchedZone, backupZone, rootZone)
	if len(records) > 0 {
		replyWithAnswer(w, req, records)
		return
	}

	s.negativeReply(w, req, rootZone, backupZone, match, qname, backupSOA)
}

// negativeReply sends an NXDOMAIN or NODATA response with the zone SOA in the
// authority section.
//
// NXDOMAIN: the name does not exist in the zone at all.
// NODATA:   the name exists but has no records of the requested type → RCODE=NOERROR.
//
// soaRR is the pre-computed authority SOA (root or backup). May be nil.
// The authority SOA TTL is capped to min(SOA.Hdr.Ttl, SOA.Minttl) per RFC 2308.
func (s *Server) negativeReply(
	w dns.ResponseWriter,
	req *dns.Msg,
	rootZone *zone.Zone,
	backupZone *zone.Zone,
	match alias.Match,
	qname string,
	soaRR *dns.SOA,
) {
	// Determine NXDOMAIN vs NODATA.
	rcode := dns.RcodeNameError // NXDOMAIN by default

	if match.IsBackup {
		// For backup: check whether any record exists at qname (in backup override or root).
		if backupZoneHasName(backupZone, rootZone, match, qname) {
			rcode = dns.RcodeSuccess // NODATA
		}
	} else {
		// For root: check if any record exists at qname under any type.
		if _, exists := rootZone.Records[qname]; exists {
			rcode = dns.RcodeSuccess // NODATA
		}
	}

	// Build TTL-capped SOA copy for authority section per RFC 2308.
	var authSOA *dns.SOA
	if soaRR != nil {
		authSOA = cappedSOA(soaRR)
	}

	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = false
	m.Authoritative = true
	m.Rcode = rcode

	if authSOA != nil {
		m.Ns = []dns.RR{authSOA}
	}

	_ = w.WriteMsg(m)
}

// backupZoneHasName returns true if any record exists at qname (the backup-namespace
// name) — either in the backup override zone or via the root zone after rewriting.
func backupZoneHasName(backupZone *zone.Zone, rootZone *zone.Zone, match alias.Match, qname string) bool {
	// Check the backup override zone first.
	if backupZone != nil {
		if _, exists := backupZone.Records[qname]; exists {
			return true
		}
	}

	// Check the root zone by rewriting qname to the root namespace.
	rootQName := alias.RewriteQName(qname, match.MatchedZone, match.RootZone)
	if _, exists := rootZone.Records[rootQName]; exists {
		return true
	}

	return false
}

// handleTransfer routes AXFR/IXFR requests through the ACL and then dispatches
// to the transfer subsystem. qname is the already-lowercased query name from
// ServeDNS.
func (s *Server) handleTransfer(w dns.ResponseWriter, req *dns.Msg, qname string, st *ServerState) {
	// Extract source IP for ACL check.
	srcIP, err := addrFromRemote(w)
	if err != nil {
		s.Logger.Info("zone transfer: cannot parse source IP; replying REFUSED",
			"remote", w.RemoteAddr().String(),
			"err", err,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	// ACL check: nil ACL or non-matching IP → REFUSED.
	if !st.AllowTransferACL.Allows(srcIP) {
		s.Logger.Info("zone transfer: source IP not in allow-transfer ACL; replying REFUSED",
			"src", srcIP.String(),
			"qname", qname,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	// Determine which view this client falls into, then look up the zone.
	viewName := st.Matcher.Resolve(srcIP)
	if viewName == "" {
		s.Logger.Info("zone transfer: no view matched client; replying REFUSED",
			"src", srcIP.String(),
			"qname", qname,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	origins := st.ZoneOrigins[viewName]
	match := alias.Detect(qname, origins, st.Aliases)
	if match.MatchedZone == "" {
		s.Logger.Info("zone transfer: qname outside all loaded zones; replying REFUSED",
			"view", viewName,
			"qname", qname,
		)
		replyRcode(w, req, dns.RcodeRefused)
		return
	}

	if match.IsBackup {
		rootZone := st.RootZones[viewName][match.RootZone]
		backupZone := st.BackupZones[viewName][match.MatchedZone] // may be nil
		transfer.HandleAliasAXFR(w, req, match.MatchedZone, rootZone, backupZone)
	} else {
		rootZone := st.RootZones[viewName][match.MatchedZone]
		transfer.HandleAXFR(w, req, rootZone)
	}
}

// cappedSOA returns a copy of soa with Hdr.Ttl set to min(Hdr.Ttl, Minttl)
// per RFC 2308 §3.
func cappedSOA(soa *dns.SOA) *dns.SOA {
	cp := *soa // shallow copy is fine; no pointer fields need deep copy for SOA
	if cp.Hdr.Ttl > cp.Minttl {
		cp.Hdr.Ttl = cp.Minttl
	}
	return &cp
}

// addrFromRemote extracts the client netip.Addr from the ResponseWriter.
func addrFromRemote(w dns.ResponseWriter) (netip.Addr, error) {
	addr := w.RemoteAddr()
	if addr == nil {
		return netip.Addr{}, fmt.Errorf("nil remote addr")
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parsing remote addr %q: %w", addr.String(), err)
	}
	ip, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		return netip.Addr{}, fmt.Errorf("parsing IP %q: %w", host, parseErr)
	}
	return ip, nil
}

// replyRcode sends a response with the given RCODE and no payload.
// RA is always false; AA is false for error responses.
func replyRcode(w dns.ResponseWriter, req *dns.Msg, rcode int) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = false
	m.Authoritative = false
	m.Rcode = rcode
	_ = w.WriteMsg(m)
}

// replyWithAnswer sends a successful authoritative response.
// AA=1, RA=0 per RFC 1034. Authority and additional sections are omitted
// (minimal responses). TC=1 and payload truncated to effective UDP size when
// the transport is UDP.
func replyWithAnswer(w dns.ResponseWriter, req *dns.Msg, answer []dns.RR) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = false
	m.Authoritative = true
	m.Rcode = dns.RcodeSuccess
	m.Answer = answer

	// Apply UDP truncation per RFC 1035 §4.2.1 / RFC 7766.
	// If the client sent an EDNS0 OPT record, honour its advertised buffer size;
	// otherwise the legacy limit is 512 bytes (dns.MinMsgSize).
	if dnsutil.IsUDP(w) {
		maxSize := udpMaxSize(req)
		m.Truncate(maxSize)
	}

	_ = w.WriteMsg(m)
}

// udpMaxSize returns the maximum UDP payload size for the response.
// If the request carries an EDNS0 OPT record, that record's Udp field is used;
// otherwise the minimum (512 bytes) is returned.
func udpMaxSize(req *dns.Msg) int {
	if opt := req.IsEdns0(); opt != nil && opt.UDPSize() > dns.MinMsgSize {
		return int(opt.UDPSize())
	}
	return dns.MinMsgSize
}

// addrFamily returns "ipv4" or "ipv6" for the given address.
// Returns "unknown" for zero-value (early-exit paths before IP is parsed).
func addrFamily(ip netip.Addr) string {
	if !ip.IsValid() {
		return "unknown"
	}
	if ip.Is4() || ip.Is4In6() {
		return "ipv4"
	}
	return "ipv6"
}
