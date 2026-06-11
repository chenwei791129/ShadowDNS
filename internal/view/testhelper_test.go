package view

import (
	"net/netip"
	"os"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"go4.org/netipx"
)

// buildCountryMMDB creates a temporary GeoLite2-Country-compatible mmdb file
// and returns its path. The db contains three entries:
//
//	192.0.2.1/32 → country "TH"
//	198.51.100.1/32 → country "JP"
//	203.0.113.0/32 → country "TW"
func buildCountryMMDB(t testing.TB) string {
	t.Helper()

	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}

	insertCountry := func(cidr string, code string) {
		prefix := netip.MustParsePrefix(cidr)
		ipNet := netipx.PrefixIPNet(prefix)
		record := mmdbtype.Map{
			"country": mmdbtype.Map{
				"iso_code": mmdbtype.String(code),
			},
		}
		if err := writer.Insert(ipNet, record); err != nil {
			t.Fatalf("writer.Insert(%s): %v", cidr, err)
		}
	}

	insertCountry("192.0.2.1/32", "TH")
	insertCountry("198.51.100.1/32", "JP")
	insertCountry("203.0.113.0/32", "TW")

	dir := t.TempDir()
	path := dir + "/GeoLite2-Country.mmdb"
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create mmdb file: %v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := writer.WriteTo(f); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return path
}

// buildASNMMDB creates a temporary GeoLite2-ASN-compatible mmdb file
// and returns its path. The db contains two entries:
//
//	203.0.113.1/32 → ASN 64500
//	203.0.113.2/32 → ASN 64501
func buildASNMMDB(t testing.TB) string {
	t.Helper()

	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-ASN",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}

	insertASN := func(cidr string, asn uint) {
		prefix := netip.MustParsePrefix(cidr)
		ipNet := netipx.PrefixIPNet(prefix)
		record := mmdbtype.Map{
			"autonomous_system_number": mmdbtype.Uint32(asn),
		}
		if err := writer.Insert(ipNet, record); err != nil {
			t.Fatalf("writer.Insert(%s): %v", cidr, err)
		}
	}

	insertASN("203.0.113.1/32", 64500)
	insertASN("203.0.113.2/32", 64501)

	dir := t.TempDir()
	path := dir + "/GeoLite2-ASN.mmdb"
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create mmdb file: %v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := writer.WriteTo(f); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return path
}
