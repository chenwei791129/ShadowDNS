package zone

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// msgDiscardDuplicate is the per-duplicate DEBUG message emitted for each
// byte-identical RR dropped at load. msgDuplicateSummary is the per-zone WARN
// aggregate. Held as consts so ParseFile and its tests reference the same
// strings.
const (
	msgDiscardDuplicate = "zone load: discarding duplicate record"
	msgDuplicateSummary = "zone load: duplicate record summary"
)

// ParseFile parses a single RFC 1035 zone file and returns the parsed Zone.
// Out-of-zone owner names are logged as warnings and skipped, matching
// BIND 9's behaviour. Syntax errors are still fatal.
//
// MUST NOT panic on any input.
func ParseFile(path string, origin string, logger *zap.Logger) (*Zone, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("zone: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// Lowercase-fold the origin: it is used as the zone's lookup-comparison
	// anchor (z.Origin keying, IsInZone bailiwick checks) and as miekg's
	// default origin for relative owner names. Per RFC 4343, comparisons are
	// case-insensitive, so storing the fold is canonical. Owner-name case in
	// the zone file is still preserved on the stored RR by miekg's parser.
	canonOrigin := dnsutil.LookupKey(origin)

	z := &Zone{
		Origin:  canonOrigin,
		Path:    path,
		Records: make(map[string]*qtypeStore),
	}

	// BIND accepts $INCLUDE with a double-quoted path, but miekg/dns rejects
	// the surrounding quotes. Strip them in a thin pre-processing layer so
	// that operator-authored zone files written in the BIND idiom load.
	zReader, err := rewriteBindIncludes(f)
	if err != nil {
		return nil, fmt.Errorf("zone: read %q: %w", path, err)
	}

	zp := dns.NewZoneParser(zReader, canonOrigin, path)
	zp.SetDefaultTTL(0)
	// Real BIND deployments split zones across $INCLUDE-d fragments; honour
	// them. Zone files come from trusted operator config, not network input.
	zp.SetIncludeAllowed(true)

	// Tally byte-identical RRs that AddRR discards, mirroring the backup-override
	// drop-summary shape (classify.go): per-duplicate DEBUG plus one per-zone
	// WARN aggregate. dupHist reuses dropHistogram for grep-stable, alphabetic
	// serialization. The DEBUG level is checked once and the per-record call is
	// skipped when disabled — at production scale a single zone-view can carry
	// thousands of duplicates and zap evaluates field args at the call site
	// even for a filtered entry.
	dupHist := dropHistogram{}
	debugEnabled := logger.Core().Enabled(zapcore.DebugLevel)

	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		// ownerName is a local lowercase fold used solely for the IsInZone
		// bailiwick check and the warn log; the RR itself is passed to
		// AddRR unmodified, so rr.Header().Name retains its zone-file case
		// for on-wire transmission (per the AddRR case-preservation invariant).
		ownerName := strings.ToLower(rr.Header().Name)
		if !dnsutil.IsInZone(ownerName, canonOrigin) {
			attrs := []any{"file", path, "owner", ownerName, "zone", canonOrigin}
			if line := findOwnerLine(path, ownerName); line > 0 {
				attrs = append(attrs, "line", line)
			}
			logger.Sugar().Warnw("ignoring out-of-zone data", attrs...)
			continue
		}
		if !z.AddRR(rr) {
			rrtype := dns.TypeToString[rr.Header().Rrtype]
			dupHist[rrtype]++
			if debugEnabled {
				logger.Debug(msgDiscardDuplicate,
					zap.String("zone", canonOrigin),
					zap.String("owner", ownerName),
					zap.String("type", rrtype),
				)
			}
		}
	}

	if err := zp.Err(); err != nil {
		return nil, fmt.Errorf("zone: parse %q: %w", path, err)
	}

	dupTotal := 0
	for _, n := range dupHist {
		dupTotal += n
	}
	if dupTotal > 0 {
		logger.Warn(msgDuplicateSummary,
			zap.String("zone", canonOrigin),
			zap.Int("count", dupTotal),
			zap.Object("duplicates", dupHist),
		)
	}

	return z, nil
}

