package querylog_test

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/querylog"
)

// specTime returns the fixed time that renders as "07-Jun-2026 05:59:41.389"
// under local-time layouts. We use UTC+0 so local == UTC and the digits are
// unambiguous.
func specTime() time.Time {
	loc := time.FixedZone("test", 0)
	return time.Date(2026, time.June, 7, 5, 59, 41, 389_000_000, loc)
}

// baseEntry returns the Entry matching the spec's full-line example.
func baseEntry() querylog.Entry {
	return querylog.Entry{
		ClientAddr:    netip.MustParseAddrPort("192.0.2.10:16361"),
		Qname:         "www.example.com.", // on-wire with trailing dot
		Qclass:        dns.ClassINET,
		Qtype:         dns.TypeA,
		ViewName:      "view-eu",
		RD:            false,
		DO:            true,
		CD:            true,
		TCP:           false,
		EDNSPresent:   true,
		EDNSVersion:   0,
		CookiePresent: false,
		LocalAddr:     netip.MustParseAddr("198.51.100.7"),
	}
}

// TestFormat_FullLine verifies the spec's byte-exact full-line example.
//
// Expected line (from spec):
//
//	07-Jun-2026 05:59:41.389 queries: info: client @0x2a 192.0.2.10#16361 (www.example.com): view view-eu: query: www.example.com IN A -E(0)DC (198.51.100.7)
func TestFormat_FullLine(t *testing.T) {
	var buf bytes.Buffer
	l := querylog.NewWithWriter(&buf, querylog.Config{
		PrintTime:     "yes",
		PrintCategory: true,
		PrintSeverity: true,
	})

	want := "07-Jun-2026 05:59:41.389 queries: info: client @0x2a 192.0.2.10#16361 (www.example.com): view view-eu: query: www.example.com IN A -E(0)DC (198.51.100.7)\n"

	l.LogAt(baseEntry(), specTime(), 0x2a)

	got := buf.String()
	if got != want {
		t.Errorf("full line mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestFormat_PrintOptions covers the four-combination table from the spec plus
// the iso8601 and iso8601-utc variants.
func TestFormat_PrintOptions(t *testing.T) {
	entry := baseEntry()
	ts := specTime()

	cases := []struct {
		name      string
		cfg       querylog.Config
		wantStart string
	}{
		{
			name:      "yes/yes/yes",
			cfg:       querylog.Config{PrintTime: "yes", PrintCategory: true, PrintSeverity: true},
			wantStart: "07-Jun-2026 05:59:41.389 queries: info: client @0x2a",
		},
		{
			name:      "no/yes/yes",
			cfg:       querylog.Config{PrintTime: "no", PrintCategory: true, PrintSeverity: true},
			wantStart: "queries: info: client @0x2a",
		},
		{
			name:      "yes/no/no",
			cfg:       querylog.Config{PrintTime: "yes", PrintCategory: false, PrintSeverity: false},
			wantStart: "07-Jun-2026 05:59:41.389 client @0x2a",
		},
		{
			name:      "no/no/no",
			cfg:       querylog.Config{PrintTime: "no", PrintCategory: false, PrintSeverity: false},
			wantStart: "client @0x2a",
		},
		{
			// iso8601 renders local time in ISO 8601 layout; with UTC+0 zone digits are same.
			name:      "iso8601/yes/yes",
			cfg:       querylog.Config{PrintTime: "iso8601", PrintCategory: true, PrintSeverity: true},
			wantStart: "2026-06-07T05:59:41.389 queries: info: client @0x2a",
		},
		{
			// iso8601-utc renders UTC time; UTC+0 zone so digits are identical.
			name:      "iso8601-utc/yes/yes",
			cfg:       querylog.Config{PrintTime: "iso8601-utc", PrintCategory: true, PrintSeverity: true},
			wantStart: "2026-06-07T05:59:41.389 queries: info: client @0x2a",
		},
		{
			// "local" is the same as "yes" (local time, dd-Mmm-yyyy layout).
			name:      "local/yes/yes",
			cfg:       querylog.Config{PrintTime: "local", PrintCategory: true, PrintSeverity: true},
			wantStart: "07-Jun-2026 05:59:41.389 queries: info: client @0x2a",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := querylog.NewWithWriter(&buf, tc.cfg)
			l.LogAt(entry, ts, 0x2a)
			got := buf.String()
			if !strings.HasPrefix(got, tc.wantStart) {
				t.Errorf("prefix mismatch:\ngot:  %q\nwant prefix: %q", got, tc.wantStart)
			}
		})
	}
}

// TestFormat_Flags covers the spec's flag field values table exactly.
func TestFormat_Flags(t *testing.T) {
	cases := []struct {
		name      string
		entry     querylog.Entry
		wantFlags string
	}{
		{
			name: "RD=0 EDNS v0 UDP DO=1 CD=1",
			entry: querylog.Entry{
				ClientAddr:    netip.MustParseAddrPort("192.0.2.1:1234"),
				Qname:         "example.com.",
				Qclass:        dns.ClassINET,
				Qtype:         dns.TypeA,
				ViewName:      "v",
				RD:            false,
				DO:            true,
				CD:            true,
				TCP:           false,
				EDNSPresent:   true,
				EDNSVersion:   0,
				CookiePresent: false,
				LocalAddr:     netip.MustParseAddr("198.51.100.1"),
			},
			wantFlags: "-E(0)DC",
		},
		{
			name: "RD=0 EDNS v0 UDP DO=1 CD=1 COOKIE",
			entry: querylog.Entry{
				ClientAddr:    netip.MustParseAddrPort("192.0.2.1:1234"),
				Qname:         "example.com.",
				Qclass:        dns.ClassINET,
				Qtype:         dns.TypeA,
				ViewName:      "v",
				RD:            false,
				DO:            true,
				CD:            true,
				TCP:           false,
				EDNSPresent:   true,
				EDNSVersion:   0,
				CookiePresent: true,
				LocalAddr:     netip.MustParseAddr("198.51.100.1"),
			},
			wantFlags: "-E(0)DCK",
		},
		{
			name: "RD=1 no EDNS TCP",
			entry: querylog.Entry{
				ClientAddr:    netip.MustParseAddrPort("192.0.2.1:1234"),
				Qname:         "example.com.",
				Qclass:        dns.ClassINET,
				Qtype:         dns.TypeA,
				ViewName:      "v",
				RD:            true,
				DO:            false,
				CD:            false,
				TCP:           true,
				EDNSPresent:   false,
				CookiePresent: false,
				LocalAddr:     netip.MustParseAddr("198.51.100.1"),
			},
			wantFlags: "+T",
		},
		{
			name: "RD=0 no EDNS UDP",
			entry: querylog.Entry{
				ClientAddr:    netip.MustParseAddrPort("192.0.2.1:1234"),
				Qname:         "example.com.",
				Qclass:        dns.ClassINET,
				Qtype:         dns.TypeA,
				ViewName:      "v",
				RD:            false,
				DO:            false,
				CD:            false,
				TCP:           false,
				EDNSPresent:   false,
				CookiePresent: false,
				LocalAddr:     netip.MustParseAddr("198.51.100.1"),
			},
			wantFlags: "-",
		},
	}

	cfg := querylog.Config{PrintTime: "no", PrintCategory: false, PrintSeverity: false}
	ts := specTime()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := querylog.NewWithWriter(&buf, cfg)
			l.LogAt(tc.entry, ts, 0x1)
			line := strings.TrimSuffix(buf.String(), "\n")
			// Line format (no prefix segments):
			// client @0x1 <ip>#<port> (<qname>): view <view>: query: <qname> <class> <qtype> <flags> (<localip>)
			// The flags token is second-to-last (last is "(198.51.100.1)").
			parts := strings.Fields(line)
			if len(parts) < 2 {
				t.Fatalf("too few fields in line: %q", line)
			}
			flags := parts[len(parts)-2]
			if flags != tc.wantFlags {
				t.Errorf("flags: got %q want %q (line: %q)", flags, tc.wantFlags, line)
			}
		})
	}
}

