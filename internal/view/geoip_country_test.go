package view

import (
	"net/netip"
	"testing"
)

func TestCountryDB_Lookup(t *testing.T) {
	path := buildCountryMMDB(t)

	db, err := OpenCountryDB(path)
	if err != nil {
		t.Fatalf("OpenCountryDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	t.Run("known IP returns country code", func(t *testing.T) {
		code, ok := db.Lookup(netip.MustParseAddr("192.0.2.1"))
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if code != "TH" {
			t.Errorf("expected TH, got %q", code)
		}
	})

	t.Run("second known IP returns correct code", func(t *testing.T) {
		code, ok := db.Lookup(netip.MustParseAddr("198.51.100.1"))
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if code != "JP" {
			t.Errorf("expected JP, got %q", code)
		}
	})

	t.Run("unknown IP returns no-match", func(t *testing.T) {
		code, ok := db.Lookup(netip.MustParseAddr("10.0.0.1"))
		if ok {
			t.Errorf("expected ok=false for unknown IP, got code=%q", code)
		}
	})

	t.Run("nil CountryDB does not panic", func(t *testing.T) {
		var nilDB *CountryDB
		code, ok := nilDB.Lookup(netip.MustParseAddr("192.0.2.1"))
		if ok || code != "" {
			t.Errorf("expected ('', false), got (%q, %v)", code, ok)
		}
	})
}

func TestOpenCountryDB_MissingFile(t *testing.T) {
	_, err := OpenCountryDB("/nonexistent/path/GeoLite2-Country.mmdb")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestCountryDB_Metadata_ReturnsMetadata(t *testing.T) {
	path := buildCountryMMDB(t)

	db, err := OpenCountryDB(path)
	if err != nil {
		t.Fatalf("OpenCountryDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	meta := db.Metadata()
	if meta.DatabaseType == "" {
		t.Error("expected non-empty DatabaseType, got empty string")
	}
	if meta.BuildEpoch == 0 {
		t.Error("expected BuildEpoch > 0, got 0")
	}
}

func TestCountryDB_Metadata_NilReceiver(t *testing.T) {
	var nilDB *CountryDB
	meta := nilDB.Metadata()
	if meta.DatabaseType != "" {
		t.Errorf("expected empty DatabaseType for nil receiver, got %q", meta.DatabaseType)
	}
	if meta.BuildEpoch != 0 {
		t.Errorf("expected BuildEpoch 0 for nil receiver, got %d", meta.BuildEpoch)
	}
}

func BenchmarkCountryDB_Lookup(b *testing.B) {
	path := buildCountryMMDB(b)

	db, err := OpenCountryDB(path)
	if err != nil {
		b.Fatalf("OpenCountryDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	ip := netip.MustParseAddr("192.0.2.1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code, ok := db.Lookup(ip)
		if !ok || code == "" {
			b.Fatalf("unexpected miss: ok=%v code=%q", ok, code)
		}
	}
}
