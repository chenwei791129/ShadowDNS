package server

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/alias"
	"github.com/chenwei791129/ShadowDNS/internal/cookie"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/querylog"
	"github.com/chenwei791129/ShadowDNS/internal/ratelimit"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// queryOpt is the result of the single per-query EDNS0 OPT parse. It feeds
// response assembly, UDP payload sizing, and query-log extraction so the
// hot path never iterates opt.Option more than once (design: single-parse).
type queryOpt struct {
	present bool
	version uint8
	udpSize uint16
	do      bool
	// opt points at the request's OPT record; attachOPT reuses it for the
	// response (the request is owned by this handler goroutine and is not
	// read after response assembly), avoiding a per-response allocation.
	opt *dns.OPT
	// cookie points at the first COOKIE option in the OPT record (RFC 7873
	// §5.2 first-wins); nil when absent. The Cookie field is hex-encoded by
	// miekg/dns, so its raw byte length is len(Cookie)/2.
	cookie *dns.EDNS0_COOKIE
	// respCookie is the hex-encoded full COOKIE option payload (client
	// cookie echo + fresh RFC 9018 server cookie) to emit in the response.
	// Empty when the query carried no valid COOKIE option.
	respCookie string
}

// parseQueryOpt extracts the EDNS0 OPT record fields from req in one pass.
// It only touches req.Extra (never req.Question), so it is safe to call
// before the question-count check.
func parseQueryOpt(req *dns.Msg) queryOpt {
	opt := req.IsEdns0()
	if opt == nil {
		return queryOpt{}
	}
	qo := queryOpt{
		present: true,
		version: opt.Version(),
		udpSize: opt.UDPSize(),
		do:      opt.Do(),
		opt:     opt,
	}
	for _, o := range opt.Option {
		if c, ok := o.(*dns.EDNS0_COOKIE); ok {
			qo.cookie = c
			break
		}
	}
	return qo
}

// ednsUDPSize is the UDP payload size the server advertises in every OPT
// record it sends. 1232 follows DNS Flag Day 2020 and matches the BIND 9.18
// edns-udp-size default, avoiding IPv6 fragmentation. The OPT Udp field
// carries the sender's own receive capability, so the client's advertised
// size is deliberately not echoed.
const ednsUDPSize = 1232

// attachOPT appends the response OPT record (version 0, UDP payload size
// 1232) when the query carried an OPT record, per RFC 6891. It is the single
// assembly point for response EDNS content; extended rcodes (e.g. BADVERS)
// rely on this OPT being present because miekg/dns Pack fails with
// ErrExtendedRcode for Rcode > 15 without one.
func attachOPT(m *dns.Msg, qo queryOpt) {
	if !qo.present {
		return
	}
	opt := qo.opt
	if opt == nil {
		// Cold path (panic recovery re-detects EDNS without the parse
		// result): build a fresh OPT record.
		opt = &dns.OPT{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT}}
	} else {
		// Hot path: reuse the request's OPT record for the response.
		// Dropping the client's options also discards the COOKIE option
		// it sent — the response cookie is appended below from
		// qo.respCookie. Hdr.Name is reset defensively: IsEdns0 matches
		// on Rrtype only, so a client may have sent a non-root owner.
		opt.Option = opt.Option[:0]
		opt.Hdr.Name = "."
	}
	// Hdr.Ttl = 0 clears the client's version, DO bit, and any extended-
	// rcode bits; Pack sets the response extended rcode itself via
	// SetExtendedRcode.
	opt.Hdr.Ttl = 0
	opt.SetUDPSize(ednsUDPSize)
	if qo.respCookie != "" {
		opt.Option = append(opt.Option, &dns.EDNS0_COOKIE{
			Code:   dns.EDNS0COOKIE,
			Cookie: qo.respCookie,
		})
	}
	m.Extra = append(m.Extra, opt)
}