// rewriteBindIncludes wraps r so that BIND-style quoted $INCLUDE directives
// (e.g. `$include "path/to/file"`) are converted into the bare-form syntax
// that miekg/dns's zone scanner accepts. Only lines whose first non-
// whitespace token is the $INCLUDE directive (case-insensitive) are touched;
// other lines pass through byte-for-byte. The leading and trailing quote
// characters around the path are replaced with spaces rather than removed,
// so parser error line:col positions remain aligned with the original file.
//
// If a $INCLUDE line has an unquoted path, or an opening `"` without a
// matching closing `"` on the same line, the line is passed through
// unchanged so that miekg can report any error against the original input.
//
// Limitations:
//   - Nested quoted $INCLUDE: files pulled in via $INCLUDE are opened
//     directly by miekg and bypass this layer; only the top-level zone
//     file is rewritten.
//   - Whitespace inside the path: miekg's scanner tokenises the path
//     argument as a single zString (no whitespace allowed). A quoted
//     path containing space or tab will still fail — this wrapper only
//     removes the surrounding quote characters, not the whitespace they
//     wrap. Operators must avoid whitespace in zone-file include paths.
func rewriteBindIncludes(r io.Reader) (io.Reader, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(src))
	for offset := 0; offset < len(src); {
		nl := bytes.IndexByte(src[offset:], '\n')
		var line []byte
		hasNL := nl >= 0
		if hasNL {
			line = src[offset : offset+nl]
			offset += nl + 1
		} else {
			line = src[offset:]
			offset = len(src)
		}

		out = append(out, rewriteIncludeLine(line)...)
		if hasNL {
			out = append(out, '\n')
		}
	}

	return bytes.NewReader(out), nil
}

// rewriteIncludeLine returns a copy of line with the path-wrapping `"`
// characters replaced by spaces if line is a $INCLUDE directive with a
// quoted path; otherwise returns line unchanged.
func rewriteIncludeLine(line []byte) []byte {
	// Skip leading whitespace.
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}

	const directive = "$INCLUDE"
	if i+len(directive) > len(line) {
		return line
	}
	if !bytes.EqualFold(line[i:i+len(directive)], []byte(directive)) {
		return line
	}

	// Directive must be followed by whitespace.
	j := i + len(directive)
	if j >= len(line) || (line[j] != ' ' && line[j] != '\t') {
		return line
	}

	// Skip whitespace between directive and path token.
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}

	// Need an opening quote — bare paths are already accepted by miekg.
	if j >= len(line) || line[j] != '"' {
		return line
	}

	openQuote := j
	closeQuote := -1
	for k := j + 1; k < len(line); k++ {
		if line[k] == '"' {
			closeQuote = k
			break
		}
	}
	if closeQuote == -1 {
		// Unmatched quote — let miekg surface the error against the
		// original line/column.
		return line
	}

	// Replace both quotes with spaces; preserves length and column alignment.
	out := make([]byte, len(line))
	copy(out, line)
	out[openQuote] = ' '
	out[closeQuote] = ' '
	return out
}

// findOwnerLine returns the 1-based line number of the first record whose
// owner token matches name, or 0 if not found.
func findOwnerLine(path, name string) int {
	// Best-effort: if the file vanished between the initial parse and this
	// re-scan, return 0 and let the caller omit the line number.
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	needle := strings.TrimSuffix(strings.ToLower(name), ".")
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if raw == "" || raw[0] == ' ' || raw[0] == '\t' {
			continue
		}
		if i := strings.IndexByte(raw, ';'); i >= 0 {
			raw = raw[:i]
		}
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		if strings.TrimSuffix(strings.ToLower(fields[0]), ".") == needle {
			return lineNo
		}
	}
	return 0
}
