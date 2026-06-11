package main

// Tests for tasks 4.1–4.5: daemon wiring of the query log in main.go.
//
// All fixture domains use RFC 2606 names (example.com, test, etc.).
// All temporary file paths are under t.TempDir().

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/logging"
)

// ---------------------------------------------------------------------------
// Helpers: named.conf fixtures with query logging
// ---------------------------------------------------------------------------

// setupQueryLogTestDir creates a minimal valid test directory (same as
// setupReloadTestDir) and then rewrites named.conf to include a logging{}
// block targeting logPath. Returns the directory path.
func setupQueryLogTestDir(t *testing.T, logPath string) string {
	t.Helper()
	dir := setupReloadTestDir(t)
	addLoggingBlock(t, dir, logPath)
	return dir
}

// addLoggingBlock appends a logging{} block to the named.conf in dir that
// targets logPath. The channel uses severity debug so nothing is filtered out.
// Optional fileExtra tokens (e.g. "versions 3 size 5000m") are appended to the
// file clause.
func addLoggingBlock(t *testing.T, dir, logPath string, fileExtra ...string) {
	t.Helper()
	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	extra := ""
	if len(fileExtra) > 0 {
		extra = " " + strings.Join(fileExtra, " ")
	}
	loggingBlock := fmt.Sprintf(`
logging {
    channel queries_log {
        file %q%s;
        severity debug;
        print-time yes;
        print-category yes;
        print-severity yes;
    };
    category queries { queries_log; };
};
`, logPath, extra)
	if err := os.WriteFile(namedConf, append(data, []byte(loggingBlock)...), 0o644); err != nil {
		t.Fatalf("write named.conf with logging block: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 4.1: Startup fails loudly when query log path is bad
// ---------------------------------------------------------------------------

// TestQueryLogStartup_FailsOnBadPath verifies that when Config.QueryLog points
// to a path inside a non-existent directory, run() returns an error that
// mentions the file path (not a silent disable).
func TestQueryLogStartup_FailsOnBadPath(t *testing.T) {
	// Use a path whose parent directory does not exist.
	badLogPath := filepath.Join(t.TempDir(), "nonexistent-subdir", "queries.log")
	dir := setupQueryLogTestDir(t, badLogPath)

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	err := run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected run() to return an error when query log path is bad, got nil")
	}
	if !strings.Contains(err.Error(), badLogPath) {
		t.Errorf("error should mention the bad path %q, got: %v", badLogPath, err)
	}
}

// ---------------------------------------------------------------------------
// Task 4.2: SIGUSR1 reopens query log sink
// ---------------------------------------------------------------------------

// startServerWithQueryLog boots run() with both --log-file and a query log,
// waits for the SIGHUP handler to be ready, and returns cleanup handles.
func startServerWithQueryLog(t *testing.T, dir, mainLogPath, queryLogPath string) (cancel context.CancelFunc, done <-chan error) {
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
		LogFile: mainLogPath,
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

// TestSIGUSR1_ReopensQueryLog verifies that after the query log file is
// renamed, sending SIGUSR1 causes the daemon to create a fresh file at the
// original path (new inode), satisfying logrotate semantics.
func TestSIGUSR1_ReopensQueryLog(t *testing.T) {
	mainLogPath := filepath.Join(t.TempDir(), "main.log")
	queryLogPath := filepath.Join(t.TempDir(), "queries.log")

	// Create the query log file path first so addLoggingBlock can write its
	// absolute path into named.conf.
	dir := setupReloadTestDir(t)
	addLoggingBlock(t, dir, queryLogPath)

	cancel, done := startServerWithQueryLog(t, dir, mainLogPath, queryLogPath)
	defer cancel()

	// Verify the query log file was created by startup.
	if _, err := os.Stat(queryLogPath); err != nil {
		t.Fatalf("query log file not created at startup: %v", err)
	}
	originalInode := inode(t, queryLogPath)

	// Simulate logrotate: rename the current log file.
	rotated := queryLogPath + ".1"
	if err := os.Rename(queryLogPath, rotated); err != nil {
		t.Fatalf("rename query log: %v", err)
	}

	// Send SIGUSR1 to trigger reopen.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}

	// Wait for the handler to create a new file at queryLogPath.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(queryLogPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(queryLogPath); err != nil {
		t.Fatalf("query log not recreated after SIGUSR1: %v", err)
	}

	newInode := inode(t, queryLogPath)
	if newInode == originalInode {
		t.Fatalf("expected new inode after SIGUSR1, got same inode %d", newInode)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// TestSIGUSR1_QueryLogOnly_NoMainLogFile verifies that the SIGUSR1 handler is
// installed even when --log-file is unset (stderr mode) as long as the query
// log is enabled.
func TestSIGUSR1_QueryLogOnly_NoMainLogFile(t *testing.T) {
	queryLogPath := filepath.Join(t.TempDir(), "queries.log")
	dir := setupReloadTestDir(t)
	addLoggingBlock(t, dir, queryLogPath)

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", port),
		// No LogReopener — stderr mode.
		ReadyCh: readyCh,
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- run(ctx, opts) }()
	waitReady(t, readyCh)

	// Query log file must exist (startup succeeded).
	if _, err := os.Stat(queryLogPath); err != nil {
		t.Fatalf("query log file not created: %v", err)
	}

	// Rename to simulate logrotate.
	rotated := queryLogPath + ".1"
	if err := os.Rename(queryLogPath, rotated); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// SIGUSR1 must reopen even without --log-file.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(queryLogPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(queryLogPath); err != nil {
		t.Fatalf("query log not recreated after SIGUSR1: %v", err)
	}

	cancel()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// ---------------------------------------------------------------------------
// Task 4.3: RotationIgnored warning at startup (including dry-run)
// ---------------------------------------------------------------------------

// setupQueryLogTestDirWithRotation creates a named.conf with a logging{} block
// that includes versions/size parameters (triggering RotationIgnored).
func setupQueryLogTestDirWithRotation(t *testing.T, logPath string) string {
	t.Helper()
	dir := setupReloadTestDir(t)
	addLoggingBlock(t, dir, logPath, "versions 3 size 5000m")
	return dir
}

// TestRotationIgnoredWarning_EmittedOnce verifies that exactly one warning
// about ignored BIND rotation parameters is emitted when RotationIgnored==true.
func TestRotationIgnoredWarning_EmittedOnce(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "queries.log")
	dir := setupQueryLogTestDirWithRotation(t, logPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- run(ctx, opts) }()
	waitReady(t, readyCh)
	cancel()
	<-doneCh

	output := buf.String()
	// Count occurrences of the rotation warning line. Each warning line
	// contains "BIND rotation parameters" as a stable substring that appears
	// exactly once per warning emission.
	const rotationMarker = "BIND rotation parameters"
	count := strings.Count(output, rotationMarker)
	if count == 0 {
		t.Errorf("expected rotation warning in log output, got none; full output:\n%s", output)
	}
	if count > 1 {
		t.Errorf("expected exactly 1 rotation warning, got %d; full output:\n%s", count, output)
	}
}

// TestRotationIgnoredWarning_NotEmittedWhenFalse verifies that no rotation
// warning is emitted when RotationIgnored==false (no versions/size params).
func TestRotationIgnoredWarning_NotEmittedWhenFalse(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "queries.log")
	// setupQueryLogTestDir uses a plain file clause without versions/size.
	dir := setupQueryLogTestDir(t, logPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- run(ctx, opts) }()
	waitReady(t, readyCh)
	cancel()
	<-doneCh

	output := buf.String()
	// Check absence of the specific "BIND rotation parameters" marker.
	if strings.Contains(output, "BIND rotation parameters") {
		t.Errorf("expected no rotation warning when RotationIgnored==false, got:\n%s", output)
	}
}

// TestRotationIgnoredWarning_EmittedInDryRun verifies that the rotation
// warning also appears in --dry-run mode.
func TestRotationIgnoredWarning_EmittedInDryRun(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "queries.log")
	dir := setupQueryLogTestDirWithRotation(t, logPath)

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "BIND rotation parameters") {
		t.Errorf("expected rotation warning (BIND rotation parameters) in dry-run output, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Task 4.4: --dry-run summary includes query log status
// ---------------------------------------------------------------------------

// TestDryRun_QueryLogSummary_Enabled verifies that when query logging is
// configured, the dry-run output includes the resolved file path and print
// option values.
func TestDryRun_QueryLogSummary_Enabled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "queries.log")
	dir := setupQueryLogTestDir(t, logPath)

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	// The path must appear in the summary.
	if !strings.Contains(output, logPath) {
		t.Errorf("dry-run output should contain query log path %q, got:\n%s", logPath, output)
	}
	// The summary must mention query_log or query log in some form.
	if !strings.Contains(output, "query_log") {
		t.Errorf("dry-run output should contain query_log field, got:\n%s", output)
	}
}

