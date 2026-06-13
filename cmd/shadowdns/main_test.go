package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/logging"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// waitReady blocks until ch is closed (signalling that run()'s SIGHUP handler
// is attached) or 5s elapses — the deadline is an upper bound for genuine
// hangs, not a typical wait, since run() reaches signal.Notify in well under
// a second on CI.
func waitReady(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("server did not become ready in 5s")
	}
}

// newBufferLogger returns a zap logger writing console-formatted text to the
// given WriteSyncer. Tests assert on log content via strings.Contains.
func newBufferLogger(sink zapcore.WriteSyncer) *zap.Logger {
	cfg := logging.BaseEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder
	enc := zapcore.NewConsoleEncoder(cfg)
	return zap.New(zapcore.NewCore(enc, sink, zapcore.DebugLevel))
}

// binBuildDir holds binaries compiled once per `go test` invocation and
// shared across tests that need to exec the built CLI. A package-level temp
// dir is used instead of t.TempDir() because sync.Once's first caller would
// otherwise scope the path to its own test's cleanup.
var (
	binBuildDir string

	plainBinOnce sync.Once
	plainBinPath string
	plainBinErr  error

	versionedBinOnce sync.Once
	versionedBinPath string
	versionedBinErr  error
)

func TestMain(m *testing.M) {
	// Several tests send SIGHUP/SIGUSR1 to this very process (reload
	// integration, shutdown-order races, the reload subcommand, log reopen).
	// Each registers its own signal.Notify channel, but a kernel-pending
	// signal can be delivered AFTER that test's signal.Stop restored the
	// default disposition — which terminates the whole test binary
	// ("signal: hangup"). Keep one absorber channel registered for the
	// binary's lifetime so the default disposition is never restored;
	// per-test channels still receive their signals.
	sigAbsorber := make(chan os.Signal, 1)
	signal.Notify(sigAbsorber, syscall.SIGHUP, syscall.SIGUSR1)
	defer signal.Stop(sigAbsorber)

	dir, err := os.MkdirTemp("", "shadowdns-main-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create build temp dir:", err)
		os.Exit(1)
	}
	binBuildDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// buildPlainBinary compiles ./cmd/shadowdns once per test binary and returns
// the path. Tests that don't need a specific version string share this.
func buildPlainBinary(t *testing.T) string {
	t.Helper()
	plainBinOnce.Do(func() {
		bin := filepath.Join(binBuildDir, "shadowdns")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = "."
		if out, err := cmd.CombinedOutput(); err != nil {
			plainBinErr = fmt.Errorf("build failed: %w\n%s", err, out)
			return
		}
		plainBinPath = bin
	})
	if plainBinErr != nil {
		t.Fatalf("%v", plainBinErr)
	}
	return plainBinPath
}

// buildVersionedBinary compiles ./cmd/shadowdns once per test binary with
// `-X main.version=v1.2.3-test` so version-output tests share a single
// compile.
func buildVersionedBinary(t *testing.T) string {
	t.Helper()
	versionedBinOnce.Do(func() {
		bin := filepath.Join(binBuildDir, "shadowdns-v1.2.3-test")
		cmd := exec.Command("go", "build", "-ldflags", "-X main.version=v1.2.3-test", "-o", bin, ".")
		cmd.Dir = "."
		if out, err := cmd.CombinedOutput(); err != nil {
			versionedBinErr = fmt.Errorf("build failed: %w\n%s", err, out)
			return
		}
		versionedBinPath = bin
	})
	if versionedBinErr != nil {
		t.Fatalf("%v", versionedBinErr)
	}
	return versionedBinPath
}

func TestVersionVariable_HasDefault(t *testing.T) {
	if version == "" {
		t.Fatal("version variable should have a non-empty default value")
	}
}

func TestVersionFlag_PrintsVersion(t *testing.T) {
	binPath := buildVersionedBinary(t)

	out, err := exec.Command(binPath, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "v1.2.3-test" {
		t.Errorf("expected %q, got %q", "v1.2.3-test", got)
	}
}

// TestShortVersionFlag verifies that `-v` produces the same output as
// `--version`. This covers the "--version has a -v short alias" driver of the
// cobra migration.
func TestShortVersionFlag(t *testing.T) {
	binPath := buildVersionedBinary(t)

	longOut, err := exec.Command(binPath, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\n%s", err, longOut)
	}
	shortOut, err := exec.Command(binPath, "-v").CombinedOutput()
	if err != nil {
		t.Fatalf("-v failed: %v\n%s", err, shortOut)
	}
	if string(longOut) != string(shortOut) {
		t.Errorf("-v output %q should match --version output %q", shortOut, longOut)
	}
}

// TestHelpShowsCombinedVersionFlag verifies that `shadowdns --help` prints
// both short and long version flag names on the same line. This is the
// visible artefact of pflag's GNU-style flag rendering that motivated the
// cobra migration.
func TestHelpShowsCombinedVersionFlag(t *testing.T) {
	binPath := buildPlainBinary(t)

	out, err := exec.Command(binPath, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("--help failed: %v\n%s", err, out)
	}

	// pflag renders combined short/long flags as "  -v, --version".
	re := regexp.MustCompile(`(?m)^\s*-v,\s*--version\b`)
	if !re.Match(out) {
		t.Errorf("--help output should list `-v, --version` on a single line, got:\n%s", out)
	}
}

func TestStartupLog_IncludesVersion(t *testing.T) {
	dir := setupReloadTestDir(t)

	ctx, cancel := context.WithCancel(context.Background())
	// run() spawns SIGHUP/notify sub-goroutines that may outlive the
	// top-level srv.Serve(ctx) return; use the synchronized buffer.
	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	output := buf.String()
	if !strings.Contains(output, "version") {
		t.Errorf("startup log should contain version, got: %s", output)
	}
}

func TestRunRequiresNamedConfPath(t *testing.T) {
	logger := zap.NewNop()
	opts := runOptions{
		NamedConfPath: "",
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	err := run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error when NamedConfPath is empty")
	}
}

// TestRunLoadsAndShutsDownGracefully exercises the full run() pipeline:
// it builds a minimal but valid named.conf + zone file + GeoIP mmdbs in a
// temp dir, starts run() in a goroutine, then cancels ctx and verifies
// that run() returns within a reasonable timeout.
func TestRunLoadsAndShutsDownGracefully(t *testing.T) {
	dir := setupReloadTestDir(t)

	ctx, cancel := context.WithCancel(context.Background())
	logger := zap.NewNop()
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// Give run() time to load and start the listener.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s after context cancellation")
	}
}

const minimalZone = `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2024010101  ; serial
    3600
    600
    604800
    300
)

@   IN NS    ns1.example.com.
@   IN A     203.0.113.10
ns1 IN A     203.0.113.1
`

// updatedZone returns a zone file body with a bumped serial and the given A
// record IP, suitable for testing reload behavior.
func updatedZone(ip string) string {
	return `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2024010102  ; serial
    3600
    600
    604800
    300
)

@   IN NS    ns1.example.com.
@   IN A     ` + ip + `
ns1 IN A     203.0.113.1
`
}

// buildMMDBPair writes a GeoLite2 country and ASN mmdb into dir, each carrying
// the given record payload for cidr. Shared by the empty-fixture and
// data-bearing-fixture helpers.
func buildMMDBPair(t *testing.T, dir, cidr string, countryData, asnData mmdbtype.Map) {
	t.Helper()

	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("parse CIDR %q: %v", cidr, err)
	}

	country, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create country mmdb writer: %v", err)
	}
	if err := country.Insert(ipnet, countryData); err != nil {
		t.Fatalf("insert country record: %v", err)
	}
	writeMMDB(t, country, filepath.Join(dir, "GeoLite2-Country.mmdb"))

	asn, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-ASN",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create ASN mmdb writer: %v", err)
	}
	if err := asn.Insert(ipnet, asnData); err != nil {
		t.Fatalf("insert ASN record: %v", err)
	}
	writeMMDB(t, asn, filepath.Join(dir, "GeoLite2-ASN.mmdb"))
}

