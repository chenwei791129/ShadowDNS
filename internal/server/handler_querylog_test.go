package server

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/querylog"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// ---------------------------------------------------------------------------
// Stubs for query-log tests
// ---------------------------------------------------------------------------

// tcpRecordingWriter is a dns.ResponseWriter stub that presents itself as a
// TCP connection (LocalAddr/RemoteAddr return *net.TCPAddr). It embeds the
// existing UDP recordingWriter and overrides only the address methods.
type tcpRecordingWriter struct {
	recordingWriter
}

func (w *tcpRecordingWriter) LocalAddr() net.Addr {
	// Use 198.51.100.1 (RFC 5737 documentation range) as the local address.
	return &net.TCPAddr{IP: net.ParseIP("198.51.100.1").To4(), Port: 53}
}

func (w *tcpRecordingWriter) RemoteAddr() net.Addr {
	// Use 192.0.2.10 (RFC 5737 documentation range) as the client address.
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.10").To4(), Port: 12345}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newQueryLogBuf constructs a querylog.Logger backed by an in-memory buffer
// using minimal print options (no timestamp, no category, no severity) so
// that assertions focus on the structural content: view name and qname.
func newQueryLogBuf(buf *bytes.Buffer) *querylog.Logger {
	return querylog.NewWithWriter(buf, querylog.Config{
		PrintTime:     "no",
		PrintCategory: false,
		PrintSeverity: false,
	})
}

// countLines returns the number of lines in buf. The formatter terminates
// every line with '\n', so counting newlines is exact and would also expose
// accidental blank lines.
func countLines(buf *bytes.Buffer) int {
	return strings.Count(buf.String(), "\n")
}

// buildServeDNSRequest builds a standard DNS query message for use with
// ServeDNS.  class defaults to IN when 0 is passed.
func buildServeDNSRequest(qname string, qtype uint16, qclass uint16, opcode int, questions int) *dns.Msg {
	m := new(dns.Msg)
	m.Id = dns.Id()
	m.Opcode = opcode
	if questions == 1 {
		q := dns.Question{Name: qname, Qtype: qtype, Qclass: qclass}
		if q.Qclass == 0 {
			q.Qclass = dns.ClassINET
		}
		m.Question = []dns.Question{q}
	}
	// questions == 0 leaves Question nil (FORMERR path)
	return m
}

// newQueryLogServer constructs a Server with a single "default" view (any
// matcher) containing one root zone (example.com.) and attaches the given
// querylog Logger and optional allow-transfer ACL.
func newQueryLogServer(ql *querylog.Logger, acl *transfer.ACL) *Server {
	rootZ := buildRootZone("example.com.",
		makeARecord("www.example.com.", "192.0.2.1", 300),
	)
	srv := NewServer(ServerState{
		Matcher:          makeAnyMatcher("default"),
		ZoneOrigins:      map[string][]string{"default": {"example.com."}},
		RootZones:        map[string]map[string]*zone.Zone{"default": {"example.com.": rootZ}},
		BackupZones:      map[string]map[string]*zone.Zone{},
		Aliases:          config.AliasMap{},
		AllowTransferACL: acl,
	}, nil)
	srv.QueryLog.Store(ql)
	return srv
}

// ---------------------------------------------------------------------------
// TestQueryLog_EmissionPoints (Task 3.1 + Task 3.3)
// ---------------------------------------------------------------------------

// TestQueryLog_EmissionPoints verifies that ServeDNS emits exactly the right
// number of query log lines for each path through the handler:
//
//	(a) in-view query for a name present in a zone          → 1 line
//	(b) in-view query for a name OUTSIDE all zones (REFUSED) → 1 line
//	(c) no-view client (no view match)                       → 0 lines
//	(d) CHAOS class query                                    → 0 lines
//	(e) FORMERR (zero questions)                             → 0 lines
//	(f) NOTIMP (unsupported opcode)                          → 0 lines
func TestQueryLog_EmissionPoints(t *testing.T) {
	// recordingWriter (UDP) is defined in handler_test.go; RemoteAddr returns
	// 127.0.0.2 which falls into the "default" (any) view.
	udpW := func() *recordingWriter { return &recordingWriter{} }

	// emptyMatcher has no views, so Resolve always returns "" — simulates the
	// no-view path where the client IP does not match any configured view.
	emptyMatcher := &view.Matcher{}

	subtests := []struct {
		name       string
		makeMsg    func() *dns.Msg
		makeW      func() dns.ResponseWriter
		useMatcher bool // when true, substitute noViewMatcher
		wantLines  int
		wantView   string // non-empty: assert "view <name>:" in the line
		wantQname  string // non-empty: assert "<qname>" in the line
	}{
		{
			name:      "(a) in-view, in-zone query",
			makeMsg:   func() *dns.Msg { return buildServeDNSRequest("www.example.com.", dns.TypeA, 0, dns.OpcodeQuery, 1) },
			makeW:     func() dns.ResponseWriter { return udpW() },
			wantLines: 1,
			wantView:  "default",
			wantQname: "www.example.com",
		},
		{
			name:      "(b) in-view, out-of-zone query (REFUSED)",
			makeMsg:   func() *dns.Msg { return buildServeDNSRequest("outside.example.net.", dns.TypeA, 0, dns.OpcodeQuery, 1) },
			makeW:     func() dns.ResponseWriter { return udpW() },
			wantLines: 1,
			wantView:  "default",
			wantQname: "outside.example.net",
		},
		{
			name:       "(c) no-view client",
			makeMsg:    func() *dns.Msg { return buildServeDNSRequest("www.example.com.", dns.TypeA, 0, dns.OpcodeQuery, 1) },
			makeW:      func() dns.ResponseWriter { return udpW() },
			useMatcher: true,
			wantLines:  0,
		},
		{
			name: "(d) CHAOS class",
			makeMsg: func() *dns.Msg {
				return buildServeDNSRequest("version.bind.", dns.TypeTXT, dns.ClassCHAOS, dns.OpcodeQuery, 1)
			},
			makeW:     func() dns.ResponseWriter { return udpW() },
			wantLines: 0,
		},
		{
			name:      "(e) FORMERR — zero questions",
			makeMsg:   func() *dns.Msg { return buildServeDNSRequest("", 0, 0, dns.OpcodeQuery, 0) },
			makeW:     func() dns.ResponseWriter { return udpW() },
			wantLines: 0,
		},
		{
			name:      "(f) NOTIMP — unsupported opcode",
			makeMsg:   func() *dns.Msg { return buildServeDNSRequest("www.example.com.", dns.TypeA, 0, dns.OpcodeUpdate, 1) },
			makeW:     func() dns.ResponseWriter { return udpW() },
			wantLines: 0,
		},
	}

	for _, tc := range subtests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			ql := newQueryLogBuf(&buf)

			var srv *Server
			if tc.useMatcher {
				// Build a server whose Matcher never matches any client.
				rootZ := buildRootZone("example.com.",
					makeARecord("www.example.com.", "192.0.2.1", 300),
				)
				srv = NewServer(ServerState{
					Matcher:     emptyMatcher,
					ZoneOrigins: map[string][]string{"default": {"example.com."}},
					RootZones:   map[string]map[string]*zone.Zone{"default": {"example.com.": rootZ}},
					BackupZones: map[string]map[string]*zone.Zone{},
					Aliases:     config.AliasMap{},
				}, nil)
			} else {
				srv = newQueryLogServer(ql, nil)
			}
			srv.QueryLog.Store(ql)

			w := tc.makeW()
			srv.ServeDNS(w, tc.makeMsg())

			got := countLines(&buf)
			if got != tc.wantLines {
				t.Errorf("line count: got %d, want %d\nlog output: %q", got, tc.wantLines, buf.String())
			}
			if tc.wantView != "" && got > 0 {
				if !strings.Contains(buf.String(), "view "+tc.wantView+":") {
					t.Errorf("expected %q in log line, got: %q", "view "+tc.wantView+":", buf.String())
				}
			}
			if tc.wantQname != "" && got > 0 {
				if !strings.Contains(buf.String(), tc.wantQname) {
					t.Errorf("expected qname %q in log line, got: %q", tc.wantQname, buf.String())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestQueryLog_Transfer (Task 3.2)
// ---------------------------------------------------------------------------

// TestQueryLog_Transfer verifies that handleTransfer emits a query log line
// only when the view resolves successfully (ACL-permitted request), and that
// the logged line contains the correct query type (AXFR) and TCP flag.
//
//	(a) ACL-allowed AXFR over TCP → 1 line containing "AXFR" and flags "T"
//	(b) ACL-refused AXFR           → 0 lines
func TestQueryLog_Transfer(t *testing.T) {
	// Build a root zone that will be the transfer target.
	const zoneOrigin = "example.com."
	rootZ := buildRootZone(zoneOrigin, makeARecord("www.example.com.", "192.0.2.1", 300))

	// ACL that allows 192.0.2.10 (the tcpRecordingWriter's remote addr).
	allowACL, err := transfer.NewACL([]string{"192.0.2.10/32"})
	if err != nil {
		t.Fatalf("ParseACL: %v", err)
	}

	// ACL that denies everything.
	denyACL, err := transfer.NewACL([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseACL deny: %v", err)
	}

	makeAXFRReq := func() *dns.Msg {
		m := new(dns.Msg)
		m.Id = dns.Id()
		m.SetQuestion(zoneOrigin, dns.TypeAXFR)
		return m
	}

	t.Run("(a) ACL-allowed AXFR over TCP", func(t *testing.T) {
		var buf bytes.Buffer
		ql := newQueryLogBuf(&buf)

		srv := NewServer(ServerState{
			Matcher:          makeAnyMatcher("default"),
			ZoneOrigins:      map[string][]string{"default": {zoneOrigin}},
			RootZones:        map[string]map[string]*zone.Zone{"default": {zoneOrigin: rootZ}},
			BackupZones:      map[string]map[string]*zone.Zone{},
			Aliases:          config.AliasMap{},
			AllowTransferACL: allowACL,
		}, nil)
		srv.QueryLog.Store(ql)

		w := &tcpRecordingWriter{}
		srv.ServeDNS(w, makeAXFRReq())

		lines := buf.String()
		got := countLines(&buf)
		if got != 1 {
			t.Errorf("line count: got %d, want 1\nlog output: %q", got, lines)
		}

		// Assert query type is AXFR.
		if !strings.Contains(lines, "AXFR") {
			t.Errorf("expected AXFR in log line, got: %q", lines)
		}

		// Assert TCP flag "T" appears in the flags segment.
		// The flags segment follows the qtype and precedes the local addr.
		// A minimal assertion: the line contains "T" as part of the flags field.
		// We check for the pattern " <flags> " where flags starts with +/- and contains T.
		if !strings.Contains(lines, "T") {
			t.Errorf("expected TCP flag 'T' in log line, got: %q", lines)
		}

		// Assert view name.
		if !strings.Contains(lines, "view default:") {
			t.Errorf("expected 'view default:' in log line, got: %q", lines)
		}

		// Assert qname.
		if !strings.Contains(lines, "example.com") {
			t.Errorf("expected qname 'example.com' in log line, got: %q", lines)
		}
	})

	t.Run("(b) ACL-refused AXFR — no log line", func(t *testing.T) {
		var buf bytes.Buffer
		ql := newQueryLogBuf(&buf)

		srv := NewServer(ServerState{
			Matcher:          makeAnyMatcher("default"),
			ZoneOrigins:      map[string][]string{"default": {zoneOrigin}},
			RootZones:        map[string]map[string]*zone.Zone{"default": {zoneOrigin: rootZ}},
			BackupZones:      map[string]map[string]*zone.Zone{},
			Aliases:          config.AliasMap{},
			AllowTransferACL: denyACL,
		}, nil)
		srv.QueryLog.Store(ql)

		w := &tcpRecordingWriter{}
		srv.ServeDNS(w, makeAXFRReq())

		if got := countLines(&buf); got != 0 {
			t.Errorf("ACL-refused AXFR: got %d log lines, want 0\nlog output: %q", got, buf.String())
		}
	})
}