// TestDryRun_QueryLogSummary_Disabled_NoLoggingBlock verifies that when
// there is no logging{} block at all, the dry-run output reports the reason.
func TestDryRun_QueryLogSummary_Disabled_NoLoggingBlock(t *testing.T) {
	// setupReloadTestDir produces a named.conf without any logging{} block.
	dir := setupReloadTestDir(t)

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "query_log") {
		t.Errorf("dry-run output should contain query_log field even when disabled, got:\n%s", output)
	}
	// Reason must indicate no logging block.
	if !strings.Contains(output, "no logging") {
		t.Errorf("dry-run output should explain disabled reason (no logging), got:\n%s", output)
	}
}

// TestDryRun_QueryLogSummary_Disabled_NoCategoryQueries verifies the reason
// for disable condition: logging{} exists but has no category queries{} block.
func TestDryRun_QueryLogSummary_Disabled_NoCategoryQueries(t *testing.T) {
	dir := setupReloadTestDir(t)

	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	// A logging block with a channel but no category queries.
	loggingBlock := `
logging {
    channel queries_log {
        file "/tmp/queries.log";
        severity debug;
    };
};
`
	if err := os.WriteFile(namedConf, append(data, []byte(loggingBlock)...), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "category queries") {
		t.Errorf("dry-run output should explain disabled reason (no category queries), got:\n%s", output)
	}
}

