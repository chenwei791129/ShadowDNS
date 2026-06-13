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

	"github.com/chenwei791129/ShadowDNS/internal/server"
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

// serveACLFixture writes named.conf, one zone file per (filename→A) entry, and a
// minimal shadowdns.yaml into a fresh dir, then serves them through the
// production chain with no GeoIP. Each zone file serves example.com with the
// given A record (via noGeoIPZone). The named.conf must reference zone files by
// their relative name so they resolve against the fixture dir.
func serveACLFixture(t *testing.T, namedConf string, zones map[string]string) (*server.Server, func()) {
	t.Helper()
	dir := t.TempDir()
	for fname, ip := range zones {
		if err := os.WriteFile(filepath.Join(dir, fname), []byte(noGeoIPZone(ip)), 0o644); err != nil {
			t.Fatalf("write zone %s: %v", fname, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(namedConf), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shadowdns.yaml"), []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}
	return newNoGeoIPTestServer(t, dir)
}

// TestBindCompat_AclBasedViewSelection drives the full ACL grammar end to end:
// a defined acl referenced by name, `!` negation, a nested `{ ... }` group, the
// built-in `localhost`, and an undefined reference that stays fail-closed. The
// test client is always loopback (127.0.0.1), so each fixture distinguishes the
// winning view by the A record it serves for example.com (RFC 5737 addresses);
// match-clients use loopback (127.0.0.0/8 — the actual client) to land in or out
// of a view and 192.0.2.0/24 (RFC 5737) for the non-matching case.
func TestBindCompat_AclBasedViewSelection(t *testing.T) {
	t.Run("named reference selects the matching view", func(t *testing.T) {
		// view "internal" matches the loopback client via the named acl; the
		// later "external" view (any) would otherwise win.
		namedConf := `options { recursion no; };
acl "loopback" { 127.0.0.0/8; };
view "internal" {
    match-clients { loopback; };
    zone "example.com" { type master; file "internal.fwd"; };
};
view "external" {
    match-clients { any; };
    zone "example.com" { type master; file "external.fwd"; };
};
`
		srv, teardown := serveACLFixture(t, namedConf, map[string]string{
			"internal.fwd": "192.0.2.10",
			"external.fwd": "192.0.2.20",
		})
		defer teardown()

		resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
		assertNoError(t, resp)
		assertHasA(t, resp, "example.com", "192.0.2.10")
	})

	t.Run("negation rejects the loopback client and falls through", func(t *testing.T) {
		// "non-loop" excludes loopback via `! loopback`, so the loopback client
		// falls through to "loop".
		namedConf := `options { recursion no; };
acl "loopback" { 127.0.0.0/8; };
view "non-loop" {
    match-clients { ! loopback; any; };
    zone "example.com" { type master; file "nonloop.fwd"; };
};
view "loop" {
    match-clients { any; };
    zone "example.com" { type master; file "loop.fwd"; };
};
`
		srv, teardown := serveACLFixture(t, namedConf, map[string]string{
			"nonloop.fwd": "192.0.2.30",
			"loop.fwd":    "192.0.2.40",
		})
		defer teardown()

		resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
		assertNoError(t, resp)
		// 192.0.2.40 proves `! loopback` rejected the loopback client from the
		// first view and evaluation fell through.
		assertHasA(t, resp, "example.com", "192.0.2.40")
	})

	t.Run("nested group matches the loopback client", func(t *testing.T) {
		namedConf := `options { recursion no; };
view "grouped" {
    match-clients { { 127.0.0.0/8; }; };
    zone "example.com" { type master; file "grouped.fwd"; };
};
view "other" {
    match-clients { any; };
    zone "example.com" { type master; file "other.fwd"; };
};
`
		srv, teardown := serveACLFixture(t, namedConf, map[string]string{
			"grouped.fwd": "192.0.2.50",
			"other.fwd":   "192.0.2.60",
		})
		defer teardown()

		resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
		assertNoError(t, resp)
		assertHasA(t, resp, "example.com", "192.0.2.50")
	})

	t.Run("built-in localhost matches the loopback client", func(t *testing.T) {
		// The loopback source address is one of the server's own addresses, so
		// the localhost built-in selects this view.
		namedConf := `options { recursion no; };
view "lh" {
    match-clients { localhost; };
    zone "example.com" { type master; file "lh.fwd"; };
};
view "other" {
    match-clients { any; };
    zone "example.com" { type master; file "other.fwd"; };
};
`
		srv, teardown := serveACLFixture(t, namedConf, map[string]string{
			"lh.fwd":    "192.0.2.70",
			"other.fwd": "192.0.2.80",
		})
		defer teardown()

		resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
		assertNoError(t, resp)
		assertHasA(t, resp, "example.com", "192.0.2.70")
	})

	t.Run("undefined reference is fail-closed and evaluation falls through", func(t *testing.T) {
		// "broken" references an undefined acl (dropped fail-closed → empty
		// match-clients → never matches); the loopback client falls through to
		// "ok", which matches via the defined acl. 192.0.2.100 (not the broken
		// view's 192.0.2.90) proves the dropped reference never served.
		namedConf := `options { recursion no; };
acl "loopback" { 127.0.0.0/8; };
view "broken" {
    match-clients { nosuchacl; };
    zone "example.com" { type master; file "broken.fwd"; };
};
view "ok" {
    match-clients { loopback; };
    zone "example.com" { type master; file "ok.fwd"; };
};
`
		srv, teardown := serveACLFixture(t, namedConf, map[string]string{
			"broken.fwd": "192.0.2.90",
			"ok.fwd":     "192.0.2.100",
		})
		defer teardown()

		resp := queryUDP(t, udpAddr(srv), "example.com", dns.TypeA)
		assertNoError(t, resp)
		assertHasA(t, resp, "example.com", "192.0.2.100")
	})
}
