package view

import (
	"net/netip"
	"runtime"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
)

// CountryDB wraps a MaxMind Country mmdb reader (GeoIP2 or GeoLite2 edition;
// the on-disk format is identical).
type CountryDB struct {
	db *maxminddb.Reader
}

// OpenCountryDB opens a Country mmdb (GeoIP2 or GeoLite2) at path.
// Returns an error if the file is missing or fails mmdb validation.
// The caller is expected to treat this as a fatal startup condition.
func OpenCountryDB(path string) (*CountryDB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &CountryDB{db: r}, nil
}

// Lookup returns the ISO 3166-1 alpha-2 country code (as stored in the mmdb,
// typically uppercase) for ip, or ("", false) when no record exists.
// MUST NOT panic on any input.
func (c *CountryDB) Lookup(ip netip.Addr) (string, bool) {
	if c == nil || c.db == nil {
		return "", false
	}

	var iso string
	err := c.db.Lookup(ip).DecodePath(&iso, "country", "iso_code")
	// Keep the reader (hence its mmap) alive until the decode above completes,
	// so a concurrent reclamation cannot unmap it mid-decode regardless of how
	// a caller uses the state snapshot afterward. Compile-time liveness barrier
	// with no runtime cost.
	runtime.KeepAlive(c.db)
	if err != nil {
		// Lookup errors indicate bad data or unsupported IP version;
		// treat as no-match (not error) per audit discipline.
		return "", false
	}

	if iso == "" {
		return "", false
	}
	return iso, true
}

// Metadata returns the mmdb file metadata. Returns a zero-value Metadata
// if the receiver or its database is nil.
func (c *CountryDB) Metadata() maxminddb.Metadata {
	if c == nil || c.db == nil {
		return maxminddb.Metadata{}
	}
	return c.db.Metadata
}

// Close releases the mmdb file handle.
func (c *CountryDB) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}
