package zone

import (
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
// Wraps github.com/miekg/dns ZoneParser. Bubbles up errors with path:line.
//
// MUST NOT panic on any input.
func ParseFile(path string, origin string) (*Zone, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("zone: open %q: %w", path, err)
	}
	defer f.Close()

	// Normalize origin: lowercase + trailing dot.
	canonOrigin := dnsutil.Canonicalize(origin)

	z := &Zone{
		Origin:  canonOrigin,
		Path:    path,
		Records: make(map[string][]dns.RR),
	}

	zp := dns.NewZoneParser(f, canonOrigin, path)
	// Enable parsing of $TTL and other directives.
	zp.SetDefaultTTL(0)

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

// checkInZone verifies that the owner name is within the zone origin.
// It returns an error citing the file path if the owner is out-of-zone.
func checkInZone(owner, origin, path string) error {
	if dnsutil.IsInZone(owner, origin) {
		return nil
	}
	return fmt.Errorf("zone: %q: out-of-zone owner name %q (zone origin: %q)", path, owner, origin)
}