// buildEmptyMMDBs creates two minimal valid mmdb files in dir so that
// view.LoadGeoIP succeeds. The DBs contain no records — every IP lookup
// returns no-match, which is fine for tests that don't exercise GeoIP rules.
func buildEmptyMMDBs(t *testing.T, dir string) {
	t.Helper()
	buildMMDBPair(t, dir, "0.0.0.0/0", mmdbtype.Map{}, mmdbtype.Map{})
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

// ---------------------------------------------------------------------------
// reload tests
// ---------------------------------------------------------------------------

// TestReload_UpdatesServerState verifies that reload() re-reads config and
// zone files, then swaps the server state so new queries see updated data.
func TestReload_UpdatesServerState(t *testing.T) {
	dir := setupReloadTestDir(t)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	// Verify initial A record: 203.0.113.10.
	resp := reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "203.0.113.10")

	// Overwrite zone file with a new A record.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte(updatedZone("198.51.100.42")), 0o644); err != nil {
		t.Fatalf("rewrite zone: %v", err)
	}

	// Reload.
	if err := reload(context.Background(), opts, srv, geo, qlState, opts.Logger); err != nil {
		t.Fatalf("reload returned error: %v", err)
	}

	// Verify updated A record: 198.51.100.42.
	resp = reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "198.51.100.42")
}

// TestReload_FailurePreservesOldState verifies that when a reload fails (e.g.
// broken zone file), the server continues serving with the old state.
func TestReload_FailurePreservesOldState(t *testing.T) {
	dir := setupReloadTestDir(t)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	// Verify initial state works.
	resp := reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "203.0.113.10")

	// Break the zone file.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte("THIS IS NOT A VALID ZONE FILE"), 0o644); err != nil {
		t.Fatalf("break zone: %v", err)
	}

	// Reload should fail.
	if err := reload(context.Background(), opts, srv, geo, qlState, opts.Logger); err == nil {
		t.Fatal("expected reload to return an error for broken zone")
	}

	// Old state should still be in effect.
	resp = reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "203.0.113.10")
}

// ---------------------------------------------------------------------------
// reload test helpers
// ---------------------------------------------------------------------------

// writeMasterZonesFixture (re)writes the master.zones fixture with a single
// view named viewName whose match-clients body is matchClients. The view is
// declared on line 1 so error-location assertions can pin ":1".
func writeMasterZonesFixture(t *testing.T, dir, viewName, matchClients string) (masterZones string) {
	t.Helper()
	zoneFile := filepath.Join(dir, "example.com.zone")
	masterZones = filepath.Join(dir, "master.zones")
	masterZonesContent := `view "` + viewName + `" {
    match-clients { ` + matchClients + ` };
    recursion no;
    zone "example.com" {
        type master;
        file "` + zoneFile + `";
    };
};
`
	if err := os.WriteFile(masterZones, []byte(masterZonesContent), 0o644); err != nil {
		t.Fatalf("write master.zones: %v", err)
	}
	return masterZones
}

