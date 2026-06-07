// Package querylog implements a BIND9-compatible per-query log writer.
//
// The Logger accepts Entry values produced by the DNS hot path and appends
// one line per entry to a file sink. The format is identical to BIND9's
// queries channel output so that downstream log parsers continue to work
// without modification.
//
// Performance contract: the Log hot path performs zero heap allocations in
// steady state. Buffer pooling (sync.Pool of *[]byte) and append-based
// formatting ensure this.
package querylog

import (
	"io"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/logging"
)

// Config holds the print-option flags parsed from a named.conf logging channel.
// It is intentionally independent of internal/config so that the querylog
// package can be constructed and tested without pulling in config parsing.
type Config struct {
	// PrintTime controls timestamp rendering.
	// Accepted values: "yes", "local", "iso8601", "iso8601-utc", "no".
	// "yes" and "local" both render local time as dd-Mmm-yyyy HH:MM:SS.mmm.
	// "iso8601" renders local time as yyyy-MM-ddTHH:MM:SS.mmm.
	// "iso8601-utc" renders UTC time as yyyy-MM-ddTHH:MM:SS.mmm.
	// "no" omits the timestamp segment entirely.
	PrintTime string

	// PrintCategory, when true, includes the "queries: " segment.
	PrintCategory bool

	// PrintSeverity, when true, includes the "info: " segment.
	PrintSeverity bool
}

// Entry carries all fields needed to format a single BIND9 query log line.
// It is a value type; callers allocate it on the stack before calling Log.
type Entry struct {
	// ClientAddr is the client's IP address and port.
	ClientAddr netip.AddrPort

	// Qname is the query name exactly as it arrived on the wire
	// (case-preserved, presentation form, WITH the trailing dot).
	// A root query is represented as ".".
	Qname string

	// Qclass and Qtype are the DNS class and type as uint16 wire values.
	Qclass uint16
	Qtype  uint16

	// ViewName is the name of the view that matched this query.
	ViewName string

	// RD is the Recursion Desired bit from the query header.
	RD bool

	// DO is the DNSSEC OK bit from the EDNS OPT record.
	DO bool

	// CD is the Checking Disabled bit from the query header.
	CD bool

	// TCP is true when the query arrived over TCP.
	TCP bool

	// EDNSPresent is true when the query carried an EDNS OPT record.
	EDNSPresent bool

	// EDNSVersion is the EDNS version number (0 for EDNS0).
	// Only meaningful when EDNSPresent is true.
	EDNSVersion uint8

	// CookiePresent is true when the EDNS OPT record contained a COOKIE option.
	CookiePresent bool

	// LocalAddr is the local IP address the query was received on (no port).
	LocalAddr netip.Addr
}

// Timestamp layouts for the print-time variants.
const (
	bindTimeLayout = "02-Jan-2006 15:04:05.000"
	isoTimeLayout  = "2006-01-02T15:04:05.000"
)

// Logger writes BIND9-compatible query log lines to a sink.
// A nil *Logger is safe: its Log method is a no-op.
//
// The print options are resolved once at construction (timeLayout/timeUTC
// instead of re-interpreting Config.PrintTime on every Log call).
type Logger struct {
	sink          io.Writer
	timeLayout    string // empty: omit the timestamp segment
	timeUTC       bool
	printCategory bool
	printSeverity bool
	pool          sync.Pool
	counter       atomic.Uint64
}

// New opens the file at path using logging.OpenReopenSink (O_APPEND|O_CREATE,
// mode 0640) and returns a Logger backed by that sink together with the
// ReopenSink so the caller can wire it into the SIGUSR1 handler.
//
// An open failure is returned immediately so that daemon startup aborts loudly
// rather than silently disabling query logging.
func New(path string, cfg Config) (*Logger, *logging.ReopenSink, error) {
	sink, err := logging.OpenReopenSink(path)
	if err != nil {
		return nil, nil, err
	}
	return NewWithWriter(sink, cfg), sink, nil
}

// NewWithWriter constructs a Logger that writes to an arbitrary io.Writer.
// Intended for tests and benchmarks where no real file is needed.
func NewWithWriter(w io.Writer, cfg Config) *Logger {
	l := &Logger{
		sink:          w,
		printCategory: cfg.PrintCategory,
		printSeverity: cfg.PrintSeverity,
	}
	switch cfg.PrintTime {
	case "yes", "local":
		l.timeLayout = bindTimeLayout
	case "iso8601":
		l.timeLayout = isoTimeLayout
	case "iso8601-utc":
		l.timeLayout = isoTimeLayout
		l.timeUTC = true
		// "no" or any unrecognised value: timeLayout stays empty (omit)
	}
	l.pool = sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 256)
			return &buf
		},
	}
	return l
}

// Log formats e as a BIND9 query log line and writes it to the sink in a
// single Write call. Log is safe to call concurrently.
//
// Calling Log on a nil *Logger is a no-op.
func (l *Logger) Log(e Entry) {
	if l == nil {
		return
	}
	token := l.counter.Add(1)
	l.LogAt(e, time.Now(), token)
}

