// Integration tests covering the honor-listen-on change: the server must
// derive bind addresses from named.conf's listen-on directive when
// --listen does not carry a host component, and must continue serving
// queries even if a subset of the listen-on addresses fail to bind.
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// startServerWithListenOn copies the integration fixtures into a temp dir
// like newTestServer, but rewrites the options block so listen-on contains
// the caller-supplied token list. The caller also chooses --listen form,
// which selects the override vs listen-on branch. Returns the running
// server and a teardown func.
func startServerWithListenOn(t *testing.T, listenOnTokens, listenFlag string) (*server.Server, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	copyFixtures(t, tmpDir)

	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)
	patchNamedConf(t, tmpDir)

	namedConf := filepath.Join(tmpDir, "named.conf")

	// Rewrite the listen-on directive in the patched named.conf so tests can
	// exercise different token lists without touching the fixture source.
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	original := string(data)
	// The fixture uses `listen-on { any; };` — replace it with the test value.
	const anyListen = "listen-on { any; };"
	newListen := "listen-on " + listenOnTokens + ";"
	if !strings.Contains(original, anyListen) {
		t.Fatalf("fixture named.conf does not contain %q; update test", anyListen)
	}
	patched := strings.Replace(original, anyListen, newListen, 1)
	if err := os.WriteFile(namedConf, []byte(patched), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	logger := zap.NewNop()
	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("LoadNamedConf: %v", err)
	}
	sdCfg, err := shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}
	aliases := sdCfg.Aliases
	country, asn, err := view.LoadGeoIP(geoIPDir, logger)
	if err != nil {
		t.Fatalf("LoadGeoIP: %v", err)
	}
	state, _, err := server.BuildState(cfg, aliases, nil, nil, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		_ = country.Close()
		_ = asn.Close()
		t.Fatalf("BuildState: %v", err)
	}

	addrs, err := server.ResolveListenAddresses(listenFlag, cfg.Options.ListenOn, cfg.Options.ListenOnV6, logger)
	if err != nil {
		_ = country.Close()
		_ = asn.Close()
		t.Fatalf("ResolveListenAddresses: %v", err)
	}

	srv := server.NewServer(state, logger)
	if err := srv.BindMany(addrs); err != nil {
		_ = country.Close()
		_ = asn.Close()
		t.Fatalf("BindMany: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			t.Logf("server exited: %v", err)
		}
	}()

	teardown := func() {
		cancel()
		<-done
		_ = country.Close()
		_ = asn.Close()
	}
	return srv, teardown
}

// TestListenOn_BindsOnExplicitLoopbackAddress validates the end-to-end path:
// with named.conf listen-on set to { 127.0.0.1; } and --listen :0, the
// server binds on 127.0.0.1 (with an ephemeral port) and answers a query.
func TestListenOn_BindsOnExplicitLoopbackAddress(t *testing.T) {
	srv, teardown := startServerWithListenOn(t, `{ 127.0.0.1; }`, ":0")
	defer teardown()

	bound := srv.UDPAddr()
	if bound == nil {
		t.Fatal("no UDP address bound")
	}
	if !strings.HasPrefix(bound.String(), "127.0.0.1:") {
		t.Errorf("expected bind on 127.0.0.1, got %s", bound.String())
	}

	// Send a query to the bound address and verify we get a response from
	// the production handler wiring.
	resp := queryUDP(t, bound.String(), "example.com.", dns.TypeA)
	if resp == nil {
		t.Fatal("no DNS response")
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got rcode=%d", resp.Rcode)
	}
}

// TestListenOn_PartialFailStartsWithSurvivingAddresses exercises the
// graceful-bind-failure path end-to-end: listen-on includes a loopback
// address that is typically unreachable for binding (no interface alias)
// plus a working one, and the server must still start and serve queries
// on the working address. On macOS 127.0.0.0/8 is fully routable by
// default so we choose a less-common alias — but note that some CI
// environments may still let every 127.x.y.z succeed, in which case the
// test still passes (both bind, server starts). The important property
// is: if any one of them fails, the server still starts with the others.
func TestListenOn_PartialFailStartsWithSurvivingAddresses(t *testing.T) {
	// Use two loopback addresses. On Linux both usually work; on macOS
	// 127.0.0.2 typically also works. If both succeed, this is a stricter
	// superset of the "all bind" case and still valid. If one fails
	// (e.g. in a minimal container), the test verifies graceful-fail:
	// the server must still come up on at least one.
	srv, teardown := startServerWithListenOn(t, `{ 127.0.0.1; 127.0.0.2; }`, ":0")
	defer teardown()

	bound := srv.UDPAddrs()
	if len(bound) == 0 {
		t.Fatal("expected at least one bound UDP listener")
	}
	// At least one must be a 127.x.y.z address.
	found := false
	for _, a := range bound {
		if strings.HasPrefix(a.String(), "127.") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one loopback bound address, got %v", bound)
	}

	// Confirm queries flow to the first bound address.
	resp := queryUDP(t, bound[0].String(), "example.com.", dns.TypeA)
	if resp == nil {
		t.Fatal("no DNS response")
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got rcode=%d", resp.Rcode)
	}
}

// TestListenOn_OverrideBranchIgnoresListenOn: --listen carries a host,
// so listen-on in named.conf (even if unreachable) must be ignored.
// The server must bind exactly the --listen address.
func TestListenOn_OverrideBranchIgnoresListenOn(t *testing.T) {
	srv, teardown := startServerWithListenOn(t, `{ 10.255.255.255; }`, "127.0.0.1:0")
	defer teardown()

	bound := srv.UDPAddrs()
	if len(bound) != 1 {
		t.Fatalf("expected exactly 1 bound listener (override mode), got %d: %v", len(bound), bound)
	}
	if !strings.HasPrefix(bound[0].String(), "127.0.0.1:") {
		t.Errorf("override should bind 127.0.0.1, got %s", bound[0].String())
	}
}