// writeNamedConfFixture (re)writes the named.conf fixture; geoipDirective is
// inserted verbatim into the options block ("" omits the option entirely).
func writeNamedConfFixture(t *testing.T, dir, masterZones, geoipDirective string) {
	t.Helper()
	namedConf := filepath.Join(dir, "named.conf")
	namedConfContent := `options {
    directory "` + dir + `";
    ` + geoipDirective + `
    listen-on { any; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(namedConfContent), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}
}

func setupReloadTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	geoIPDir := filepath.Join(dir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildEmptyMMDBs(t, geoIPDir)

	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte(minimalZone), 0o644); err != nil {
		t.Fatalf("write zone: %v", err)
	}

	masterZones := writeMasterZonesFixture(t, dir, "view-other", "any;")
	writeNamedConfFixture(t, dir, masterZones, `geoip-directory "`+geoIPDir+`";`)

	shadowConf := filepath.Join(dir, "shadowdns.yaml")
	if err := os.WriteFile(shadowConf, []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}

	return dir
}

func startReloadTestServer(t *testing.T, dir string) (*server.Server, *geoipRuntime, *atomic.Pointer[queryLogState], runOptions) {
	t.Helper()

	namedConf := filepath.Join(dir, "named.conf")
	logger := zap.NewNop()
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("load named.conf: %v", err)
	}

	aliases := config.AliasMap{}

	// The production conditional loader keeps this helper usable for both
	// geo and no-geo fixtures: nil handles when geoip-directory is unset.
	country, asn, err := loadGeoIPIfRequired(cfg, logger)
	if err != nil {
		t.Fatalf("load geoip: %v", err)
	}

	state, _, err := server.BuildState(cfg, aliases, nil, nil, nil, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		t.Fatalf("build state: %v", err)
	}

	srv := server.NewServer(state, logger)
	geo := &geoipRuntime{country: country, asn: asn}
	qlState := &atomic.Pointer[queryLogState]{}
	qlState.Store(&queryLogState{cfg: cfg.QueryLog})
	return srv, geo, qlState, opts
}

func reloadQuery(t *testing.T, srv *server.Server, qname string, qtype uint16) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = false

	rw := &testResponseWriter{}
	srv.ServeDNS(rw, m)
	if rw.msg == nil {
		t.Fatal("ServeDNS did not write a response")
	}
	return rw.msg
}

func requireARecord(t *testing.T, resp *dns.Msg, expectedIP string) {
	t.Helper()
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer is not A: %T", resp.Answer[0])
	}
	if a.A.String() != expectedIP {
		t.Errorf("expected %s, got %s", expectedIP, a.A.String())
	}
}

// TestReload_SuccessLogsCompletionMessage verifies that a successful reload
// emits both "reload initiated" and "reload complete" log messages.
func TestReload_SuccessLogsCompletionMessage(t *testing.T) {
	dir := setupReloadTestDir(t)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	// Create a separate logger backed by a buffer we can inspect.
	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	if err := reload(context.Background(), opts, srv, geo, qlState, logger); err != nil {
		t.Fatalf("reload returned unexpected error: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("reload initiated")) {
		t.Errorf("expected log to contain %q, got: %s", "reload initiated", output)
	}
	if !bytes.Contains([]byte(output), []byte("reload complete")) {
		t.Errorf("expected log to contain %q, got: %s", "reload complete", output)
	}
}

// TestReload_FailureLogsInitiatedOnly verifies that when reload fails (e.g.
// broken zone file), "reload initiated" is logged but "reload complete" is not.
func TestReload_FailureLogsInitiatedOnly(t *testing.T) {
	dir := setupReloadTestDir(t)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	// Break the zone file so reload will fail.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte("THIS IS NOT A VALID ZONE FILE"), 0o644); err != nil {
		t.Fatalf("break zone: %v", err)
	}

	// Create a separate logger backed by a buffer we can inspect.
	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	if err := reload(context.Background(), opts, srv, geo, qlState, logger); err == nil {
		t.Fatal("expected reload to return an error for broken zone")
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("reload initiated")) {
		t.Errorf("expected log to contain %q, got: %s", "reload initiated", output)
	}
	if bytes.Contains([]byte(output), []byte("reload complete")) {
		t.Errorf("expected log NOT to contain %q on failure, got: %s", "reload complete", output)
	}
}

// TestReload_DiffLog_ContainsVerifyModeAndCounts verifies that a successful
// reload emits an INFO log entry containing the verify mode, the number of
// zones that were reused (pointer unchanged), and the number that were
// re-parsed (file changed / first load), per Requirement: Reload diff logging.
func TestReload_DiffLog_ContainsVerifyModeAndCounts(t *testing.T) {
	dir := setupReloadTestDir(t)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	// Use hash mode (the default and safest mode).
	opts.ReloadVerify = server.VerifyModeHash

	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	if err := reload(context.Background(), opts, srv, geo, qlState, logger); err != nil {
		t.Fatalf("reload: %v", err)
	}

	output := buf.String()
	// The diff INFO log must include all three structured fields.
	for _, want := range []string{`"verify_mode"`, `"reused"`, `"reparsed"`} {
		if !strings.Contains(output, want) {
			t.Errorf("reload diff log missing field %q; full output:\n%s", want, output)
		}
	}
}

// TestReload_DiffLog_ReusedCountReflectsUnchangedZones verifies that when a
// zone file has not changed, the reused count equals the total loaded zone
// count and reparsed is 0.
func TestReload_DiffLog_ReusedCountReflectsUnchangedZones(t *testing.T) {
	dir := setupReloadTestDir(t)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	opts.ReloadVerify = server.VerifyModeHash

	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	// First reload: zone was just loaded at startup, fingerprints are set, so
	// the zone file is unchanged → reused=1, reparsed=0.
	if err := reload(context.Background(), opts, srv, geo, qlState, logger); err != nil {
		t.Fatalf("reload: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"reused": 1`) {
		t.Errorf("expected reused=1 (zone unchanged), got:\n%s", output)
	}
	if !strings.Contains(output, `"reparsed": 0`) {
		t.Errorf("expected reparsed=0 (no zone changed), got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// SIGHUP integration test
// ---------------------------------------------------------------------------

// TestSIGHUP_ReloadIntegration is an end-to-end test that starts the full
// run() pipeline, modifies a zone file on disk, sends SIGHUP to the process,
// and verifies that subsequent queries return the updated data.
func TestSIGHUP_ReloadIntegration(t *testing.T) {
	dir := setupReloadTestDir(t)

	// Find a free UDP port, then release it so run() can bind it.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	// Also release the TCP side of the same port.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = ln.Close()
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zap.NewNop()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    listenAddr,
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	waitReady(t, readyCh)

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("example.com.", dns.TypeA)

	// Verify initial state: 203.0.113.10 (from minimalZone).
	resp, _, err := c.Exchange(m, listenAddr)
	if err != nil {
		t.Fatalf("initial query: %v", err)
	}
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("initial query: rcode=%s answers=%d", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
	a := resp.Answer[0].(*dns.A)
	if a.A.String() != "203.0.113.10" {
		t.Fatalf("initial state: expected 203.0.113.10, got %s", a.A.String())
	}

	// Overwrite zone file with a new A record.
	if err := os.WriteFile(filepath.Join(dir, "example.com.zone"), []byte(updatedZone("198.51.100.99")), 0o644); err != nil {
		t.Fatalf("rewrite zone: %v", err)
	}

	// Send SIGHUP to trigger reload inside run().
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}

	// Wait for reload to complete.
	time.Sleep(200 * time.Millisecond)

	// Verify updated state: 198.51.100.99.
	resp, _, err = c.Exchange(m, listenAddr)
	if err != nil {
		t.Fatalf("post-reload query: %v", err)
	}
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("post-reload query: rcode=%s answers=%d", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
	a = resp.Answer[0].(*dns.A)
	if a.A.String() != "198.51.100.99" {
		t.Errorf("after SIGHUP: expected 198.51.100.99, got %s", a.A.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// ---------------------------------------------------------------------------
// PID file lifecycle tests
// ---------------------------------------------------------------------------

// TestPidFile_WrittenOnStartup verifies that run() writes a PID file when
// pid-file is configured, and removes it after shutdown.
func TestPidFile_WrittenOnStartup(t *testing.T) {
	dir := setupReloadTestDir(t)

	pidPath := filepath.Join(dir, "shadowdns.pid")
	patchNamedConfPidFile(t, dir, pidPath)

	// Find a free UDP port, then release it so run() can bind it.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	// Also release the TCP side of the same port.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = ln.Close()
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	logger := zap.NewNop()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    listenAddr,
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// run() writes the PID file before installing the SIGHUP handler, so by
	// the time readyCh closes the file is guaranteed to be on disk.
	waitReady(t, readyCh)

	// PID file must exist and contain a valid integer PID.
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("PID file not found after startup: %v", err)
	}
	pidStr := strings.TrimSpace(string(pidData))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		t.Fatalf("PID file does not contain a valid PID: %q", pidStr)
	}

	// Cancel the context and wait for run() to finish.
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s after context cancellation")
	}

	// PID file must be removed after shutdown.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected PID file to be removed after shutdown, stat err: %v", err)
	}
}

// TestPidFile_NotWrittenWhenEmpty verifies that run() does not create a PID
// file when pid-file is not configured in named.conf.
func TestPidFile_NotWrittenWhenEmpty(t *testing.T) {
	dir := setupReloadTestDir(t)

	// setupReloadTestDir does not include pid-file; define a path to check.
	pidPath := filepath.Join(dir, "shadowdns.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zap.NewNop()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// readyCh closes after run() has progressed past the PID-file branch, so
	// "no PID file present" is a stable assertion at this point.
	waitReady(t, readyCh)

	// PID file must NOT exist since pid-file was not configured.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected PID file to be absent when pid-file not configured, stat err: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// runReload CLI tests
// ---------------------------------------------------------------------------

// patchNamedConfPidFile rewrites the named.conf in dir to inject a pid-file
// directive pointing at pidPath. It replaces the first occurrence of
// "recursion no;" so the result is valid and self-consistent.
func patchNamedConfPidFile(t *testing.T, dir, pidPath string) {
	t.Helper()
	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	patched := strings.Replace(
		string(data),
		"recursion no;",
		fmt.Sprintf("pid-file %q;\n    recursion no;", pidPath),
		1,
	)
	if err := os.WriteFile(namedConf, []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched named.conf: %v", err)
	}
}

// TestRunReload_Success verifies that runReload returns nil when named.conf
// contains a valid pid-file option and the PID file holds the current PID.
// We register a signal.Notify channel for SIGHUP before calling runReload so
// the signal sent to the test process is absorbed rather than terminating it.
func TestRunReload_Success(t *testing.T) {
	dir := setupReloadTestDir(t)

	pidPath := filepath.Join(dir, "shadowdns.pid")
	patchNamedConfPidFile(t, dir, pidPath)

	// Write the current process PID so runReload can find it.
	if err := os.WriteFile(pidPath, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	// Install a SIGHUP handler to absorb the signal that runReload will send.
	// Without this the default action (process termination) would kill the test.
	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)
	defer signal.Stop(sighupCh)

	if err := runReload(filepath.Join(dir, "named.conf"), zap.NewNop()); err != nil {
		t.Fatalf("runReload returned unexpected error: %v", err)
	}
}

// TestRunReload_MissingNamedConf verifies that runReload returns an error
// containing "--named-conf is required" when the path is empty.
func TestRunReload_MissingNamedConf(t *testing.T) {
	err := runReload("", zap.NewNop())
	if err == nil {
		t.Fatal("expected error when named-conf path is empty")
	}
	if !strings.Contains(err.Error(), "--named-conf is required") {
		t.Errorf("expected error to contain %q, got: %v", "--named-conf is required", err)
	}
}

// TestRunReload_NoPidFileConfigured verifies that runReload returns an error
// mentioning "pid-file" when named.conf has no pid-file option.
func TestRunReload_NoPidFileConfigured(t *testing.T) {
	dir := setupReloadTestDir(t)

	err := runReload(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err == nil {
		t.Fatal("expected error when pid-file is not configured")
	}
	if !strings.Contains(err.Error(), "pid-file") {
		t.Errorf("expected error to contain %q, got: %v", "pid-file", err)
	}
}

// TestRunReload_PidFileNotFound verifies that runReload returns an error when
// the pid-file path in named.conf does not exist on disk.
func TestRunReload_PidFileNotFound(t *testing.T) {
	dir := setupReloadTestDir(t)

	// Point to a path that will never exist.
	patchNamedConfPidFile(t, dir, "/nonexistent/path/pid")

	err := runReload(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err == nil {
		t.Fatal("expected error when pid file does not exist")
	}
}

// TestRunReload_InvalidPidContent verifies that runReload returns an error
// when the pid-file exists but contains non-numeric content.
func TestRunReload_InvalidPidContent(t *testing.T) {
	dir := setupReloadTestDir(t)

	pidPath := filepath.Join(dir, "shadowdns.pid")
	patchNamedConfPidFile(t, dir, pidPath)

	// Write garbage content — not a valid PID integer.
	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	err := runReload(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err == nil {
		t.Fatal("expected error when pid file contains non-numeric content")
	}
}

// TestRunReload_ProcessNotRunning verifies that runReload returns an error
// when the PID file contains a valid integer but no process with that PID exists.
func TestRunReload_ProcessNotRunning(t *testing.T) {
	dir := setupReloadTestDir(t)

	pidPath := filepath.Join(dir, "shadowdns.pid")
	patchNamedConfPidFile(t, dir, pidPath)

	// Write a PID that is almost certainly not in use.
	if err := os.WriteFile(pidPath, []byte("999999999\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	err := runReload(filepath.Join(dir, "named.conf"), zap.NewNop())
	if err == nil {
		t.Fatal("expected error when PID does not correspond to a running process")
	}
}

// ---------------------------------------------------------------------------
// run() NOTIFY guard tests
// ---------------------------------------------------------------------------

// patchNamedConfNotify injects a `notify yes|no;` directive into the options
// block of the named.conf in dir. `value` MUST be "yes" or "no". Called from
// tests that exercise the options.notify path.
func patchNamedConfNotify(t *testing.T, dir, value string) {
	t.Helper()
	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	patched := strings.Replace(
		string(data),
		"recursion no;",
		"notify "+value+";\n    recursion no;",
		1,
	)
	if err := os.WriteFile(namedConf, []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched named.conf: %v", err)
	}
}

// runRunAndCaptureLogs starts run() in a goroutine with a captured logger,
// polls for the "notify state resolved" log line to appear, then cancels
// context and waits for run() to return. Returns the captured log output.
// Polling (rather than a fixed sleep) keeps the test fast locally and
// resilient on slow CI runners.
func runRunAndCaptureLogs(t *testing.T, dir string, explicitNoNotify bool) string {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath:    filepath.Join(dir, "named.conf"),
		ConfigPath:       filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:       "127.0.0.1:0",
		Logger:           logger,
		NoNotifyExplicit: explicitNoNotify,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "notify state resolved") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s after context cancellation")
	}
	return buf.String()
}

// threadSafeBuffer wraps bytes.Buffer with a mutex so a logger writing from
// run()'s goroutine and the test's polling reader can share it safely.
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRun_NotifyGuard_FlagExplicit verifies that when --no-notify is set,
// run() resolves notify=false with source=flag regardless of what config says.
// Config is set to notify yes; explicit flag MUST win.
func TestRun_NotifyGuard_FlagExplicit(t *testing.T) {
	dir := setupReloadTestDir(t)
	patchNamedConfNotify(t, dir, "yes")

	output := runRunAndCaptureLogs(t, dir, true)

	if !strings.Contains(output, "notify state resolved") {
		t.Fatalf("expected notify-state log, got: %s", output)
	}
	if !strings.Contains(output, `"enabled": false`) {
		t.Errorf("expected enabled=false (flag overrides config yes), got: %s", output)
	}
	if !strings.Contains(output, `"source": "flag"`) {
		t.Errorf("expected source=flag, got: %s", output)
	}
}

// TestRun_NotifyGuard_ConfigYes verifies that `options { notify yes; };` with
// no CLI flag resolves to enabled=true source=config.
func TestRun_NotifyGuard_ConfigYes(t *testing.T) {
	dir := setupReloadTestDir(t)
	patchNamedConfNotify(t, dir, "yes")

	output := runRunAndCaptureLogs(t, dir, false)

	if !strings.Contains(output, `"enabled": true`) {
		t.Errorf("expected enabled=true for `notify yes;`, got: %s", output)
	}
	if !strings.Contains(output, `"source": "config"`) {
		t.Errorf("expected source=config, got: %s", output)
	}
}

// TestRun_NotifyGuard_ConfigNo verifies that `options { notify no; };` with
// no CLI flag resolves to enabled=false source=config.
func TestRun_NotifyGuard_ConfigNo(t *testing.T) {
	dir := setupReloadTestDir(t)
	patchNamedConfNotify(t, dir, "no")

	output := runRunAndCaptureLogs(t, dir, false)

	if !strings.Contains(output, `"enabled": false`) {
		t.Errorf("expected enabled=false for `notify no;`, got: %s", output)
	}
	if !strings.Contains(output, `"source": "config"`) {
		t.Errorf("expected source=config, got: %s", output)
	}
}

// TestRun_NotifyGuard_Default verifies that with neither --no-notify nor a
// notify directive in config, run() resolves to enabled=true source=default
// (preserving pre-change behavior).
func TestRun_NotifyGuard_Default(t *testing.T) {
	dir := setupReloadTestDir(t) // setupReloadTestDir does not set notify.

	output := runRunAndCaptureLogs(t, dir, false)

	if !strings.Contains(output, `"enabled": true`) {
		t.Errorf("expected enabled=true by default, got: %s", output)
	}
	if !strings.Contains(output, `"source": "default"`) {
		t.Errorf("expected source=default, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// resolveNotifyEnabled precedence tests
// ---------------------------------------------------------------------------

// TestResolveNotifyEnabled_AllCombinations covers every (flag × config) input
// combination defined by the precedence rule: explicit flag > config > default.
//
// The 6 rows are:
//
//	flag explicit=true,  config=nil    → false (flag wins)
//	flag explicit=true,  config=&true  → false (flag overrides config yes)
//	flag explicit=true,  config=&false → false (flag and config agree)
//	flag explicit=false, config=nil    → true  (default)
//	flag explicit=false, config=&true  → true  (config yes)
//	flag explicit=false, config=&false → false (config no)
func TestResolveNotifyEnabled_AllCombinations(t *testing.T) {
	truePtr := func(b bool) *bool { return &b }

	tests := []struct {
		name       string
		explicit   bool
		config     *bool
		wantEnable bool
		wantSource string
	}{
		{"flag-explicit / config-nil", true, nil, false, "flag"},
		{"flag-explicit / config-true", true, truePtr(true), false, "flag"},
		{"flag-explicit / config-false", true, truePtr(false), false, "flag"},
		{"flag-absent / config-nil", false, nil, true, "default"},
		{"flag-absent / config-true", false, truePtr(true), true, "config"},
		{"flag-absent / config-false", false, truePtr(false), false, "config"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enabled, source := resolveNotifyEnabled(tc.explicit, tc.config)
			if enabled != tc.wantEnable {
				t.Errorf("enabled: got %v, want %v", enabled, tc.wantEnable)
			}
			if source != tc.wantSource {
				t.Errorf("source: got %q, want %q", source, tc.wantSource)
			}
		})
	}
}

// testResponseWriter is a minimal dns.ResponseWriter for in-process testing.
type testResponseWriter struct {
	msg *dns.Msg
}

func (w *testResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}
}
func (w *testResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}
func (w *testResponseWriter) WriteMsg(msg *dns.Msg) error { w.msg = msg; return nil }
func (w *testResponseWriter) Write([]byte) (int, error)   { return 0, nil }
func (w *testResponseWriter) Close() error                { return nil }
func (w *testResponseWriter) TsigStatus() error           { return nil }
func (w *testResponseWriter) TsigTimersOnly(bool)         {}

// ---------------------------------------------------------------------------
// --reload-verify flag tests
// ---------------------------------------------------------------------------

// TestParseVerifyMode_AcceptsValidValues verifies that parseVerifyMode returns
// the expected server.VerifyMode for each accepted string value.
func TestParseVerifyMode_AcceptsValidValues(t *testing.T) {
	tests := []struct {
		input string
		want  server.VerifyMode
	}{
		{"hash", server.VerifyModeHash},
		{"size", server.VerifyModeSize},
		{"none", server.VerifyModeNone},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseVerifyMode(tc.input)
			if err != nil {
				t.Fatalf("parseVerifyMode(%q) returned unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseVerifyMode(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseVerifyMode_RejectsInvalidValue verifies that parseVerifyMode returns
// a non-nil error for unrecognized values.
func TestParseVerifyMode_RejectsInvalidValue(t *testing.T) {
	for _, bad := range []string{"foo", "Hash", "HASH", "sha256", ""} {
		t.Run(bad, func(t *testing.T) {
			_, err := parseVerifyMode(bad)
			if err == nil {
				t.Errorf("parseVerifyMode(%q) expected error, got nil", bad)
			}
		})
	}
}

// TestReloadVerifyFlag_DefaultIsHash verifies that the runOptions zero value
// carries VerifyModeHash as the reload-verify default.
func TestReloadVerifyFlag_DefaultIsHash(t *testing.T) {
	var opts runOptions
	if opts.ReloadVerify != server.VerifyModeHash {
		t.Errorf("default ReloadVerify = %v, want VerifyModeHash", opts.ReloadVerify)
	}
}

// TestReloadVerifyFlag_InvalidExitsNonZero builds the binary and runs it with
// an invalid --reload-verify value, expecting a non-zero exit code.
func TestReloadVerifyFlag_InvalidExitsNonZero(t *testing.T) {
	binPath := buildPlainBinary(t)

	out, err := exec.Command(binPath, "--named-conf", "/dev/null", "--reload-verify", "bogus").CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid --reload-verify value, got exit 0")
	}
	if !strings.Contains(string(out), "bogus") {
		t.Errorf("expected error output to mention the invalid value %q, got: %s", "bogus", out)
	}
}

// TestReloadSubcommand verifies the end-to-end behavior of the `reload`
// subcommand invoked via the built binary: a successful SIGHUP send when
// pid-file is configured and points at the current process, and a non-zero
// exit when named.conf does not configure pid-file. This covers the
// "No pid-file configured in named.conf" scenario from the pid-file spec.
func TestReloadSubcommand(t *testing.T) {
	binPath := buildPlainBinary(t)

	t.Run("success with pid-file", func(t *testing.T) {
		dir := setupReloadTestDir(t)
		pidPath := filepath.Join(dir, "shadowdns.pid")
		patchNamedConfPidFile(t, dir, pidPath)

		// Absorb SIGHUP so the child-sent signal does not terminate this test.
		sighupCh := make(chan os.Signal, 1)
		signal.Notify(sighupCh, syscall.SIGHUP)
		defer signal.Stop(sighupCh)

		if err := os.WriteFile(pidPath, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644); err != nil {
			t.Fatalf("write pid file: %v", err)
		}

		out, err := exec.Command(binPath, "reload", "--named-conf", filepath.Join(dir, "named.conf")).CombinedOutput()
		if err != nil {
			t.Fatalf("reload subcommand failed: %v\n%s", err, out)
		}
	})

	t.Run("no pid-file configured", func(t *testing.T) {
		dir := setupReloadTestDir(t) // setupReloadTestDir does not include pid-file

		out, err := exec.Command(binPath, "reload", "--named-conf", filepath.Join(dir, "named.conf")).CombinedOutput()
		if err == nil {
			t.Fatalf("reload without pid-file should exit non-zero, got success\n%s", out)
		}
		if !strings.Contains(string(out), "pid-file") {
			t.Errorf("expected error output to mention pid-file, got: %s", out)
		}
	})

	t.Run("missing --named-conf flag", func(t *testing.T) {
		out, err := exec.Command(binPath, "reload").CombinedOutput()
		if err == nil {
			t.Fatalf("reload without --named-conf should exit non-zero, got success\n%s", out)
		}
		if !strings.Contains(string(out), "named-conf") {
			t.Errorf("expected error output to mention named-conf, got: %s", out)
		}
	})
}
func (w *testResponseWriter) Hijack() {}

// ---------------------------------------------------------------------------
// --log-file flag and SIGUSR1 reopen
// ---------------------------------------------------------------------------

// Requirement: Daemon SHALL support file-backed log output — the
// `--log-file` flag MUST be registered on the root command and its
// runtime value MUST flow into runOptions.LogFile.
func TestLogFileFlag_RegisteredAndBound(t *testing.T) {
	cmd := newRootCmd()
	flag := cmd.Flags().Lookup("log-file")
	if flag == nil {
		t.Fatal("--log-file flag not registered on root command")
	}
	if flag.DefValue != "" {
		t.Errorf("--log-file default = %q, want empty string (stderr mode)", flag.DefValue)
	}

	if reloadCmd, _, err := cmd.Find([]string{"reload"}); err == nil {
		if reloadCmd.Flags().Lookup("log-file") != nil {
			t.Error("reload subcommand unexpectedly has --log-file flag")
		}
	}
	if pruneCmd, _, err := cmd.Find([]string{"prune-backup"}); err == nil {
		if pruneCmd.Flags().Lookup("log-file") != nil {
			t.Error("prune-backup subcommand unexpectedly has --log-file flag")
		}
	}
}

// inodeOf returns the inode number for path. Test fails if stat fails.
func inode(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat sys is not *syscall.Stat_t: %T", st.Sys())
	}
	return sys.Ino
}

// startServerWithLogFile boots run() with LogFile set so SIGUSR1 wires up,
// returning the cancel func, the file path, and a ready channel proving
// the SIGHUP / SIGUSR1 handlers are attached.
func startServerWithLogFile(t *testing.T, dir, logPath string) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()
	if ln, lerr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); lerr == nil {
		_ = ln.Close()
	}

	logger, reopener, lerr := logging.New(logging.Options{
		Level:   zapcore.InfoLevel,
		LogFile: logPath,
	})
	if lerr != nil {
		t.Fatalf("logging.New: %v", lerr)
	}

	ctx, cancelFn := context.WithCancel(context.Background())
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", port),
		Logger:        logger,
		LogReopener:   reopener,
		ReadyCh:       readyCh,
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- run(ctx, opts) }()
	waitReady(t, readyCh)
	return cancelFn, doneCh
}

// Requirement: Daemon SHALL reopen log file on SIGUSR1 — after rename,
// SIGUSR1 makes the daemon write to a new inode at the original path.
func TestSIGUSR1_ReopensLogFileAfterRename(t *testing.T) {
	dir := setupReloadTestDir(t)
	logPath := filepath.Join(t.TempDir(), "shadowdns.log")

	cancel, done := startServerWithLogFile(t, dir, logPath)
	defer cancel()

	// At this point the server has emitted at least the "shadowdns ready"
	// log line through the file sink.
	originalInode := inode(t, logPath)

	rotated := logPath + ".1"
	if err := os.Rename(logPath, rotated); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}

	// Wait for the SIGUSR1 handler goroutine to drain the channel and
	// emit "log file reopened" through the new fd.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(logPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	newInode := inode(t, logPath)
	if newInode == originalInode {
		t.Fatalf("expected new inode after SIGUSR1, got same %d", newInode)
	}

	// The reopen-confirmation INFO log should appear in the new file.
	deadline = time.Now().Add(2 * time.Second)
	var newContent []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(b), "log file reopened") {
			newContent = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(newContent), "log file reopened") {
		t.Fatalf("new file missing reopen confirmation log; got %q", string(newContent))
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// Requirement: Daemon SHALL reopen log file on SIGUSR1 — when reopen
// fails (parent dir gone), the previous fd is preserved and an error
// log appears through it.
func TestSIGUSR1_ReopenFailurePreservesFd(t *testing.T) {
	dir := setupReloadTestDir(t)
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "shadowdns.log")

	cancel, done := startServerWithLogFile(t, dir, logPath)
	defer cancel()

	// Capture the original fd's identity by reading current inode.
	originalInode := inode(t, logPath)

	// Remove the parent directory entirely; reopen will now fail.
	if err := os.RemoveAll(logDir); err != nil {
		t.Fatalf("removeall: %v", err)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}

	// Give the handler a moment to run. We can't read the file (it's
	// gone), but we verify the daemon is still alive and shutdown
	// cleanly — meaning the fd survived and no panic propagated.
	time.Sleep(200 * time.Millisecond)

	// As a sanity check the inode number of the deleted file path is
	// still observable through proc only on Linux; the simpler proxy is
	// "the daemon did not crash". Re-create directory + file and confirm
	// daemon will write to the *original* fd (which is dangling) — we
	// cannot easily observe that without /proc, so instead re-rotate
	// and confirm a follow-up SIGUSR1 with the dir back can recover.
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		t.Fatalf("recreate dir: %v", err)
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1 (recovery): %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(logPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("recovery reopen did not recreate %s: %v", logPath, err)
	}
	recoveredInode := inode(t, logPath)
	if recoveredInode == originalInode {
		t.Fatalf("recovery reopen returned same inode %d; expected fresh fd", recoveredInode)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// Requirement: SIGHUP SHALL NOT trigger log reopen — sending SIGHUP must
// leave the log file inode unchanged (zone reload runs but log fd
// untouched).
func TestSIGHUP_DoesNotReopenLogFile(t *testing.T) {
	dir := setupReloadTestDir(t)
	logPath := filepath.Join(t.TempDir(), "shadowdns.log")

	cancel, done := startServerWithLogFile(t, dir, logPath)
	defer cancel()

	originalInode := inode(t, logPath)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}

	// Wait a bit for reload to run.
	time.Sleep(300 * time.Millisecond)

	postInode := inode(t, logPath)
	if postInode != originalInode {
		t.Fatalf("SIGHUP changed log inode from %d to %d; SIGHUP must not reopen log file", originalInode, postInode)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// --------------------------------------------------------------------------
// dispatchNotifies tests
// --------------------------------------------------------------------------

// makeRootZoneWithGlue builds a *zone.Zone whose origin has the given NS
// records, with optional A/AAAA glue keyed by NS hostname. glueA / glueAAAA
// values are dotted-quad / IPv6 string literals respectively.
func makeRootZoneWithGlue(origin, mname string, nsTargets []string,
	glueA, glueAAAA map[string][]string) *zone.Zone {
	z := &zone.Zone{Origin: origin}
	z.AddRR(&dns.SOA{
		Hdr: dns.RR_Header{
			Name:   origin,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Ns:     mname,
		Mbox:   "hostmaster." + origin,
		Serial: 1,
	})
	for _, ns := range nsTargets {
		z.AddRR(&dns.NS{
			Hdr: dns.RR_Header{
				Name:   origin,
				Rrtype: dns.TypeNS,
				Class:  dns.ClassINET,
				Ttl:    3600,
			},
			Ns: ns,
		})
	}
	for host, ips := range glueA {
		for _, ip := range ips {
			z.AddRR(&dns.A{
				Hdr: dns.RR_Header{
					Name:   host,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    3600,
				},
				A: net.ParseIP(ip).To4(),
			})
		}
	}
	for host, ips := range glueAAAA {
		for _, ip := range ips {
			z.AddRR(&dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   host,
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    3600,
				},
				AAAA: net.ParseIP(ip),
			})
		}
	}
	return z
}

// notifyStub captures each notifySendFn invocation and signals once `want`
// calls have been observed. Tests obtain the stub function via fn() and
// install it onto notifySendFn (use installNotifyStub to restore the
// original on cleanup).
type notifyStub struct {
	mu       sync.Mutex
	calls    []struct{ origin, addr string }
	done     chan struct{}
	doneOnce sync.Once
	want     int
	err      error // if non-nil, the stub returns this for every call
}

func newNotifyStub(want int) *notifyStub {
	return &notifyStub{done: make(chan struct{}), want: want}
}

func (s *notifyStub) fn() func(context.Context, string, string, *zap.Logger) error {
	return func(_ context.Context, origin, addr string, _ *zap.Logger) error {
		s.mu.Lock()
		s.calls = append(s.calls, struct{ origin, addr string }{origin, addr})
		reached := s.want > 0 && len(s.calls) >= s.want
		s.mu.Unlock()
		if reached {
			s.doneOnce.Do(func() { close(s.done) })
		}
		return s.err
	}
}

func (s *notifyStub) waitForCalls(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(timeout):
		s.mu.Lock()
		got := len(s.calls)
		s.mu.Unlock()
		t.Fatalf("timed out waiting for %d notifySendFn calls; got %d", s.want, got)
	}
}

func (s *notifyStub) addrs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	for i, c := range s.calls {
		out[i] = c.addr
	}
	return out
}

func installNotifyStub(t *testing.T, fn func(context.Context, string, string, *zap.Logger) error) {
	t.Helper()
	orig := notifySendFn
	notifySendFn = fn
	t.Cleanup(func() { notifySendFn = orig })
}

// observedFields converts the field slice of a single observed log entry
// into a key→string map for easier assertions in tests.
func observedFields(entry observer.LoggedEntry) map[string]string {
	out := make(map[string]string, len(entry.Context))
	for _, f := range entry.Context {
		if f.String != "" {
			out[f.Key] = f.String
		} else {
			out[f.Key] = fmt.Sprint(f.Interface)
		}
	}
	return out
}

// NS target with no in-zone glue causes dispatchNotifies to skip — no
// goroutine is spawned, no notifySendFn call, and a debug log records
// source="skipped-no-glue".
func TestDispatchNotifies_NoGlue_SkippedNoGoroutine(t *testing.T) {
	var calls atomic.Int32
	installNotifyStub(t, func(context.Context, string, string, *zap.Logger) error {
		calls.Add(1)
		return nil
	})

	core, obs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	z := makeRootZoneWithGlue("example.com.", "ns1.example.com.",
		[]string{"ns.elsewhere.test."}, nil, nil)
	rootZones := map[string]map[string]*zone.Zone{
		"default": {"example.com.": z},
	}

	dispatchNotifies(context.Background(), rootZones, logger)

	// Give any (erroneously) spawned goroutine time to run before asserting
	// that none did.
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("notifySendFn called %d times for no-glue target; want 0", got)
	}

	skips := obs.FilterMessage("NOTIFY skipped: no in-zone glue").All()
	if len(skips) != 1 {
		t.Fatalf("expected 1 skip log, got %d (all entries: %+v)",
			len(skips), obs.All())
	}
	f := observedFields(skips[0])
	if f["zone"] != "example.com." {
		t.Errorf("zone field: want example.com., got %q", f["zone"])
	}
	if f["target"] != "ns.elsewhere.test." {
		t.Errorf("target field: want ns.elsewhere.test., got %q", f["target"])
	}
	if f["source"] != "skipped-no-glue" {
		t.Errorf("source field: want skipped-no-glue, got %q", f["source"])
	}
}

// NS target with two glue IPs (A + AAAA) produces exactly two
// notifySendFn invocations, one per IP, each on host:53.
func TestDispatchNotifies_MultiIPGlue_OneGoroutinePerIP(t *testing.T) {
	stub := newNotifyStub(2)
	installNotifyStub(t, stub.fn())

	logger := zap.NewNop()
	z := makeRootZoneWithGlue("example.com.", "ns1.example.com.",
		[]string{"ns21.example.com."},
		map[string][]string{"ns21.example.com.": {"10.0.0.21"}},
		map[string][]string{"ns21.example.com.": {"2001:db8::21"}})
	rootZones := map[string]map[string]*zone.Zone{
		"default": {"example.com.": z},
	}

	dispatchNotifies(context.Background(), rootZones, logger)
	stub.waitForCalls(t, 2*time.Second)

	got := stub.addrs()
	wantSet := map[string]bool{
		net.JoinHostPort("10.0.0.21", "53"):    true,
		net.JoinHostPort("2001:db8::21", "53"): true,
	}
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	for _, addr := range got {
		if !wantSet[addr] {
			t.Errorf("unexpected address: %q (want one of %v)", addr, wantSet)
		}
		delete(wantSet, addr)
	}
	if len(wantSet) != 0 {
		t.Errorf("missing expected addresses: %v", wantSet)
	}
}

// When notifySendFn errors, the final "NOTIFY failed" warn carries
// source="glue" so operators can distinguish glue-driven sends from a
// future also-notify path.
func TestDispatchNotifies_FailureLogCarriesSourceGlue(t *testing.T) {
	stub := newNotifyStub(1)
	stub.err = fmt.Errorf("stubbed transport failure")
	installNotifyStub(t, stub.fn())

	core, obs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	z := makeRootZoneWithGlue("example.com.", "ns1.example.com.",
		[]string{"ns2.example.com."},
		map[string][]string{"ns2.example.com.": {"10.0.0.2"}}, nil)
	rootZones := map[string]map[string]*zone.Zone{
		"default": {"example.com.": z},
	}

	dispatchNotifies(context.Background(), rootZones, logger)
	stub.waitForCalls(t, 2*time.Second)

	// stub.waitForCalls only confirms notifySendFn returned; the goroutine
	// then synchronously enters the Warnw branch. A short grace window is
	// enough for the log to land in the observer.
	time.Sleep(20 * time.Millisecond)

	fails := obs.FilterMessage("NOTIFY failed").All()
	if len(fails) < 1 {
		t.Fatalf("expected at least 1 'NOTIFY failed' log, got 0 (all: %+v)", obs.All())
	}
	f := observedFields(fails[0])
	if f["source"] != "glue" {
		t.Errorf("source: want glue, got %q (fields: %v)", f["source"], f)
	}
	if f["ip"] != "10.0.0.2" {
		t.Errorf("ip: want 10.0.0.2, got %q", f["ip"])
	}
}

// Same (zone, NS host, glue IP) tuple loaded under two views is
// deduplicated — exactly one NOTIFY send sequence.
func TestDispatchNotifies_CrossViewDedup(t *testing.T) {
	stub := newNotifyStub(1)
	installNotifyStub(t, stub.fn())

	logger := zap.NewNop()
	makeZ := func() *zone.Zone {
		return makeRootZoneWithGlue("example.com.", "ns1.example.com.",
			[]string{"ns2.example.com."},
			map[string][]string{"ns2.example.com.": {"10.0.0.2"}}, nil)
	}
	rootZones := map[string]map[string]*zone.Zone{
		"viewA": {"example.com.": makeZ()},
		"viewB": {"example.com.": makeZ()},
	}

	dispatchNotifies(context.Background(), rootZones, logger)
	stub.waitForCalls(t, 2*time.Second)

	// After done closes, grant a small grace window for any second goroutine
	// to (incorrectly) fire before asserting count.
	time.Sleep(50 * time.Millisecond)

	addrs := stub.addrs()
	if len(addrs) != 1 {
		t.Fatalf("cross-view dedup failed: want 1 call, got %d: %v", len(addrs), addrs)
	}
	want := net.JoinHostPort("10.0.0.2", "53")
	if addrs[0] != want {
		t.Errorf("addr: want %q, got %q", want, addrs[0])
	}
}

// ---------------------------------------------------------------------------
// --ecs-enable flag
// ---------------------------------------------------------------------------

// Requirement: ECS support is disabled by default and gated by the
// --ecs-enable flag — the flag MUST be registered on the root command with
// default false.
func TestECSEnableFlag_RegisteredWithDefaultFalse(t *testing.T) {
	cmd := newRootCmd()
	flag := cmd.Flags().Lookup("ecs-enable")
	if flag == nil {
		t.Fatal("--ecs-enable flag not registered on root command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--ecs-enable default = %q, want %q", flag.DefValue, "false")
	}
}

// ---------------------------------------------------------------------------
// conditional GeoIP loading tests (geoip-optional)
// ---------------------------------------------------------------------------

// setupGeoIPOptionalTestDir builds a minimal config dir whose named.conf has
// no geoip-directory unless geoipDirective is non-empty (the directive line is
// inserted verbatim, e.g. `geoip-directory "";`). matchClients is the body of
// the single view's match-clients block. No mmdb file is created anywhere.
// The view "view-th" is declared on line 1 of master.zones.
func setupGeoIPOptionalTestDir(t *testing.T, geoipDirective, matchClients string) (dir, masterZones string) {
	t.Helper()
	dir = t.TempDir()

	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte(minimalZone), 0o644); err != nil {
		t.Fatalf("write zone: %v", err)
	}

	masterZones = writeMasterZonesFixture(t, dir, "view-th", matchClients)
	writeNamedConfFixture(t, dir, masterZones, geoipDirective)

	shadowConf := filepath.Join(dir, "shadowdns.yaml")
	if err := os.WriteFile(shadowConf, []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}
	return dir, masterZones
}

// TestRun_NoGeoRules_StartsWithoutMMDB verifies that a configuration with only
// non-geo match-clients rules and no geoip-directory starts successfully on a
// host with no mmdb files at all.
func TestRun_NoGeoRules_StartsWithoutMMDB(t *testing.T) {
	dir, _ := setupGeoIPOptionalTestDir(t, "", "any;")

	// Reaching readiness is the whole assertion: startup must not require mmdb.
	_, teardown := runUntilReadyObserved(t, runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
	})
	teardown()
}

// requireGeoRuleConfigError asserts that err is the explicit configuration
// error for geo rules without geoip-directory: it must name the offending
// view, its source file, and its line number — and must not be a file-open
// error.
func requireGeoRuleConfigError(t *testing.T, err error, viewName, masterZones string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected geo-rule configuration error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `view "`+viewName+`"`) {
		t.Errorf("error should name the offending view, got: %s", msg)
	}
	if !strings.Contains(msg, masterZones+":1") {
		t.Errorf("error should contain source path and line %q, got: %s", masterZones+":1", msg)
	}
	if !strings.Contains(msg, "geoip-directory") {
		t.Errorf("error should mention geoip-directory, got: %s", msg)
	}
	if strings.Contains(msg, "no such file or directory") {
		t.Errorf("error must be a configuration error, not a file-open error, got: %s", msg)
	}
}

// TestRun_GeoRulesWithoutDirectory_FailsWithSourceLine verifies that a view
// using a geoip country rule without geoip-directory fails startup with an
// explicit configuration error naming the view and its source location.
func TestRun_GeoRulesWithoutDirectory_FailsWithSourceLine(t *testing.T) {
	dir, masterZones := setupGeoIPOptionalTestDir(t, "", "geoip country TH;")

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        zap.NewNop(),
	}

	err := run(context.Background(), opts)
	requireGeoRuleConfigError(t, err, "view-th", masterZones)
}

// TestRun_EmptyGeoIPDirectory_BehavesAsUnset verifies that
// `geoip-directory "";` behaves exactly like an absent option: geo rules
// produce the same explicit configuration error, never a relative-path
// file-open error.
func TestRun_EmptyGeoIPDirectory_BehavesAsUnset(t *testing.T) {
	dir, masterZones := setupGeoIPOptionalTestDir(t, `geoip-directory "";`, "geoip country TH;")

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        zap.NewNop(),
	}

	err := run(context.Background(), opts)
	requireGeoRuleConfigError(t, err, "view-th", masterZones)
}

