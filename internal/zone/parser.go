package zone

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// ParseFile parses a single RFC 1035 zone file and returns the parsed Zone.
// path is the absolute path to the zone file.
// origin is the zone's apex name (e.g. "example.com.") supplied by the caller.
//
// Records whose owner name falls outside the zone origin are logged as
// warnings and silently skipped, matching BIND 9's behaviour. Syntax errors
// are still fatal.
//
// MUST NOT panic on any input.
func ParseFile(path string, origin string, logger *slog.Logger) (*Zone, error) {
	if logger == nil {
		logger = slog.Default()
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("zone: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	canonOrigin := dnsutil.Canonicalize(origin)

	z := &Zone{
		Origin:  canonOrigin,
		Path:    path,
		Records: make(map[string][]dns.RR),
	}

	zp := dns.NewZoneParser(f, canonOrigin, path)
	zp.SetDefaultTTL(0)
	// Real BIND deployments split zones across $INCLUDE-d fragments; honour
	// them. Zone files come from trusted operator config, not network input.
	zp.SetIncludeAllowed(true)

	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		ownerName := strings.ToLower(rr.Header().Name)
		if !dnsutil.IsInZone(ownerName, canonOrigin) {
			line := findOwnerLine(path, ownerName)
			if line > 0 {
				logger.Warn("ignoring out-of-zone data",
					"file", path, "line", line,
					"owner", ownerName, "zone", canonOrigin)
			} else {
				logger.Warn("ignoring out-of-zone data",
					"file", path,
					"owner", ownerName, "zone", canonOrigin)
			}
			continue
		}
		z.AddRR(rr)
	}

	if err := zp.Err(); err != nil {
		return nil, fmt.Errorf("zone: parse %q: %w", path, err)
	}

	return z, nil
}

// findOwnerLine returns the 1-based line number of the first record whose
// owner token matches name, or 0 if not found. Leading-whitespace lines are
// skipped because their owner is inherited from the previous record.
func findOwnerLine(path, name string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	needle := trimTrailingDot(strings.ToLower(name))
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
		if trimTrailingDot(strings.ToLower(fields[0])) == needle {
			return lineNo
		}
	}
	return 0
}

func trimTrailingDot(s string) string {
	return strings.TrimSuffix(s, ".")
}
