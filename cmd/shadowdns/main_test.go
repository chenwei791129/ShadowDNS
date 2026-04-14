package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

func TestVersionVariable_HasDefault(t *testing.T) {
	if version == "" {
		t.Fatal("version variable should have a non-empty default value")
	}
}

func TestRunRequiresNamedConfPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
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
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
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
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
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

	state, err := server.BuildState(cfg, aliases, country, asn, logger)
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
	logger := slog.New(slog.NewTextHandler(&buf, nil))

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
	logger := slog.New(slog.NewTextHandler(&buf, nil))

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

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
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
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
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

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
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

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		Logger:        logger,
	}

	if err := runReload(opts); err != nil {
		t.Fatalf("runReload returned unexpected error: %v", err)
	}
}

// TestRunReload_MissingNamedConf verifies that runReload returns an error
// containing "-named-conf is required" when NamedConfPath is empty.
func TestRunReload_MissingNamedConf(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		Logger: logger,
	}

	err := runReload(opts)
	if err == nil {
		t.Fatal("expected error when NamedConfPath is empty")
	}
	if !strings.Contains(err.Error(), "-named-conf is required") {
		t.Errorf("expected error to contain %q, got: %v", "-named-conf is required", err)
	}
}

// TestRunReload_NoPidFileConfigured verifies that runReload returns an error
// mentioning "pid-file" when named.conf has no pid-file option.
func TestRunReload_NoPidFileConfigured(t *testing.T) {
	dir := setupReloadTestDir(t)

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		Logger:        logger,
	}

	err := runReload(opts)
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

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		Logger:        logger,
	}

	err := runReload(opts)
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

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		Logger:        logger,
	}

	err := runReload(opts)
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

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		Logger:        logger,
	}

	err := runReload(opts)
	if err == nil {
		t.Fatal("expected error when PID does not correspond to a running process")
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
func (w *testResponseWriter) Hijack()                     {}
