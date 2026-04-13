package zone

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// ParseFile parses a single RFC 1035 zone file and returns the parsed Zone.
// path is the absolute path to the zone file.
// origin is the zone's apex name (e.g. "example.com.") supplied by the caller
// (LoadNamedConf already knows the origin from the zone block in named.conf).
//
// Wraps github.com/miekg/dns ZoneParser. Errors include file path and line
// number — syntax errors from miekg already embed line info; out-of-zone
// owner errors trigger a second-pass scan to locate the offending line.
//
// MUST NOT panic on any input.
func ParseFile(path string, origin string) (*Zone, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("zone: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// Normalize origin: lowercase + trailing dot.
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
		// Validate that the owner name is within the zone origin.
		ownerName := strings.ToLower(rr.Header().Name)
		if err := checkInZone(ownerName, canonOrigin, path); err != nil {
			return nil, err
		}
		z.AddRR(rr)
	}

	if err := zp.Err(); err != nil {
		return nil, fmt.Errorf("zone: parse %q: %w", path, err)
	}

	return z, nil
}

func checkInZone(owner, origin, path string) error {
	if dnsutil.IsInZone(owner, origin) {
		return nil
	}
	line := findOwnerLine(path, owner)
	if line > 0 {
		return fmt.Errorf("zone: %s:%d: out-of-zone owner name %q (zone origin: %q)", path, line, owner, origin)
	}
	return fmt.Errorf("zone: %s: out-of-zone owner name %q (zone origin: %q)", path, owner, origin)
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
	// Raise the line cap so pathological records (e.g. long TLSA/TXT strings)
	// don't silently truncate the scan and lose the match.
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
