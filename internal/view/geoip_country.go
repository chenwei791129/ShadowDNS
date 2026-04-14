package view

import (
	"net/netip"

	"github.com/oschwald/maxminddb-golang"
)

// CountryDB wraps a MaxMind GeoLite2-Country mmdb reader.
type CountryDB struct {
	db *maxminddb.Reader
}

// OpenCountryDB opens a GeoLite2-Country.mmdb at path.
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

	var rec struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}

	// maxminddb-golang expects a net.IP; convert via the standard library.
	netIP := ip.AsSlice()
	if err := c.db.Lookup(netIP, &rec); err != nil {
		// Lookup errors indicate bad data or unsupported IP version;
		// treat as no-match (not error) per audit discipline.
		return "", false
	}

	if rec.Country.ISOCode == "" {
		return "", false
	}
	return rec.Country.ISOCode, true
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