// runUntilReadyObserved starts run() with an observer-backed logger, waits for
// readiness, then waits for the "shadowdns ready" entry to be recorded before
// returning it together with the full observed logs and a teardown func.
func runUntilReadyObserved(t *testing.T, opts runOptions) (*observer.ObservedLogs, func()) {
	t.Helper()
	logger, logs := newObservedLogger()
	opts.Logger = logger
	ready := make(chan struct{})
	opts.ReadyCh = ready

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	waitReady(t, ready)
	// ReadyCh is closed just before the ready log line is emitted; poll
	// briefly for the entry instead of assuming it landed already.
	deadline := time.Now().Add(2 * time.Second)
	for logs.FilterMessage("shadowdns ready").Len() == 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("shadowdns ready log entry not observed within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	return logs, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("run did not return within 2s after cancel")
		}
	}
}

// requireBoolField asserts that the single observed entry carries the boolean
// field with the expected value.
func requireBoolField(t *testing.T, entry observer.LoggedEntry, field string, want bool) {
	t.Helper()
	got, ok := entry.ContextMap()[field]
	if !ok {
		t.Fatalf("log entry %q is missing field %q", entry.Message, field)
	}
	if got != want {
		t.Errorf("log entry %q field %q = %v, want %v", entry.Message, field, got, want)
	}
}