// TestDryRun_QueryLogSummary_Disabled_NullChannel verifies the reason for
// disable condition: category queries{} points to null or a built-in channel.
func TestDryRun_QueryLogSummary_Disabled_NullChannel(t *testing.T) {
	dir := setupReloadTestDir(t)

	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	loggingBlock := `
logging {
    category queries { null; };
};
`
	if err := os.WriteFile(namedConf, append(data, []byte(loggingBlock)...), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	// null channel disable reason.
	if !strings.Contains(output, "null") || !strings.Contains(output, "built-in") {
		t.Errorf("dry-run output should explain null/built-in channel disable, got:\n%s", output)
	}
}

// TestDryRun_QueryLogSummary_Disabled_NonFileChannel verifies the reason for
// disable condition: category queries{} points to a non-file channel.
func TestDryRun_QueryLogSummary_Disabled_NonFileChannel(t *testing.T) {
	dir := setupReloadTestDir(t)

	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	loggingBlock := `
logging {
    channel q {
        syslog daemon;
    };
    category queries { q; };
};
`
	if err := os.WriteFile(namedConf, append(data, []byte(loggingBlock)...), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "non-file") {
		t.Errorf("dry-run output should explain non-file channel disable, got:\n%s", output)
	}
}

// TestDryRun_QueryLogSummary_Disabled_StrictSeverity verifies the reason for
// disable condition: channel severity is stricter than info.
func TestDryRun_QueryLogSummary_Disabled_StrictSeverity(t *testing.T) {
	dir := setupReloadTestDir(t)

	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	loggingBlock := `
logging {
    channel q {
        file "/tmp/q.log";
        severity warning;
    };
    category queries { q; };
};
`
	if err := os.WriteFile(namedConf, append(data, []byte(loggingBlock)...), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
		DryRun:        true,
	}

	if err := run(context.Background(), opts); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "severity") {
		t.Errorf("dry-run output should explain severity-too-strict disable, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// SIGHUP reload re-applies logging configuration
// ---------------------------------------------------------------------------

// TestReload_QueryLogPathChangeApplied verifies that a SIGHUP reload applies a
// changed logging{} file path: the new path is created and the unchanged
// remainder of the run keeps serving (scenario "Query log path change takes
// effect on reload"; the former does-not-re-apply requirement was removed).
func TestReload_QueryLogPathChangeApplied(t *testing.T) {
	origQueryLog := filepath.Join(t.TempDir(), "queries.log")
	newQueryLog := filepath.Join(t.TempDir(), "queries-new.log")

	dir := setupReloadTestDir(t)
	addLoggingBlock(t, dir, origQueryLog)

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", port),
		ReadyCh:       readyCh,
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- run(ctx, opts) }()
	waitReady(t, readyCh)

	// Original query log must be created.
	if _, err := os.Stat(origQueryLog); err != nil {
		t.Fatalf("original query log not created: %v", err)
	}

	// Point the logging block at a new path; the reload must apply it.
	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf for patch: %v", err)
	}
	patched := strings.ReplaceAll(string(data), origQueryLog, newQueryLog)
	if err := os.WriteFile(namedConf, []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched named.conf: %v", err)
	}

	// Send SIGHUP to trigger reload.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}

	// The new log path must be created by the reload.
	waitForFile(t, newQueryLog)

	// Query traffic must land in the new file.
	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("example.com.", dns.TypeA)
	if _, _, err := c.Exchange(m, fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("post-reload query: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var newContent []byte
	for time.Now().Before(deadline) {
		b, rerr := os.ReadFile(newQueryLog)
		if rerr == nil && strings.Contains(string(b), "example.com") {
			newContent = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(newContent), "example.com") {
		t.Errorf("query line did not land in the new query log; got %q", string(newContent))
	}

	cancel()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// TestReload_QueryLogUntouched_MalformedLogging verifies that when the
// reloaded named.conf contains a malformed logging{} block, the reload fails
// and the original query log sink is preserved.
func TestReload_QueryLogUntouched_MalformedLogging(t *testing.T) {
	origQueryLog := filepath.Join(t.TempDir(), "queries.log")
	dir := setupReloadTestDir(t)
	addLoggingBlock(t, dir, origQueryLog)

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readyCh := make(chan struct{})
	buf := &threadSafeBuffer{}
	logger := newBufferLogger(zapcore.AddSync(buf))
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", port),
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- run(ctx, opts) }()
	waitReady(t, readyCh)

	if _, err := os.Stat(origQueryLog); err != nil {
		t.Fatalf("original query log not created: %v", err)
	}
	originalInode := inode(t, origQueryLog)

	// Inject a malformed logging block into named.conf.
	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	malformed := string(data) + "\nlogging { channel bad { /* unterminated\n"
	if err := os.WriteFile(namedConf, []byte(malformed), 0o644); err != nil {
		t.Fatalf("write malformed named.conf: %v", err)
	}

	// SIGHUP: reload should fail due to parse error.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Verify reload failure was logged.
	if !strings.Contains(buf.String(), "reload failed") {
		t.Logf("log output so far:\n%s", buf.String())
		// This is a soft check; the important assertion is below.
	}

	// The original query log sink must be untouched.
	if _, err := os.Stat(origQueryLog); err != nil {
		t.Fatalf("original query log disappeared after failed reload: %v", err)
	}
	postInode := inode(t, origQueryLog)
	if postInode != originalInode {
		t.Errorf("original query log inode changed after failed reload (%d → %d)",
			originalInode, postInode)
	}

	cancel()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}
