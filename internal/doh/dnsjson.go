package doh

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// dnsJSONMediaType is the Google Public DNS / CloudFlare de-facto JSON media
// type negotiated via the Accept header on GET requests.
const dnsJSONMediaType = "application/dns-json"

// jsonQuestion is one entry of the Google Public DNS schema Question array.
type jsonQuestion struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
}

// jsonAnswer is one entry of the Google Public DNS schema Answer array. Data
// is the RDATA in presentation format with the record header stripped.
type jsonAnswer struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
	TTL  uint32 `json:"TTL"`
	Data string `json:"data"`
}

// jsonResponse is the Google Public DNS-compatible response object. Field
// ordering and whitespace are not normative; only names, types, and values
// are. EDNSClientSubnet is omitted unless the response carries a
// server-populated ECS option.
type jsonResponse struct {
	Status           int            `json:"Status"`
	TC               bool           `json:"TC"`
	RD               bool           `json:"RD"`
	RA               bool           `json:"RA"`
	AD               bool           `json:"AD"`
	CD               bool           `json:"CD"`
	Question         []jsonQuestion `json:"Question"`
	Answer           []jsonAnswer   `json:"Answer"`
	EDNSClientSubnet string         `json:"edns_client_subnet,omitempty"`
}

// acceptsDNSJSON reports whether the Accept header lists the
// application/dns-json media type, ignoring q-values and other parameters.
// Only an explicit listing matches; wildcards (*/*, application/*) do not, so
// a generic client default never silently switches the response format.
func acceptsDNSJSON(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(part)
		if i := strings.IndexByte(mt, ';'); i >= 0 {
			mt = strings.TrimSpace(mt[:i])
		}
		if strings.EqualFold(mt, dnsJSONMediaType) {
			return true
		}
	}
	return false
}

// serveJSON handles an application/dns-json GET request: it parses the name,
// type, and edns_client_subnet parameters into a single-question query,
// dispatches it through the shared authoritative path (with the same
// zone-transfer refusal and empty-capture guard as the wire path), and
// serializes the response as the Google Public DNS schema. Request-level
// errors return 400, an empty capture returns 500, and every dispatched
// DNS-level outcome (REFUSED/NXDOMAIN/empty) returns 200 with the RCODE in
// Status. q is the already-parsed query string (handleGet parses it once to
// route on the dns parameter, then passes it in to avoid a second parse).
func (s *Server) serveJSON(w http.ResponseWriter, r *http.Request, q url.Values) {
	name := q.Get("name")
	if name == "" {
		http.Error(w, "missing name query parameter", http.StatusBadRequest)
		return
	}
	// dns.Fqdn only appends a trailing dot; the on-wire letter case is
	// preserved so the JSON Question name and any owner echo match wire DoH.
	fqdn := dns.Fqdn(name)
	// A label over 63 octets or a name over 255 octets is malformed client
	// input: the wire path's Unpack would reject it, so validate here and
	// return 400 rather than letting the over-long name fail downstream and
	// surface as a 500.
	if _, ok := dns.IsDomainName(fqdn); !ok {
		http.Error(w, "invalid name query parameter", http.StatusBadRequest)
		return
	}
	// Canonicalize the name to the exact on-wire presentation form the
	// wire-format path produces. dns.IsDomainName is a structural validator
	// that accepts raw control bytes (e.g. a newline in the URL name), and
	// dns.Fqdn does not escape; without this step those bytes would reach
	// Question[0].Name verbatim and, via the query log, let an unauthenticated
	// client forge log lines (the wire path is safe only because
	// UnpackDomainName escapes control bytes to \DDD). A wire round-trip
	// applies that same escaping, so control bytes and master-file special
	// characters are rendered identically to a wire-unpacked query and no raw
	// control byte survives downstream. The buffer is stack-allocated (a wire
	// name is at most 255 octets).
	var wire [256]byte
	off, err := dns.PackDomainName(fqdn, wire[:], 0, nil, false)
	if err != nil {
		// Unreachable for an IsDomainName-valid name (<=255 octets); guarded
		// defensively so a name that cannot be encoded on the wire is rejected
		// rather than dispatched or surfaced as a 500.
		http.Error(w, "invalid name query parameter", http.StatusBadRequest)
		return
	}
	canonical, _, err := dns.UnpackDomainName(wire[:off], 0)
	if err != nil {
		http.Error(w, "invalid name query parameter", http.StatusBadRequest)
		return
	}
	fqdn = canonical
	qtype, ok := parseQType(q.Get("type"))
	if !ok {
		http.Error(w, "invalid type query parameter", http.StatusBadRequest)
		return
	}

	req := new(dns.Msg)
	req.SetQuestion(fqdn, qtype)
	req.RecursionDesired = true
	// The cd parameter is tolerated but ignored: req.CheckingDisabled stays
	// false so the response CD bit is false. do and ct are likewise ignored.

	if ecsParam := q.Get("edns_client_subnet"); ecsParam != "" {
		ecsOpt, ok := dnsutil.ParseECSParam(ecsParam)
		if !ok {
			http.Error(w, "invalid edns_client_subnet query parameter", http.StatusBadRequest)
			return
		}
		// Attach the ECS option to a fresh OPT record so the shared query path
		// sees it exactly as it would on a wire query carrying ECS. The
		// advertised UDP size is immaterial for this synthetic in-process query
		// (the DoH writer never truncates), so the library default is fine.
		req.SetEdns0(dns.DefaultMsgSize, false)
		opt := req.IsEdns0()
		opt.Option = append(opt.Option, ecsOpt)
	}

	rw := newJSONResponseWriter(remoteTCPAddr(r), localTCPAddr(r))
	s.dispatch(rw, req)

	if rw.msg == nil {
		// The query path always writes a response via WriteMsg, which sets msg.
		// A nil capture means an internal failure, not a client error. (This
		// guards on msg rather than the wire path's packed because the JSON
		// writer skips packing and serializes from msg.)
		s.logger.Warn("doh: json query path produced no response")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", dnsJSONMediaType)
	setCacheControl(h, rw.msg)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildJSONResponse(rw.msg))
}