// TestRun_ReadyLogReportsGeoIPEnabledField verifies the readiness log carries
// geoip_enabled=false without GeoIP and geoip_enabled=true with it.
func TestRun_ReadyLogReportsGeoIPEnabledField(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		dir, _ := setupGeoIPOptionalTestDir(t, "", "any;")
		logs, teardown := runUntilReadyObserved(t, runOptions{
			NamedConfPath: filepath.Join(dir, "named.conf"),
			ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
			ListenAddr:    "127.0.0.1:0",
		})
		defer teardown()
		entries := logs.FilterMessage("shadowdns ready").All()
		requireBoolField(t, entries[0], "geoip_enabled", false)
	})

	t.Run("enabled", func(t *testing.T) {
		dir := setupReloadTestDir(t)
		logs, teardown := runUntilReadyObserved(t, runOptions{
			NamedConfPath: filepath.Join(dir, "named.conf"),
			ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
			ListenAddr:    "127.0.0.1:0",
		})
		defer teardown()
		entries := logs.FilterMessage("shadowdns ready").All()
		requireBoolField(t, entries[0], "geoip_enabled", true)
	})
}

// TestRun_DryRun_NoGeoIP_SucceedsAndReportsState verifies --dry-run succeeds
// without mmdb files when no geo rule exists and its summary log carries
// geoip_enabled=false.
func TestRun_DryRun_NoGeoIP_SucceedsAndReportsState(t *testing.T) {
	dir, _ := setupGeoIPOptionalTestDir(t, "", "any;")

	logger, logs := newObservedLogger()
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		DryRun:        true,
		Logger:        logger,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run should succeed without GeoIP, got: %v", err)
	}
	entries := logs.FilterMessage("dry-run: configuration loaded successfully").All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 dry-run summary log entry, got %d", len(entries))
	}
	requireBoolField(t, entries[0], "geoip_enabled", false)
}

