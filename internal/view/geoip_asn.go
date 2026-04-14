package view

import (
	"net/netip"

	"github.com/oschwald/maxminddb-golang"
)

// ASNDB wraps a MaxMind GeoLite2-ASN mmdb reader.
type ASNDB struct {
	db *maxminddb.Reader
}

// OpenASNDB opens a GeoLite2-ASN.mmdb at path.
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

	var rec struct {
		ASN uint `maxminddb:"autonomous_system_number"`
	}

	netIP := ip.AsSlice()
	if err := a.db.Lookup(netIP, &rec); err != nil {
		// Lookup errors indicate bad data or unsupported IP version;
		// treat as no-match (not error) per audit discipline.
		return 0, false
	}

	if rec.ASN == 0 {
		return 0, false
	}
	return uint32(rec.ASN), true
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
