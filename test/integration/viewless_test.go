// End-to-end test for viewless configurations (implicit-default-view):
// a named.conf that declares a top-level zone and no view block must start and
// serve that zone through the synthesized _default view (match-clients any),
// answering authoritatively for any source IP. This covers the matcher and
// query paths that --dry-run cannot reach (dry-run exits before NewServer).
//
// The fixture declares no geoip-directory and creates no mmdb file, relying on
// the geoip-optional behavior already in place: a config with no geo rules
// loads without GeoIP, and the synthesized _default view holds only an any rule.
//
// All fixture domains use RFC 2606 names and RFC 5737 IPs.
package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miekg/dns"
)

// writeViewlessFixture writes a complete config dir with no view block: an
// options block (no geoip-directory) plus a single top-level zone "example.com"
// whose apex A record is 192.0.2.10. No mmdb file is created. The zone body
// reuses noGeoIPZone (shared with the geoip-optional fixtures).
func writeViewlessFixture(t *testing.T, dir string) {
	t.Helper()

	zonePath := filepath.Join(dir, "example.com.fwd")
	if err := os.WriteFile(zonePath, []byte(noGeoIPZone("192.0.2.10")), 0o644); err != nil {
		t.Fatalf("write zone: %v", err)
	}

	namedConf := `options {
    directory "` + dir + `";
    listen-on { any; };
    recursion no;
};

zone "example.com" {
    type master;
    file "` + zonePath + `";
};
`
	if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(namedConf), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "shadowdns.yaml"), []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}
}

// TestViewless_StartsAndServesViaImplicitDefaultView verifies the end-to-end
// viewless path: a config with a top-level zone and no view block starts
// without any mmdb file and answers an A query authoritatively. A NOERROR
// answer with the zone's A record proves the synthesized _default view matched
// the (loopback) client and routed the query to the top-level zone.
func TestViewless_StartsAndServesViaImplicitDefaultView(t *testing.T) {
	dir := t.TempDir()
	writeViewlessFixture(t, dir)

	srv, teardown := newNoGeoIPTestServer(t, dir)
	defer teardown()

	resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasA(t, resp, "example.com", "192.0.2.10")
}
