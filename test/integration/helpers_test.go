// Package integration_test contains end-to-end integration tests for ShadowDNS.
//
// Tests start a real server in-process using the production wiring
// (config.LoadNamedConf → buildServerState → server.NewServer → srv.Start),
// then exercise it with real DNS queries via miekg/dns.Client.
//
// All tests use the loopback source IP (127.0.0.1), which is matched by the
// view-other "any" rule. Per-view routing (view-th via GeoIP) is covered by
// unit tests in internal/server/server_test.go.
package integration_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// fixtureDir is the path to the shared testdata/integration directory.
const fixtureDir = "../../testdata/integration"

// testdataPlaceholder is the string in named.conf that must be replaced.
const testdataPlaceholder = "TESTDATA_DIR_PLACEHOLDER"

// ---------------------------------------------------------------------------
// Server lifecycle helpers
// ---------------------------------------------------------------------------

// newTestServer builds a full ShadowDNS server from the integration fixtures.
// It copies testdata/integration into a fresh TempDir, substitutes
// TESTDATA_DIR_PLACEHOLDER with the actual path, builds in-memory mmdb files,
// and starts the server.  Returns the server and a cancel func.
func newTestServer(t *testing.T) (*server.Server, func()) {
	t.Helper()
	return buildTestServer(t, nil)
}

// newTestServerWithEphemeral is newTestServer with an attached ephemeral
// store. The store is attached after Bind but before Serve starts, so the
// handler goroutine's reads of srv.EphemeralStore do not race this write.
func newTestServerWithEphemeral(t *testing.T, store *ephemeral.Store) (*server.Server, func()) {
	t.Helper()
	return buildTestServer(t, func(srv *server.Server) {
		srv.EphemeralStore = store
	})
}

func buildTestServer(t *testing.T, preServe func(*server.Server)) (*server.Server, func()) {
	t.Helper()

	tmpDir := t.TempDir()

	// Copy fixture tree into tmpDir.
	copyFixtures(t, tmpDir)

	// Write GeoIP mmdb files.
	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)

	// Patch named.conf (substitute TESTDATA_DIR_PLACEHOLDER → tmpDir).
	patchNamedConf(t, tmpDir)

	namedConf := filepath.Join(tmpDir, "named.conf")
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

	state, _, err := server.BuildState(cfg, aliases, nil, nil, nil, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		_ = country.Close()
		_ = asn.Close()
		t.Fatalf("server.BuildState: %v", err)
	}

	srv := server.NewServer(state, logger)

	_, srvCleanup := bindAndServe(t, srv, preServe)

	teardown := func() {
		srvCleanup()
		_ = country.Close()
		_ = asn.Close()
	}
	return srv, teardown
}

// bindAndServe binds srv to a loopback OS-assigned port, runs an optional
// preServe hook before Serve, starts Serve in a goroutine, and returns the
// UDP address plus a cleanup that cancels Serve and waits for it to exit.
// Shared by buildTestServer (fixture-driven) and tests that supply their own
// hand-built ServerState.
func bindAndServe(t *testing.T, srv *server.Server, preServe func(*server.Server)) (string, func()) {
	t.Helper()
	// UDPAddr() reads must not race the Serve goroutine writing s.listeners.
	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("srv.Bind: %v", err)
	}
	if preServe != nil {
		preServe(srv)
	}
	udpAddr := srv.UDPAddr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			t.Logf("server exited: %v", err)
		}
	}()
	return udpAddr, func() {
		cancel()
		<-done
	}
}

// udpAddr returns the string representation of the server's UDP listener.
func udpAddr(srv *server.Server) string {
	return srv.UDPAddr().String()
}

// tcpAddr returns the string representation of the server's TCP listener.
func tcpAddr(srv *server.Server) string {
	return srv.TCPAddr().String()
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

// queryUDP sends a DNS query over UDP and returns the response.
func queryUDP(t *testing.T, addr, qname string, qtype uint16) *dns.Msg {
	t.Helper()
	return dnsQuery(t, "udp", addr, qname, qtype)
}

// queryTCP sends a DNS query over TCP and returns the response.
func queryTCP(t *testing.T, addr, qname string, qtype uint16) *dns.Msg {
	t.Helper()
	return dnsQuery(t, "tcp", addr, qname, qtype)
}

func dnsQuery(t *testing.T, network, addr, qname string, qtype uint16) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: network, Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = false

	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("DNS query %s %s via %s to %s: %v", qname, dns.TypeToString[qtype], network, addr, err)
	}
	return resp
}