// LogAt is identical to Log but accepts an explicit timestamp and token value.
// It exists so tests can inject a fixed time and token for byte-exact assertions
// without depending on global state.
func (l *Logger) LogAt(e Entry, now time.Time, token uint64) {
	if l == nil {
		return
	}

	bufp := l.pool.Get().(*[]byte)
	buf := (*bufp)[:0]

	buf = l.appendLine(buf, e, now, token)

	// Single Write call — ReopenSink's mutex serializes concurrent writes.
	_, _ = l.sink.Write(buf)

	*bufp = buf
	l.pool.Put(bufp)
}

// appendLine assembles the complete log line (including the trailing newline)
// into dst and returns the extended slice. It never allocates.
func (l *Logger) appendLine(dst []byte, e Entry, now time.Time, token uint64) []byte {
	needSpace := false

	// --- timestamp segment ---
	if l.timeLayout != "" {
		if l.timeUTC {
			now = now.UTC()
		}
		dst = now.AppendFormat(dst, l.timeLayout)
		needSpace = true
	}

	// --- category segment: "queries: " ---
	if l.printCategory {
		if needSpace {
			dst = append(dst, ' ')
		}
		dst = append(dst, "queries: "...)
		needSpace = false // trailing space already embedded in literal
	}

	// --- severity segment: "info: " ---
	if l.printSeverity {
		if needSpace {
			dst = append(dst, ' ')
			needSpace = false
		}
		dst = append(dst, "info: "...)
	}

	// --- "client @0x<hex> " ---
	if needSpace {
		dst = append(dst, ' ')
	}
	dst = append(dst, "client @0x"...)
	dst = strconv.AppendUint(dst, token, 16)
	dst = append(dst, ' ')

	// --- "<ip>#<port> " ---
	dst = appendAddrPort(dst, e.ClientAddr)
	dst = append(dst, ' ')

	// --- "(<qname>): " ---
	qname := stripTrailingDot(e.Qname)
	dst = append(dst, '(')
	dst = append(dst, qname...)
	dst = append(dst, "): "...)

	// --- "view <view>: " ---
	dst = append(dst, "view "...)
	dst = append(dst, e.ViewName...)
	dst = append(dst, ": "...)

	// --- "query: <qname> <class> <qtype> <flags> (<localip>)" ---
	dst = append(dst, "query: "...)
	dst = append(dst, qname...)
	dst = append(dst, ' ')
	dst = appendClass(dst, e.Qclass)
	dst = append(dst, ' ')
	dst = appendType(dst, e.Qtype)
	dst = append(dst, ' ')
	dst = appendFlags(dst, e)
	dst = append(dst, " ("...)
	dst = appendAddr(dst, e.LocalAddr)
	dst = append(dst, ')')
	dst = append(dst, '\n')

	return dst
}

// stripTrailingDot removes the trailing dot from a DNS presentation-form name.
// A root query (".") is returned unchanged — the trailing dot IS the name.
func stripTrailingDot(name string) string {
	if len(name) > 1 && name[len(name)-1] == '.' {
		return name[:len(name)-1]
	}
	return name
}

// appendAddrPort appends "<ip>#<port>" to dst without allocating.
func appendAddrPort(dst []byte, ap netip.AddrPort) []byte {
	dst = ap.Addr().AppendTo(dst)
	dst = append(dst, '#')
	dst = strconv.AppendUint(dst, uint64(ap.Port()), 10)
	return dst
}

// appendAddr appends the IP address (without port) to dst without allocating.
func appendAddr(dst []byte, a netip.Addr) []byte {
	return a.AppendTo(dst)
}

// appendClass appends the DNS class mnemonic (e.g. "IN") or "CLASS<n>" for
// unknown classes, matching BIND9's RFC 3597 fallback behaviour.
func appendClass(dst []byte, class uint16) []byte {
	if s, ok := dns.ClassToString[class]; ok {
		return append(dst, s...)
	}
	dst = append(dst, "CLASS"...)
	return strconv.AppendUint(dst, uint64(class), 10)
}

// appendType appends the DNS type mnemonic (e.g. "A", "AAAA") or "TYPE<n>"
// for unknown types, matching BIND9's RFC 3597 fallback behaviour.
func appendType(dst []byte, qtype uint16) []byte {
	if s, ok := dns.TypeToString[qtype]; ok {
		return append(dst, s...)
	}
	dst = append(dst, "TYPE"...)
	return strconv.AppendUint(dst, uint64(qtype), 10)
}

// appendFlags assembles the BIND9 flags field into dst.
//
// Format: +/- (RD), then without separators: E(n) EDNS, T TCP, D DO, C CD, K COOKIE.
// S (TSIG) and V (valid cookie) are never emitted.
func appendFlags(dst []byte, e Entry) []byte {
	if e.RD {
		dst = append(dst, '+')
	} else {
		dst = append(dst, '-')
	}
	if e.EDNSPresent {
		dst = append(dst, "E("...)
		dst = strconv.AppendUint(dst, uint64(e.EDNSVersion), 10)
		dst = append(dst, ')')
	}
	if e.TCP {
		dst = append(dst, 'T')
	}
	if e.DO {
		dst = append(dst, 'D')
	}
	if e.CD {
		dst = append(dst, 'C')
	}
	if e.CookiePresent {
		dst = append(dst, 'K')
	}
	return dst
}