// TestRun_DryRun_GeoRulesWithoutDirectory_Fails verifies --dry-run fails under
// exactly the same GeoIP conditions as a real startup, with the same explicit
// configuration error.
func TestRun_DryRun_GeoRulesWithoutDirectory_Fails(t *testing.T) {
	dir, masterZones := setupGeoIPOptionalTestDir(t, "", "geoip country TH;")

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		DryRun:        true,
		Logger:        zap.NewNop(),
	}

	err := run(context.Background(), opts)
	requireGeoRuleConfigError(t, err, "view-th", masterZones)
}

// TestRun_ECSWithoutGeoIP_WarnsAtStartup verifies that --ecs-enable with no
// GeoIP database loaded logs exactly one warning and the server still starts,
// and that no such warning is emitted when GeoIP is loaded.
func TestRun_ECSWithoutGeoIP_WarnsAtStartup(t *testing.T) {
	t.Run("warns without GeoIP", func(t *testing.T) {
		dir, _ := setupGeoIPOptionalTestDir(t, "", "any;")
		logs, teardown := runUntilReadyObserved(t, runOptions{
			NamedConfPath: filepath.Join(dir, "named.conf"),
			ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
			ListenAddr:    "127.0.0.1:0",
			ECSEnable:     true,
		})
		defer teardown()
		if got := countECSWarnings(logs); got != 1 {
			t.Errorf("expected exactly 1 ECS-without-GeoIP warning, got %d", got)
		}
	})

	t.Run("no warning with GeoIP loaded", func(t *testing.T) {
		dir := setupReloadTestDir(t)
		logs, teardown := runUntilReadyObserved(t, runOptions{
			NamedConfPath: filepath.Join(dir, "named.conf"),
			ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
			ListenAddr:    "127.0.0.1:0",
			ECSEnable:     true,
		})
		defer teardown()
		if got := countECSWarnings(logs); got != 0 {
			t.Errorf("expected no ECS warning when GeoIP is loaded, got %d", got)
		}
	})
}

// TestRun_NoGeoIP_MetricsHasNoGeoIPDBInfoSeries verifies that the metrics
// endpoint exposes no shadowdns_geoip_db_info series when GeoIP is not loaded.
func TestRun_NoGeoIP_MetricsHasNoGeoIPDBInfoSeries(t *testing.T) {
	dir, _ := setupGeoIPOptionalTestDir(t, "", "any;")
	addr := freePort(t)

	_, teardown := runUntilReadyObserved(t, runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   addr,
	})
	defer teardown()

	code, body := httpGet(t, "http://"+addr+"/metrics")
	if code != 200 {
		t.Fatalf("GET /metrics: got %d, want 200", code)
	}
	if strings.Contains(string(body), "shadowdns_geoip_db_info") {
		t.Errorf("metrics endpoint must expose no shadowdns_geoip_db_info series without GeoIP, but found one")
	}
}