// TestFormat_Qname verifies case preservation and trailing-dot stripping.
func TestFormat_Qname(t *testing.T) {
	cfg := querylog.Config{PrintTime: "no", PrintCategory: false, PrintSeverity: false}
	ts := specTime()

	cases := []struct {
		name      string
		qname     string
		wantQname string
	}{
		{"mixed case", "WwW.ExAmPlE.cOm.", "WwW.ExAmPlE.cOm"},
		{"root query", ".", "."},
		{"normal", "www.example.com.", "www.example.com"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := querylog.NewWithWriter(&buf, cfg)
			e := querylog.Entry{
				ClientAddr: netip.MustParseAddrPort("192.0.2.1:53"),
				Qname:      tc.qname,
				Qclass:     dns.ClassINET,
				Qtype:      dns.TypeA,
				ViewName:   "v",
				LocalAddr:  netip.MustParseAddr("198.51.100.1"),
			}
			l.LogAt(e, ts, 0x1)
			line := buf.String()

			// qname appears parenthesised after the client addr
			paren := "(" + tc.wantQname + ")"
			if !strings.Contains(line, paren) {
				t.Errorf("parenthesised qname %q not found in line: %q", paren, line)
			}
			// and again as a bare token after "query: "
			afterQuery := "query: " + tc.wantQname + " "
			if !strings.Contains(line, afterQuery) {
				t.Errorf("bare qname after 'query: ' not found in line: %q", line)
			}
		})
	}
}

// TestLogger_WritesLine verifies that a Logger backed by a real file (opened
// via OpenReopenSink) writes the correct content to disk.
func TestLogger_WritesLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queries.log")

	l, sink, err := querylog.New(path, querylog.Config{
		PrintTime:     "yes",
		PrintCategory: true,
		PrintSeverity: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := sink.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	want := "07-Jun-2026 05:59:41.389 queries: info: client @0x2a 192.0.2.10#16361 (www.example.com): view view-eu: query: www.example.com IN A -E(0)DC (198.51.100.7)\n"

	l.LogAt(baseEntry(), specTime(), 0x2a)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != want {
		t.Errorf("file content mismatch:\ngot:  %q\nwant: %q", string(data), want)
	}
}

// TestLogger_NilSafe verifies that a nil *Logger.Log is a safe no-op.
func TestLogger_NilSafe(t *testing.T) {
	var l *querylog.Logger
	l.Log(baseEntry()) // must not panic
}

// BenchmarkLog measures the full Log path with an in-memory sink.
// Requirement: 0 allocs/op in steady state.
func BenchmarkLog(b *testing.B) {
	l := querylog.NewWithWriter(nopWriter{}, querylog.Config{
		PrintTime:     "yes",
		PrintCategory: true,
		PrintSeverity: true,
	})
	e := baseEntry()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Log(e)
	}
}

// nopWriter is a zero-alloc io.Writer that discards all bytes.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
