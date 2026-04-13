package view

import (
	"net/netip"
	"testing"
)

func TestASNDB_Lookup(t *testing.T) {
	path := buildASNMMDB(t)

	db, err := OpenASNDB(path)
	if err != nil {
		t.Fatalf("OpenASNDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	t.Run("known IP returns ASN", func(t *testing.T) {
		asn, ok := db.Lookup(netip.MustParseAddr("203.0.113.1"))
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if asn != 64500 {
			t.Errorf("expected 64500, got %d", asn)
		}
	})

	t.Run("second known IP returns correct ASN", func(t *testing.T) {
		asn, ok := db.Lookup(netip.MustParseAddr("203.0.113.2"))
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if asn != 64501 {
			t.Errorf("expected 64501, got %d", asn)
		}
	})

	t.Run("unknown IP returns no-match", func(t *testing.T) {
		asn, ok := db.Lookup(netip.MustParseAddr("10.0.0.1"))
		if ok {
			t.Errorf("expected ok=false for unknown IP, got asn=%d", asn)
		}
	})

	t.Run("nil ASNDB does not panic", func(t *testing.T) {
		var nilDB *ASNDB
		asn, ok := nilDB.Lookup(netip.MustParseAddr("203.0.113.1"))
		if ok || asn != 0 {
			t.Errorf("expected (0, false), got (%d, %v)", asn, ok)
		}
	})
}

func TestOpenASNDB_MissingFile(t *testing.T) {
	_, err := OpenASNDB("/nonexistent/path/GeoLite2-ASN.mmdb")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
