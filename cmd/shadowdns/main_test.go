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
	"syscall"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/logging"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

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
	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
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
		AliasesPath:   "",
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

// buildEmptyMMDBs creates two minimal valid mmdb files in dir so that
// view.LoadGeoIP succeeds. The DBs contain no records — every IP lookup
// returns no-match, which is fine for tests that don't exercise GeoIP rules.
func buildEmptyMMDBs(t *testing.T, dir string) {
	t.Helper()

	country, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create country mmdb writer: %v", err)
	}
	// Insert a no-op record so the writer produces a valid file.
	_, ipnet, _ := net.ParseCIDR("0.0.0.0/0")
	if err := country.Insert(ipnet, mmdbtype.Map{}); err != nil {
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
	if err := asn.Insert(ipnet, mmdbtype.Map{}); err != nil {
		t.Fatalf("insert ASN record: %v", err)
	}
	writeMMDB(t, asn, filepath.Join(dir, "GeoLite2-ASN.mmdb"))
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

	srv, country, asn, opts := startReloadTestServer(t, dir)
	defer func() {
		_ = country.Close()
		_ = asn.Close()
	}()

	// Verify initial A record: 203.0.113.10.
	resp := reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "203.0.113.10")

	// Overwrite zone file with a new A record.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte(updatedZone("198.51.100.42")), 0o644); err != nil {
		t.Fatalf("rewrite zone: %v", err)
	}

	// Reload.
	if err := reload(context.Background(), opts, srv, country, asn, opts.Logger); err != nil {
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

	srv, country, asn, opts := startReloadTestServer(t, dir)
	defer func() {
		_ = country.Close()
		_ = asn.Close()
	}()

	// Verify initial state works.
	resp := reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "203.0.113.10")

	// Break the zone file.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte("THIS IS NOT A VALID ZONE FILE"), 0o644); err != nil {
		t.Fatalf("break zone: %v", err)
	}

	// Reload should fail.
	if err := reload(context.Background(), opts, srv, country, asn, opts.Logger); err == nil {
		t.Fatal("expected reload to return an error for broken zone")
	}

	// Old state should still be in effect.
	resp = reloadQuery(t, srv, "example.com.", dns.TypeA)
	requireARecord(t, resp, "203.0.113.10")
}

// ---------------------------------------------------------------------------
// reload test helpers
// ---------------------------------------------------------------------------

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

	masterZones := filepath.Join(dir, "master.zones")
	masterZonesContent := `view "view-other" {
    match-clients { any; };
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

	namedConf := filepath.Join(dir, "named.conf")
	namedConfContent := `options {
    directory "` + dir + `";
    geoip-directory "` + geoIPDir + `";
    listen-on { any; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(namedConfContent), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	return dir
}

func startReloadTestServer(t *testing.T, dir string) (*server.Server, *view.CountryDB, *view.ASNDB, runOptions) {
	t.Helper()

	namedConf := filepath.Join(dir, "named.conf")
	logger := zap.NewNop()
	opts := runOptions{
		NamedConfPath: namedConf,
		AliasesPath:   "",
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("load named.conf: %v", err)
	}

	aliases, err := config.LoadAliases("", logger)
	if err != nil {
		t.Fatalf("load aliases: %v", err)
	}

	country, asn, err := view.LoadGeoIP(cfg.Options.GeoIPDirectory, logger)
	if err != nil {
		t.Fatalf("load geoip: %v", err)
	}

	state, _, err := server.BuildState(cfg, aliases, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		t.Fatalf("build state: %v", err)
	}

	srv := server.NewServer(state, logger)
	return srv, country, asn, opts
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

	srv, country, asn, opts := startReloadTestServer(t, dir)
	defer func() {
		_ = country.Close()
		_ = asn.Close()
	}()

	// Create a separate logger backed by a buffer we can inspect.
	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	if err := reload(context.Background(), opts, srv, country, asn, logger); err != nil {
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

	srv, country, asn, opts := startReloadTestServer(t, dir)
	defer func() {
		_ = country.Close()
		_ = asn.Close()
	}()

	// Break the zone file so reload will fail.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte("THIS IS NOT A VALID ZONE FILE"), 0o644); err != nil {
		t.Fatalf("break zone: %v", err)
	}

	// Create a separate logger backed by a buffer we can inspect.
	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	if err := reload(context.Background(), opts, srv, country, asn, logger); err == nil {
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

	srv, country, asn, opts := startReloadTestServer(t, dir)
	defer func() {
		_ = country.Close()
		_ = asn.Close()
	}()

	// Use hash mode (the default and safest mode).
	opts.ReloadVerify = server.VerifyModeHash

	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	if err := reload(context.Background(), opts, srv, country, asn, logger); err != nil {
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

	srv, country, asn, opts := startReloadTestServer(t, dir)
	defer func() {
		_ = country.Close()
		_ = asn.Close()
	}()

	opts.ReloadVerify = server.VerifyModeHash

	var buf bytes.Buffer
	logger := newBufferLogger(zapcore.AddSync(&buf))

	// First reload: zone was just loaded at startup, fingerprints are set, so
	// the zone file is unchanged → reused=1, reparsed=0.
	if err := reload(context.Background(), opts, srv, country, asn, logger); err != nil {
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
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		AliasesPath:   "",
		ListenAddr:    listenAddr,
		Logger:        logger,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// Wait for the server to start listening.
	time.Sleep(200 * time.Millisecond)

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
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		AliasesPath:   "",
		ListenAddr:    listenAddr,
		Logger:        logger,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// Wait for run() to bind listeners and write the PID file.
	time.Sleep(200 * time.Millisecond)

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
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		AliasesPath:   "",
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// Wait for run() to start up fully.
	time.Sleep(200 * time.Millisecond)

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
