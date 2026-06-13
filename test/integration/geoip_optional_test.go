// End-to-end test for operating without GeoIP databases (geoip-optional):
// a configuration using only any/IP/CIDR match-clients rules and no
// geoip-directory must start with no mmdb file anywhere and route queries
// through the matcher — covering the matcher and query paths that --dry-run
// cannot reach.
//
// All fixture domains use RFC 2606 names and RFC 5737 IPs.
package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miekg/dns"
)

// noGeoIPZone returns a minimal example.com zone whose apex A record is ip,
// so each view can serve a distinguishable answer.
func noGeoIPZone(ip string) string {
	return `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2024010101  ; serial
    3600
    600
    604800
    300
)

@   IN NS    ns1.example.com.
@   IN A     ` + ip + `
ns1 IN A     192.0.2.1
`
}

// writeNoGeoIPFixture writes a complete config dir with no geoip-directory
// and two views: view-cidr (127.0.0.0/8, answers 192.0.2.77) declared before
// view-fallback (any, answers 192.0.2.88). The loopback test client can only
// receive 192.0.2.77 by matching the CIDR rule. No mmdb file is created.
func writeNoGeoIPFixture(t *testing.T, dir string) {
	t.Helper()

	zoneCIDR := filepath.Join(dir, "example.com.cidr.zone")
	if err := os.WriteFile(zoneCIDR, []byte(noGeoIPZone("192.0.2.77")), 0o644); err != nil {
		t.Fatalf("write cidr zone: %v", err)
	}
	zoneAny := filepath.Join(dir, "example.com.any.zone")
	if err := os.WriteFile(zoneAny, []byte(noGeoIPZone("192.0.2.88")), 0o644); err != nil {
		t.Fatalf("write any zone: %v", err)
	}

	masterZones := filepath.Join(dir, "master.zones")
	masterZonesContent := `view "view-cidr" {
    match-clients { 127.0.0.0/8; };
    recursion no;
    zone "example.com" {
        type master;
        file "` + zoneCIDR + `";
    };
};

view "view-fallback" {
    match-clients { any; };
    recursion no;
    zone "example.com" {
        type master;
        file "` + zoneAny + `";
    };
};
`
	if err := os.WriteFile(masterZones, []byte(masterZonesContent), 0o644); err != nil {
		t.Fatalf("write master.zones: %v", err)
	}

	namedConf := filepath.Join(dir, "named.conf")
	namedConfContent := `options {
    directory "` + dir + `";
    listen-on { any; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(namedConfContent), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "shadowdns.yaml"), []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}
}

// TestNoGeoIP_StartsAndRoutesByCIDR verifies the end-to-end no-GeoIP path:
// the server starts without any mmdb file, and a query whose source IP
// matches the CIDR rule receives the authoritative answer from that view's
// zone — not from the later any-rule fallback view.
func TestNoGeoIP_StartsAndRoutesByCIDR(t *testing.T) {
	dir := t.TempDir()
	writeNoGeoIPFixture(t, dir)

	srv, teardown := newNoGeoIPTestServer(t, dir)
	defer teardown()

	resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	// 192.0.2.77 proves the CIDR view matched; 192.0.2.88 would mean the
	// query fell through to the any-rule view.
	assertHasA(t, resp, "example.com", "192.0.2.77")
}