// assertNoError verifies RCODE=NOERROR.
func assertNoError(t *testing.T, resp *dns.Msg) {
	t.Helper()
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// assertAuthoritative verifies AA=1 and RA=0.
func assertAuthoritative(t *testing.T, resp *dns.Msg) {
	t.Helper()
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if resp.RecursionAvailable {
		t.Error("expected RA=0")
	}
}

// assertAnswerCount verifies the number of answer records.
func assertAnswerCount(t *testing.T, resp *dns.Msg, n int) {
	t.Helper()
	if len(resp.Answer) != n {
		t.Errorf("expected %d answer records, got %d", n, len(resp.Answer))
	}
}

// assertHasA verifies a specific A record is in the answer section.
func assertHasA(t *testing.T, resp *dns.Msg, owner, ip string) {
	t.Helper()
	want := net.ParseIP(ip).To4()
	for _, rr := range resp.Answer {
		if a, ok := rr.(*dns.A); ok {
			if strings.EqualFold(a.Hdr.Name, dns.Fqdn(owner)) && a.A.Equal(want) {
				return
			}
		}
	}
	t.Errorf("expected A %s → %s in answer; got: %v", owner, ip, resp.Answer)
}

// assertHasAAAA verifies a specific AAAA record is in the answer section.
func assertHasAAAA(t *testing.T, resp *dns.Msg, owner, ip string) {
	t.Helper()
	want := net.ParseIP(ip)
	for _, rr := range resp.Answer {
		if aaaa, ok := rr.(*dns.AAAA); ok {
			if strings.EqualFold(aaaa.Hdr.Name, dns.Fqdn(owner)) && aaaa.AAAA.Equal(want) {
				return
			}
		}
	}
	t.Errorf("expected AAAA %s → %s in answer; got: %v", owner, ip, resp.Answer)
}

// assertHasCNAME verifies a specific CNAME record in the answer section.
func assertHasCNAME(t *testing.T, resp *dns.Msg, owner, target string) {
	t.Helper()
	for _, rr := range resp.Answer {
		if c, ok := rr.(*dns.CNAME); ok {
			if strings.EqualFold(c.Hdr.Name, dns.Fqdn(owner)) &&
				strings.EqualFold(c.Target, dns.Fqdn(target)) {
				return
			}
		}
	}
	t.Errorf("expected CNAME %s → %s in answer; got: %v", owner, target, resp.Answer)
}

// assertAuthoritySOA verifies the authority section contains a SOA with the given owner.
func assertAuthoritySOA(t *testing.T, resp *dns.Msg, owner string) *dns.SOA {
	t.Helper()
	for _, rr := range resp.Ns {
		if soa, ok := rr.(*dns.SOA); ok {
			if strings.EqualFold(soa.Hdr.Name, dns.Fqdn(owner)) {
				return soa
			}
		}
	}
	t.Errorf("expected SOA for %s in authority section; got: %v", owner, resp.Ns)
	return nil
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// copyFixtures copies the entire testdata/integration tree into dstDir.
func copyFixtures(t *testing.T, dstDir string) {
	t.Helper()
	src, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("abs fixture dir: %v", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("readdir fixtures: %v", err)
	}

	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())

		if e.IsDir() {
			// Skip geoip/ — generated in-test from synthetic mmdb data.
			if e.Name() == "geoip" {
				continue
			}
			if err := os.CopyFS(dstPath, os.DirFS(srcPath)); err != nil {
				t.Fatalf("copyFS %s: %v", srcPath, err)
			}
		} else {
			copyFile(t, srcPath, dstPath)
		}
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// patchNamedConf replaces TESTDATA_DIR_PLACEHOLDER in named.conf with tmpDir
// and rewrites the included master.zones include path to use the absolute
// tmpDir path (so that relative includes also resolve correctly).
func patchNamedConf(t *testing.T, tmpDir string) {
	t.Helper()
	namedConf := filepath.Join(tmpDir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}

	// Substitute TESTDATA_DIR_PLACEHOLDER → tmpDir.
	patched := strings.ReplaceAll(string(data), testdataPlaceholder, tmpDir)

	// Replace the relative include "master.zones" with an absolute path.
	// The named.conf fixture has: include "master.zones";
	patched = strings.ReplaceAll(patched,
		`include "master.zones";`,
		`include "`+filepath.Join(tmpDir, "master.zones")+`";`)

	if err := os.WriteFile(namedConf, []byte(patched), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	// Patch master.zones: rewrite zone file paths to absolute paths under tmpDir.
	patchMasterZones(t, tmpDir)
}

// patchMasterZones rewrites each `file "master/..."` path in master.zones to
// the absolute tmpDir equivalent.
func patchMasterZones(t *testing.T, tmpDir string) {
	t.Helper()
	masterZones := filepath.Join(tmpDir, "master.zones")
	data, err := os.ReadFile(masterZones)
	if err != nil {
		t.Fatalf("read master.zones: %v", err)
	}

	// Replace relative file paths with absolute paths.
	patched := strings.ReplaceAll(string(data),
		`file "master/`,
		`file "`+filepath.Join(tmpDir, "master")+`/`)

	if err := os.WriteFile(masterZones, []byte(patched), 0o644); err != nil {
		t.Fatalf("write master.zones: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GeoIP mmdb builders
// ---------------------------------------------------------------------------

// buildIntegrationMMDBs creates the country and ASN mmdb files used by the
// integration tests.  Mappings:
//
//	Country: 192.0.2.0/24 → TH, 198.51.100.0/24 → JP
//	ASN:     203.0.113.0/24 → AS64500
func buildIntegrationMMDBs(t *testing.T, dir string) {
	t.Helper()
	buildCountryMMDB(t, filepath.Join(dir, "GeoLite2-Country.mmdb"))
	buildASNMMDB(t, filepath.Join(dir, "GeoLite2-ASN.mmdb"))
}

func buildCountryMMDB(t *testing.T, path string) {
	t.Helper()

	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create country mmdb writer: %v", err)
	}

	// Insert a no-op record for 0.0.0.0/0 so all other IPs return no-match
	// unless specifically overridden below.
	_, ipnet0, _ := net.ParseCIDR("0.0.0.0/0")
	if err := w.Insert(ipnet0, mmdbtype.Map{}); err != nil {
		t.Fatalf("insert default country record: %v", err)
	}

	// 192.0.2.0/24 → TH (matches view-th country rule).
	_, th, _ := net.ParseCIDR("192.0.2.0/24")
	thRecord := mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code": mmdbtype.String("TH"),
		},
	}
	if err := w.Insert(th, thRecord); err != nil {
		t.Fatalf("insert TH record: %v", err)
	}

	// 198.51.100.0/24 → JP (does NOT match view-th country rule).
	_, jp, _ := net.ParseCIDR("198.51.100.0/24")
	jpRecord := mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code": mmdbtype.String("JP"),
		},
	}
	if err := w.Insert(jp, jpRecord); err != nil {
		t.Fatalf("insert JP record: %v", err)
	}

	writeMMDB(t, w, path)
}

func buildASNMMDB(t *testing.T, path string) {
	t.Helper()

	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-ASN",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create ASN mmdb writer: %v", err)
	}

	// Default: no-record for all IPs.
	_, ipnet0, _ := net.ParseCIDR("0.0.0.0/0")
	if err := w.Insert(ipnet0, mmdbtype.Map{}); err != nil {
		t.Fatalf("insert default ASN record: %v", err)
	}

	// 203.0.113.0/24 → AS64500 (matches view-th asnum rule).
	_, asn64500, _ := net.ParseCIDR("203.0.113.0/24")
	asnRecord := mmdbtype.Map{
		"autonomous_system_number":       mmdbtype.Uint32(64500),
		"autonomous_system_organization": mmdbtype.String("AS64500 Test ASN"),
	}
	if err := w.Insert(asn64500, asnRecord); err != nil {
		t.Fatalf("insert ASN64500 record: %v", err)
	}

	writeMMDB(t, w, path)
}

func writeMMDB(t *testing.T, tree *mmdbwriter.Tree, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := tree.WriteTo(f); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