// parseQType resolves the type parameter: empty defaults to A, a mnemonic is
// matched case-insensitively, and a numeric code is parsed in the 0–65535
// range (ParseUint with a 16-bit width rejects out-of-range values instead of
// silently truncating). It reports ok=false for an unrecognized value.
func parseQType(v string) (uint16, bool) {
	if v == "" {
		return dns.TypeA, true
	}
	if t, ok := dns.StringToType[strings.ToUpper(v)]; ok {
		return t, true
	}
	n, err := strconv.ParseUint(v, 10, 16)
	if err != nil {
		return 0, false
	}
	return uint16(n), true
}

// buildJSONResponse serializes a response message into the Google Public DNS
// schema. Question and Answer are always non-nil so an empty section marshals
// as [] rather than null.
func buildJSONResponse(m *dns.Msg) jsonResponse {
	resp := jsonResponse{
		Status:   m.Rcode,
		TC:       m.Truncated,
		RD:       m.RecursionDesired,
		RA:       m.RecursionAvailable,
		AD:       m.AuthenticatedData,
		CD:       m.CheckingDisabled,
		Question: make([]jsonQuestion, 0, len(m.Question)),
		Answer:   make([]jsonAnswer, 0, len(m.Answer)),
	}
	for _, qd := range m.Question {
		resp.Question = append(resp.Question, jsonQuestion{Name: qd.Name, Type: qd.Qtype})
	}
	for _, rr := range m.Answer {
		hdr := rr.Header()
		resp.Answer = append(resp.Answer, jsonAnswer{
			Name: hdr.Name,
			Type: hdr.Rrtype,
			TTL:  hdr.Ttl,
			Data: rdataString(rr),
		})
	}
	if ecs := responseECS(m); ecs != "" {
		resp.EDNSClientSubnet = ecs
	}
	return resp
}

// rdataString returns the RDATA in DNS presentation format by stripping the
// record header from the record's full presentation form. The header rendered
// by miekg/dns is always the four tab-separated fields name, TTL, class, and
// type followed by a tab, so the RDATA is everything after the 4th tab. Slicing
// at that tab renders the record once (rr.String()), avoiding a second header
// render, and keeps multi-field RDATA (SOA, MX) and quoted TXT data intact.
func rdataString(rr dns.RR) string {
	s := rr.String()
	tabs := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			tabs++
			if tabs == 4 {
				return s[i+1:]
			}
		}
	}
	return s
}

// responseECS formats the server-populated EDNS Client Subnet option as
// "<network>/<source-prefix>/<scope-prefix>", or "" when the response carries
// none. This authoritative server echoes scope == source, so the scope-prefix
// reflects the source prefix it applied rather than a narrowed geo boundary.
func responseECS(m *dns.Msg) string {
	opt := m.IsEdns0()
	if opt == nil {
		return ""
	}
	for _, o := range opt.Option {
		if e, ok := o.(*dns.EDNS0_SUBNET); ok {
			return e.Address.String() + "/" +
				strconv.Itoa(int(e.SourceNetmask)) + "/" +
				strconv.Itoa(int(e.SourceScope))
		}
	}
	return ""
}