// ServeDNS implements dns.Handler. It is the entry point for every DNS query.
func (s *Server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	var mw *metrics.ResponseWriter
	var clientIP netip.Addr
	viewLabel := "refused"

	// realW is the underlying writer used for transport detection in the
	// metrics defer; it stays the real writer even after the rate-limit and
	// metrics wrappers are installed.
	realW := w

	// Install the rate-limit wrapper just outside the real writer so it sees
	// the final response, but inside the metrics wrapper so a dropped response
	// is still observed by metrics (which count the produced response; RRL
	// actions are tracked by the limiter's own counters).
	if s.RateLimiter != nil {
		w = ratelimit.NewResponseWriter(w, s.RateLimiter)
	}

	if s.Metrics != nil {
		mw = metrics.NewResponseWriter(w, s.Metrics, "refused", time.Now())
		w = mw

		defer func() {
			proto := "tcp"
			if dnsutil.IsUDP(realW) {
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
			s.Logger.Sugar().Errorw("panic in DNS handler; recovering",
				"panic", fmt.Sprintf("%v", r),
			)
			m := new(dns.Msg)
			m.SetReply(req)
			m.RecursionAvailable = false
			m.Rcode = dns.RcodeServerFailure
			// Cold path: the queryOpt parse result is not trusted after a
			// panic, so re-detect EDNS in place (the one allowed exception
			// to the single-parse rule).
			attachOPT(m, queryOpt{present: req.IsEdns0() != nil})
			_ = w.WriteMsg(m)
		}
	}()

	// Single per-query OPT parse — must run before the opcode and
	// question-count checks so early-exit error responses can echo OPT too.
	qo := parseQueryOpt(req)

	// Unsupported opcodes → NOTIMP.
	if req.Opcode != dns.OpcodeQuery {
		s.Logger.Sugar().Infow("unsupported opcode; replying NOTIMP",
			"opcode", req.Opcode,
			"remote", w.RemoteAddr().String(),
		)
		replyRcode(w, req, qo, dns.RcodeNotImplemented)
		return
	}

	// Malformed query (wrong question count) → FORMERR.
	if len(req.Question) != 1 {
		s.Logger.Sugar().Infow("malformed query: question count != 1; replying FORMERR",
			"questions", len(req.Question),
			"remote", w.RemoteAddr().String(),
		)
		replyRcode(w, req, qo, dns.RcodeFormatError)
		return
	}

	// Unsupported EDNS version → BADVERS (RFC 6891 §6.1.3). Takes precedence
	// over all COOKIE processing; the extended rcode is carried by the OPT
	// record that replyRcode attaches (Pack would silently fail without it).
	if qo.present && qo.version > 0 {
		replyRcode(w, req, qo, dns.RcodeBadVers)
		return
	}

	// COOKIE option processing (RFC 7873 answer-only mode). The length check
	// uses the raw byte count — miekg/dns stores the option as a hex string,
	// so len(Cookie) is twice the raw length. Checking the hex length first
	// avoids decoding malformed payloads, and only the 8-byte client cookie
	// is ever decoded (the rest of a full cookie is a stale server cookie
	// that is never validated). A fresh server cookie is computed for every
	// cookied query; attachOPT emits it at whichever assembly point answers
	// the query.
	if qo.cookie != nil {
		hexCookie := qo.cookie.Cookie
		var cc [cookie.ClientCookieLen]byte
		if len(hexCookie)%2 != 0 || !cookie.ValidQueryLen(len(hexCookie)/2) {
			// Malformed COOKIE → FORMERR with OPT echo but no COOKIE
			// option (RFC 7873 §5.2.2).
			replyRcode(w, req, qo, dns.RcodeFormatError)
			return
		}
		if _, err := hex.Decode(cc[:], []byte(hexCookie[:cookie.ClientCookieLen*2])); err != nil {
			// Non-hex payload cannot come off the wire (miekg/dns produced
			// the hex encoding); treat in-process garbage as malformed too.
			replyRcode(w, req, qo, dns.RcodeFormatError)
			return
		}
		ip, ipErr := addrFromRemote(w)
		if ipErr != nil {
			// Same failure as view resolution handles below: without a
			// client IP there is nothing to serve — REFUSED, not a
			// silently cookie-less answer.
			s.Logger.Sugar().Warnw("cannot parse client IP for cookie; replying REFUSED",
				"remote", w.RemoteAddr().String(),
				"err", ipErr,
			)
			replyRcode(w, req, qo, dns.RcodeRefused)
			return
		}
		full := s.cookieGen.Generate(cc, ip, time.Now().Unix())
		qo.respCookie = hex.EncodeToString(full[:])
	}

	q := req.Question[0]
	// qname is the lookup-fold form (lowercase + trailing dot) used for all
	// zone matching, alias detection, and zone Lookup calls.
	// qnameOrig is the on-wire case from req.Question[0].Name and is the only
	// form fed into response assembly so the Question section, wildcard owner,
	// and alias-rewrite output preserve the client-supplied case (RFC 4343 +
	// DNS-0x20 echo).
	qname := dnsutil.LookupKey(q.Name)
	qnameOrig := q.Name
	qtype := q.Qtype
	qclass := q.Qclass

	// CHAOS class — refuse all to hide identity.
	if qclass == dns.ClassCHAOS {
		s.Logger.Sugar().Infow("CHAOS class query; replying REFUSED",
			"qname", qname,
			"remote", w.RemoteAddr().String(),
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
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
		s.handleTransfer(w, req, qname, qo, st)
		return
	}

	// Extract client IP for view resolution.
	var err error
	clientIP, err = addrFromRemote(w)
	if err != nil {
		s.Logger.Sugar().Warnw("cannot parse client IP; replying REFUSED",
			"remote", w.RemoteAddr().String(),
			"err", err,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	// Determine view from client IP.
	viewName := st.Matcher.Resolve(clientIP)
	if viewName == "" {
		s.Logger.Sugar().Infow("no view matched client; replying REFUSED",
			"client", clientIP.String(),
			"qname", qname,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	// View resolved — update metrics labels.
	viewLabel = viewName
	if mw != nil {
		mw.SetView(viewName)
	}

	// Emission point (Task 3.1): emit query log immediately after view
	// resolution succeeds and before zone matching. Queries that are later
	// REFUSED because qname is outside all zones are still logged here,
	// matching BIND9 semantics. The entire entry-build is guarded by a
	// single nil check so that disabled logging adds no overhead.
	if s.QueryLog != nil {
		s.QueryLog.Log(buildQueryEntry(w, req, qnameOrig, viewName, qo))
	}

	// Determine zone using longest-suffix match + alias map.
	origins := st.ZoneOrigins[viewName]
	match := alias.Detect(qname, origins, st.Aliases)
	if match.MatchedZone == "" {
		s.Logger.Sugar().Infow("query outside all loaded zones; replying REFUSED",
			"view", viewName,
			"qname", qname,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	if match.IsBackup {
		s.handleBackupQuery(w, req, viewName, qname, qnameOrig, qtype, qo, match, st)
	} else {
		s.handleRootQuery(w, req, viewName, qname, qnameOrig, qtype, qo, match, st)
	}
}

// handleRootQuery answers a query from a root (non-alias) zone.
//
// qname is the lookup-fold form used for zone matching and Lookup calls;
// qnameOrig carries the on-wire case from req.Question[0].Name and is used
// only when assembling response RRs (wildcard owner) and ephemeral TXT
// synthesis.
func (s *Server) handleRootQuery(
	w dns.ResponseWriter,
	req *dns.Msg,
	viewName, qname, qnameOrig string,
	qtype uint16,
	qo queryOpt,
	match alias.Match,
	st *ServerState,
) {
	rootZone := st.RootZones[viewName][match.MatchedZone]
	if rootZone == nil {
		s.Logger.Sugar().Errorw("root zone missing for matched origin; replying SERVFAIL",
			"view", viewName,
			"zone", match.MatchedZone,
		)
		replyRcode(w, req, qo, dns.RcodeServerFailure)
		return
	}

	// SOA at apex short-circuit.
	if qtype == dns.TypeSOA && qname == rootZone.Origin {
		if rootZone.SOA != nil {
			replyWithAnswer(w, req, qo, []dns.RR{rootZone.SOA})
		} else {
			s.negativeReply(w, req, qo, rootZone, nil, match, qname, rootZone.SOA)
		}
		return
	}

	records := rootZone.Lookup(qname, qtype)
	if len(records) > 0 {
		replyWithAnswer(w, req, qo, records)
		return
	}

	// Ephemeral TXT overlay. Runs after the exact (qname, qtype) match (so
	// zone records win) but before CNAME fallback and wildcard synthesis, so
	// a live ephemeral TXT at a CNAME'd qname overrides the CNAME for TXT
	// queries. Intentional RFC 1034 §3.6.2 deviation scoped to TXT qtype;
	// lookupEphemeralTXT is a no-op for other qtypes and an empty store.
	// See docs/ephemeral-api.md §"Ephemeral TXT 覆蓋 exact CNAME".
	if answer := s.lookupEphemeralTXT(qname, qnameOrig, qtype); answer != nil {
		replyWithAnswer(w, req, qo, answer)
		return
	}

	// CNAME fallback per RFC 1034 §3.6.2.
	if qtype != dns.TypeCNAME {
		if cnames := rootZone.Lookup(qname, dns.TypeCNAME); len(cnames) > 0 {
			replyWithAnswer(w, req, qo, rootZone.FollowCNAME(nil, cnames, qtype))
			return
		}
	}

	// Wildcard fallback per RFC 4592. Synthesized owner is qnameOrig so the
	// response preserves the client-supplied case.
	wRRs, wFound := rootZone.LookupWildcard(qname, qtype)
	if wFound && len(wRRs) > 0 {
		replyWithAnswer(w, req, qo, rewriteWildcardOwner(wRRs, qnameOrig))
		return
	}
	if qtype != dns.TypeCNAME {
		if wCNAMEs, _ := rootZone.LookupWildcard(qname, dns.TypeCNAME); len(wCNAMEs) > 0 {
			replyWithAnswer(w, req, qo, rootZone.FollowCNAME(nil, rewriteWildcardOwner(wCNAMEs, qnameOrig), qtype))
			return
		}
	}

	s.negativeReply(w, req, qo, rootZone, nil, match, qname, rootZone.SOA)
}

// handleBackupQuery answers a query from a backup (alias) zone.
//
// qname is the lookup-fold form used for zone matching and the in-bailiwick
// rewrite; qnameOrig carries the on-wire case from req.Question[0].Name and
// is fed into alias.Resolve* and the ephemeral-TXT synthesis path so the
// response owner / RDATA preserves client-supplied case (RFC 4343 + DNS-0x20
// echo). The alias.Resolve* entry points fold qnameOrig internally via
// dnsutil.LookupKey for matching.
func (s *Server) handleBackupQuery(
	w dns.ResponseWriter,
	req *dns.Msg,
	viewName, qname, qnameOrig string,
	qtype uint16,
	qo queryOpt,
	match alias.Match,
	st *ServerState,
) {
	rootZone := st.RootZones[viewName][match.RootZone]
	if rootZone == nil {
		s.Logger.Sugar().Errorw("root zone missing for backup alias; replying SERVFAIL",
			"view", viewName,
			"backup", match.MatchedZone,
			"root", match.RootZone,
		)
		replyRcode(w, req, qo, dns.RcodeServerFailure)
		return
	}

	backupZone := st.BackupZones[viewName][match.MatchedZone] // may be nil

	// Per-alias-group RDATA-rewrite flag (false when not declared).
	rewriteRDATALabels := st.AliasFlags[match.MatchedZone]

	// Operator-authored backup case (FQDN with trailing dot) for on-wire
	// rewriting. Falls back to the lookup-fold form when the map has no
	// entry — this preserves prior behaviour for installations that never
	// populate BackupOriginalCase.
	backupOriginalCase := st.BackupOriginalCase[match.MatchedZone]
	if backupOriginalCase == "" {
		backupOriginalCase = match.MatchedZone
	}

	// Precompute the backup SOA once; used for both the apex short-circuit and
	// the authority section of negative replies.
	backupSOA := alias.BackupSOA(rootZone.SOA, rootZone.Origin, backupOriginalCase)

	// SOA at backup apex.
	if qtype == dns.TypeSOA && qname == match.MatchedZone {
		replyWithAnswer(w, req, qo, []dns.RR{backupSOA})
		return
	}

	// Exact match (backup override + root exact), without CNAME fallback —
	// so zone records win over the ephemeral overlay below.
	if records := alias.ResolveExactNoCNAME(qnameOrig, qtype, match.MatchedZone, backupOriginalCase, backupZone, rootZone, rewriteRDATALabels); len(records) > 0 {
		replyWithAnswer(w, req, qo, records)
		return
	}

	// Ephemeral TXT overlay. Same layering and RFC 1034 §3.6.2 deviation as
	// handleRootQuery (scoped to TXT qtype); lookup uses the lookup-fold
	// qname because API callers PUT entries under that name.
	if answer := s.lookupEphemeralTXT(qname, qnameOrig, qtype); answer != nil {
		replyWithAnswer(w, req, qo, answer)
		return
	}

	// CNAME fallback per RFC 1034 §3.6.2.
	if records := alias.ResolveCNAMEFallback(qnameOrig, qtype, match.MatchedZone, backupOriginalCase, rootZone, rewriteRDATALabels); len(records) > 0 {
		replyWithAnswer(w, req, qo, records)
		return
	}

	if records := alias.ResolveWildcard(qnameOrig, qtype, match.MatchedZone, backupOriginalCase, rootZone, rewriteRDATALabels); len(records) > 0 {
		replyWithAnswer(w, req, qo, records)
		return
	}

	s.negativeReply(w, req, qo, rootZone, backupZone, match, qname, backupSOA)
}

// EphemeralResponseTTL is the TTL (in seconds) written into every ephemeral
// TXT RR returned to DNS clients. The API-supplied TTL controls only Store
// lifespan; DNS response TTL is a fixed short value so downstream resolver
// caches behave predictably and do not inherit minute-to-minute decrements
// from remaining Store lifetime.
const EphemeralResponseTTL uint32 = 0

// lookupEphemeralTXT returns a synthesized TXT RRSet if the ephemeral store
// holds one or more unexpired TXT entries for qname. Each stored value
// becomes its own dns.TXT record so DNS clients (and ACME validators) can
// iterate the RRSet naturally. Every RR carries TTL EphemeralResponseTTL
// regardless of the entry's remaining Store lifetime. Returns nil when qtype
// is not TXT, the store is disabled, or no live entries match.
//
// qname is the lookup-fold form used to query the Store (which is keyed by
// dnsutil.LookupKey, matching the API's path-parameter normalization).
// qnameOrig is the on-wire case from req.Question[0].Name and is written
// verbatim into the synthesized RR Hdr.Name so the response owner echoes
// the client-supplied case.
func (s *Server) lookupEphemeralTXT(qname, qnameOrig string, qtype uint16) []dns.RR {
	if qtype != dns.TypeTXT || s.EphemeralStore == nil {
		return nil
	}
	recs, ok := s.EphemeralStore.Lookup(qname)
	if !ok {
		return nil
	}
	answers := make([]dns.RR, 0, len(recs))
	for _, rec := range recs {
		answers = append(answers, &dns.TXT{
			Hdr: dns.RR_Header{
				Name:   qnameOrig,
				Rrtype: dns.TypeTXT,
				Class:  dns.ClassINET,
				Ttl:    EphemeralResponseTTL,
			},
			Txt: []string{rec.Value},
		})
	}
	return answers
}

// negativeReply sends an NXDOMAIN or NODATA response with the zone SOA in the
// authority section and the OPT echo when the query carried one.
//
// NXDOMAIN: the name does not exist in the zone at all.
// NODATA:   the name exists but has no records of the requested type → RCODE=NOERROR.
//
// soaRR is the pre-computed authority SOA (root or backup). May be nil.
// The authority SOA TTL is capped to min(SOA.Hdr.Ttl, SOA.Minttl) per RFC 2308.
func (s *Server) negativeReply(
	w dns.ResponseWriter,
	req *dns.Msg,
	qo queryOpt,
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
		} else if rootZone.HasWildcard(qname) {
			rcode = dns.RcodeSuccess // Wildcard NODATA
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

	attachOPT(m, qo)
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

	// Check wildcard match in root zone.
	if rootZone.HasWildcard(rootQName) {
		return true
	}

	return false
}

// handleTransfer routes AXFR/IXFR requests through the ACL and then dispatches
// to the transfer subsystem. qname is the already-lowercased query name from
// ServeDNS; qo is the per-query OPT parse result threaded through so
// pre-transfer error responses can echo OPT.
func (s *Server) handleTransfer(w dns.ResponseWriter, req *dns.Msg, qname string, qo queryOpt, st *ServerState) {
	// Extract source IP for ACL check.
	srcIP, err := addrFromRemote(w)
	if err != nil {
		s.Logger.Sugar().Infow("zone transfer: cannot parse source IP; replying REFUSED",
			"remote", w.RemoteAddr().String(),
			"err", err,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	// ACL check: nil ACL or non-matching IP → REFUSED.
	if !st.AllowTransferACL.Allows(srcIP) {
		s.Logger.Sugar().Infow("zone transfer: source IP not in allow-transfer ACL; replying REFUSED",
			"src", srcIP.String(),
			"qname", qname,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	// Determine which view this client falls into, then look up the zone.
	viewName := st.Matcher.Resolve(srcIP)
	if viewName == "" {
		s.Logger.Sugar().Infow("zone transfer: no view matched client; replying REFUSED",
			"src", srcIP.String(),
			"qname", qname,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	// Emission point (Task 3.2): emit query log after view resolution succeeds.
	// The ACL check runs above and returns before reaching this point, so
	// ACL-refused requests are never logged — consistent with BIND9 semantics
	// (the existing ACL-before-view ordering is preserved; we do not reorder).
	if s.QueryLog != nil {
		q := req.Question[0]
		s.QueryLog.Log(buildQueryEntry(w, req, q.Name, viewName, qo))
	}

	origins := st.ZoneOrigins[viewName]
	match := alias.Detect(qname, origins, st.Aliases)
	if match.MatchedZone == "" {
		s.Logger.Sugar().Infow("zone transfer: qname outside all loaded zones; replying REFUSED",
			"view", viewName,
			"qname", qname,
		)
		replyRcode(w, req, qo, dns.RcodeRefused)
		return
	}

	if match.IsBackup {
		rootZone := st.RootZones[viewName][match.RootZone]
		backupZone := st.BackupZones[viewName][match.MatchedZone] // may be nil
		rewriteRDATALabels := st.AliasFlags[match.MatchedZone]
		backupOriginalCase := st.BackupOriginalCase[match.MatchedZone]
		if backupOriginalCase == "" {
			backupOriginalCase = match.MatchedZone
		}
		transfer.HandleAliasAXFR(w, req, match.MatchedZone, backupOriginalCase, rootZone, backupZone, rewriteRDATALabels, s.Logger)
	} else {
		rootZone := st.RootZones[viewName][match.MatchedZone]
		transfer.HandleAXFR(w, req, rootZone, s.Logger)
	}
}

// rewriteWildcardOwner returns copies of the given RRs with the owner name
// set to qnameOrig. Used to synthesize wildcard responses per RFC 4592 §2.2.
// qnameOrig MUST be the on-wire case from req.Question[0].Name: wildcards
// never have a stored owner, so qname case is the only available source.
func rewriteWildcardOwner(rrs []dns.RR, qnameOrig string) []dns.RR {
	result := make([]dns.RR, len(rrs))
	for i, rr := range rrs {
		cp := dns.Copy(rr)
		cp.Header().Name = qnameOrig
		result[i] = cp
	}
	return result
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
//
// Type-switching on the concrete *net.UDPAddr / *net.TCPAddr returned by
// miekg/dns skips addr.String() + SplitHostPort + ParseAddr (~3 short-lived
// allocs per query). Unmap() canonicalizes 16-byte v4-mapped slices into
// 4-byte v4 form, keeping byte-equivalence with the legacy path so view
// matchers (== / Prefix.Contains) behave identically. The default arm
// preserves the legacy string path for unknown net.Addr implementations
// (test stubs, future PacketConn variants).
func addrFromRemote(w dns.ResponseWriter) (netip.Addr, error) {
	addr := w.RemoteAddr()
	if addr == nil {
		return netip.Addr{}, fmt.Errorf("nil remote addr")
	}
	fromIP := func(ip net.IP, proto string) (netip.Addr, error) {
		a, ok := netip.AddrFromSlice(ip)
		if !ok {
			return netip.Addr{}, fmt.Errorf("invalid %s IP slice length %d", proto, len(ip))
		}
		return a.Unmap(), nil
	}
	switch a := addr.(type) {
	case *net.UDPAddr:
		return fromIP(a.IP, "UDP")
	case *net.TCPAddr:
		return fromIP(a.IP, "TCP")
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return netip.Addr{}, fmt.Errorf("parsing remote addr %q: %w", addr.String(), err)
		}
		ip, parseErr := netip.ParseAddr(host)
		if parseErr != nil {
			return netip.Addr{}, fmt.Errorf("parsing IP %q: %w", host, parseErr)
		}
		// Unmap for byte-equivalence with the typed arms above — the cookie
		// hash and view matching both depend on a canonical address form.
		return ip.Unmap(), nil
	}
}

// replyRcode sends a response with the given RCODE and no payload beyond the
// OPT echo. RA is always false; AA is false for error responses.
func replyRcode(w dns.ResponseWriter, req *dns.Msg, qo queryOpt, rcode int) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = false
	m.Authoritative = false
	m.Rcode = rcode
	attachOPT(m, qo)
	_ = w.WriteMsg(m)
}

// replyWithAnswer sends a successful authoritative response with AA=1, RA=0,
// the OPT echo when the query carried one, and a minimal Authority section.
// Name compression (RFC 1035 §4.1.4) is always enabled. On UDP the wire size
// is strictly bounded by udpMaxSize(qo) via truncateForUDP, which drops
// trailing Answer RRs and sets TC=1 when the response would overflow; the
// OPT record is attached before truncation so it counts toward the budget
// and is never dropped (RFC 6891).
func replyWithAnswer(w dns.ResponseWriter, req *dns.Msg, qo queryOpt, answer []dns.RR) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = false
	m.Authoritative = true
	m.Rcode = dns.RcodeSuccess
	m.Answer = answer
	// Must be set before any Pack() so truncateForUDP measures compressed size.
	m.Compress = true
	attachOPT(m, qo)

	if dnsutil.IsUDP(w) {
		truncateForUDP(m, udpMaxSize(qo))
	}

	_ = w.WriteMsg(m)
}

// truncateForUDP mutates m so it Packs to no more than budget bytes. While
// the packed size exceeds budget it drops the trailing Answer RR, sets TC=1,
// and re-packs. If Answer empties before the packed size fits (oversized
// question/authority), TC=1 is set on the header-only remainder. The caller
// owns m.Compress; this function honours whatever setting m carries. A Pack
// failure leaves m unchanged and returns; the subsequent WriteMsg call will
// surface the same error through the normal path.
// See RFC 6891 §6.2.5 for the requestor-advertised UDP payload size contract.
func truncateForUDP(m *dns.Msg, budget int) {
	for {
		packed, err := m.Pack()
		if err != nil {
			return
		}
		if len(packed) <= budget {
			return
		}
		m.Truncated = true
		if len(m.Answer) == 0 {
			return
		}
		m.Answer = m.Answer[:len(m.Answer)-1]
	}
}

// udpMaxSize returns the maximum UDP payload size for the response.
// If the request carried an EDNS0 OPT record, its advertised size is used;
// otherwise the minimum (512 bytes) is returned.
func udpMaxSize(qo queryOpt) int {
	if qo.present && qo.udpSize > dns.MinMsgSize {
		return int(qo.udpSize)
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

// addrPortFromNetAddr extracts a canonical (unmapped) AddrPort from the
// concrete *net.TCPAddr / *net.UDPAddr types returned by miekg/dns without
// any per-query heap allocation, mirroring the rationale of addrFromRemote.
// Unmap() canonicalizes v4-mapped IPv6 slices to plain IPv4, matching BIND9
// output (which prints plain IPv4). isTCP reports the transport implied by
// the concrete type; ok is false for unknown net.Addr implementations.
func addrPortFromNetAddr(addr net.Addr) (ap netip.AddrPort, isTCP, ok bool) {
	switch a := addr.(type) {
	case *net.TCPAddr:
		ip, ok := netip.AddrFromSlice(a.IP)
		return netip.AddrPortFrom(ip.Unmap(), uint16(a.Port)), true, ok
	case *net.UDPAddr:
		ip, ok := netip.AddrFromSlice(a.IP)
		return netip.AddrPortFrom(ip.Unmap(), uint16(a.Port)), false, ok
	}
	return netip.AddrPort{}, false, false
}

// buildQueryEntry constructs a querylog.Entry from the current request, the
// resolved view name, and the per-query OPT parse result. Called only when
// s.QueryLog != nil (guarded by caller).
func buildQueryEntry(w dns.ResponseWriter, req *dns.Msg, qnameOrig, viewName string, qo queryOpt) querylog.Entry {
	// Transport is decided solely by the remote address type.
	clientAddr, isTCP, ok := addrPortFromNetAddr(w.RemoteAddr())
	if !ok {
		// Fallback for unknown net.Addr implementations (test stubs, future
		// PacketConn variants); may allocate, port unavailable.
		if ip, err := addrFromRemote(w); err == nil {
			clientAddr = netip.AddrPortFrom(ip, 0)
		}
	}

	// Local address: only the IP is logged (BIND9 prints no port).
	var localAddr netip.Addr
	if lap, _, ok := addrPortFromNetAddr(w.LocalAddr()); ok {
		localAddr = lap.Addr()
	}

	q := req.Question[0]
	return querylog.Entry{
		ClientAddr:    clientAddr,
		Qname:         qnameOrig,
		Qclass:        q.Qclass,
		Qtype:         q.Qtype,
		ViewName:      viewName,
		RD:            req.RecursionDesired,
		DO:            qo.do,
		CD:            req.CheckingDisabled,
		TCP:           isTCP,
		EDNSPresent:   qo.present,
		EDNSVersion:   qo.version,
		CookiePresent: qo.cookie != nil,
		LocalAddr:     localAddr,
	}
}
