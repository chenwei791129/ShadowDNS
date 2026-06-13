// End-to-end tests for the BIND-compatibility ("tolerant parsing") posture:
// a stock Debian/BIND named.conf — viewless, with default-zones, top-level
// acl/key/controls blocks, and a root `type hint` zone — must load without a
// fatal error, drop the constructs ShadowDNS does not serve, and answer
// authoritatively for the zones it does serve. A separate case pins the
// fail-closed contract for an unevaluable match-clients rule.
//
// All fixture domains are localhost/RFC 2606; all IPs are loopback or RFC 5737.
package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miekg/dns"
)

// TestBindCompat_ViewlessDefaultZonesLoadAndServe verifies the headline
// drop-in path: the bindcompat fixture loads without a fatal error (proving
// top-level acl/key/controls and the root `type hint` zone are skipped, not
// fatal — the hint zone's `file "db.root"` is absent, so a load success also
// proves the dropped zone's file is never opened), and the `type master`
// localhost zone answers an A query authoritatively.
func TestBindCompat_ViewlessDefaultZonesLoadAndServe(t *testing.T) {
	srv, teardown := serveBindCompat(t)
	defer teardown()

	resp := queryUDP(t, udpAddr(srv), "localhost", dns.TypeA)
	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertHasA(t, resp, "localhost", "127.0.0.1")
}

// TestBindCompat_NamedAclMatchClientsFailClosed verifies that a view whose only
// match-clients rule is a named-acl reference (dropped by the tolerant parser)
// serves nothing: with no rule left to match and no later `any` view, the
// loopback client matches no view and receives REFUSED rather than being served
// the view's zone. This is the fail-closed safety doctrine end to end.
func TestBindCompat_NamedAclMatchClientsFailClosed(t *testing.T) {
	dir := t.TempDir()

	zonePath := filepath.Join(dir, "example.com.fwd")
	if err := os.WriteFile(zonePath, []byte(noGeoIPZone("192.0.2.10")), 0o644); err != nil {
		t.Fatalf("write zone: %v", err)
	}

	// "internal-net" is a named-acl reference ShadowDNS cannot evaluate; it is
	// dropped, leaving the view with an empty rule set (fail-closed).
	namedConf := `options {
    listen-on { any; };
    recursion no;
};

view "internal" {
    match-clients { internal-net; };
    zone "example.com" {
        type master;
        file "` + zonePath + `";
    };
};
`
	if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(namedConf), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shadowdns.yaml"), []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}

	srv, teardown := newNoGeoIPTestServer(t, dir)
	defer teardown()

	resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED (fail-closed: dropped rule serves no client), got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("a fail-closed view must serve no records, got: %v", resp.Answer)
	}
}
