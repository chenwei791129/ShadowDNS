package view

import (
	"net/netip"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
)

// ASNDB wraps a MaxMind ASN mmdb reader (GeoIP2 or GeoLite2 edition; the
// on-disk format is identical).
type ASNDB struct {
	db *maxminddb.Reader
}

// OpenASNDB opens an ASN mmdb (GeoIP2 or GeoLite2) at path.
// Returns an error if the file is missing or fails mmdb validation.
// The caller is expected to treat this as a fatal startup condition.
func OpenASNDB(path string) (*ASNDB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &ASNDB{db: r}, nil
}

// Lookup returns the autonomous system number for ip,
// or (0, false) when no record exists.
// MUST NOT panic on any input.
func (a *ASNDB) Lookup(ip netip.Addr) (uint32, bool) {
	if a == nil || a.db == nil {
		return 0, false
	}

	var asn uint32
	if err := a.db.Lookup(ip).DecodePath(&asn, "autonomous_system_number"); err != nil {
		// Lookup errors indicate bad data or unsupported IP version;
		// treat as no-match (not error) per audit discipline.
		return 0, false
	}

	if asn == 0 {
		return 0, false
	}
	return asn, true
}

// Metadata returns the mmdb file metadata. Returns a zero-value Metadata
// if the receiver or its database is nil.
func (a *ASNDB) Metadata() maxminddb.Metadata {
	if a == nil || a.db == nil {
		return maxminddb.Metadata{}
	}
	return a.db.Metadata
}

// Close releases the mmdb file handle.
func (a *ASNDB) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}
