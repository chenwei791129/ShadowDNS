// Integration tests for the NOTIFY toggle (-no-notify CLI flag and
// options.notify directive). These tests exec the compiled shadowdns binary
// so they exercise the real flag-parsing and precedence resolution path.
//
// The zones used here intentionally declare an NS record whose target differs
// from the SOA MNAME, so NotifyTargets returns a non-empty list. With NOTIFY
// enabled, shadowdns would try to resolve that target and emit a "NOTIFY
// failed" warning (the NS name is a hostname that does not resolve); the
// absence of that log line is the observable proof that no goroutine was
// spawned and no send was attempted.
package integration_test

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// binaryPath holds the path to the compiled shadowdns binary for this test
// run. Built once per package via buildOnce.Do to amortize the go-build cost
// across all notify integration tests.
var (
	buildOnce  sync.Once
	binaryPath string
	buildErr   error
)

// buildShadowDNSBinary compiles cmd/shadowdns once and returns the binary
// path. Subsequent calls return the cached result.
func buildShadowDNSBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "shadowdns-notify-bin-*")
		if err != nil {
			buildErr = fmt.Errorf("mkdir temp: %w", err)
			return
		}
		bin := filepath.Join(dir, "shadowdns")
		cmd := exec.Command("go", "build", "-o", bin, "../../cmd/shadowdns")
		out, cerr := cmd.CombinedOutput()
		if cerr != nil {
			buildErr = fmt.Errorf("go build: %w\n%s", cerr, out)
			return
		}
		binaryPath = bin
	})
	if buildErr != nil {
		t.Fatalf("build shadowdns binary: %v", buildErr)
	}
	return binaryPath
}

// setupNotifyFixture constructs a temp directory with:
//   - a minimal named.conf (optionally with `notify yes|no;`)
//   - GeoIP mmdb files
//   - a zone file whose NS (ns2.example.com.) differs from the SOA MNAME
//     (ns1.example.com.), so NotifyTargets returns a non-empty list
//   - a pid-file path (needed so SIGHUP can be sent)
//
// Returns (tmpDir, namedConfPath, pidFilePath).
func setupNotifyFixture(t *testing.T, notifyDirective string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()

	// GeoIP.
	geoIPDir := filepath.Join(dir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)

	// Zone file: NS ns2 differs from MNAME ns1, so NotifyTargets returns [ns2].
	zoneFile := filepath.Join(dir, "example.com.zone")
	zoneContents := `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2026041501 ; serial
    3600       ; refresh
    600        ; retry
    604800     ; expire
    300        ; minimum
)

@    IN NS    ns1.example.com.
@    IN NS    ns2.example.com.
@    IN A     203.0.113.10
ns1  IN A     203.0.113.1
ns2  IN A     203.0.113.2
`
	if err := os.WriteFile(zoneFile, []byte(zoneContents), 0o644); err != nil {
		t.Fatalf("write zone: %v", err)
	}

	// named.conf: single view, optional notify directive.
	pidFile := filepath.Join(dir, "shadowdns.pid")
	namedConf := filepath.Join(dir, "named.conf")
	notifyLine := ""
	if notifyDirective != "" {
		notifyLine = fmt.Sprintf("    notify %s;\n", notifyDirective)
	}
	namedConfContent := fmt.Sprintf(`options {
    directory "%s";
    geoip-directory "%s";
    listen-on { 127.0.0.1; };
    pid-file "%s";
%s    recursion no;
};

view "view-other" {
    match-clients { any; };
    recursion no;
    zone "example.com" {
        type master;
        file "%s";
    };
};
`, dir, geoIPDir, pidFile, notifyLine, zoneFile)
	if err := os.WriteFile(namedConf, []byte(namedConfContent), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	return dir, namedConf, pidFile
}

// freeLoopbackPort returns a loopback port that was free at the moment of
// query. The close-and-bind gap is an inherent TOCTOU race; callers are
// expected to handle the race (startShadowDNS retries with a fresh port).
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	// Release TCP side of the same port too; shadowdns binds both protocols.
	ln, tcpErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if tcpErr == nil {
		_ = ln.Close()
	}
	return port
}

const (
	maxStartupAttempts = 3
	startupWindow      = 3 * time.Second
	startupPollEvery   = 20 * time.Millisecond
)

type startupOutcome int

const (
	startupSuccess startupOutcome = iota
	startupBindFailed
	startupChildExit
	startupHung
)

func (o startupOutcome) String() string {
	switch o {
	case startupSuccess:
		return "success"
	case startupBindFailed:
		return "bind-failed"
	case startupChildExit:
		return "child-exit"
	case startupHung:
		return "hung"
	default:
		return "unknown"
	}
}

// startShadowDNS launches the built binary and tolerates transient bind
// races: each attempt allocates a fresh loopback port, watches child output
// for success / bind-failure signals, kills + reaps on failure, and retries
// up to maxStartupAttempts. The caller MUST defer the returned cleanup.
func startShadowDNS(t *testing.T, namedConf string, extraArgs ...string) (*exec.Cmd, *syncBuffer, func()) {
	t.Helper()

	bin := buildShadowDNSBinary(t)

	type attemptLog struct {
		port    int
		outcome startupOutcome
		out     string
	}
	attempts := make([]attemptLog, 0, maxStartupAttempts)

	for i := 1; i <= maxStartupAttempts; i++ {
		port := freeLoopbackPort(t)
		listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

		args := []string{
			"-named-conf", namedConf,
			"-listen", listenAddr,
			"-metrics-addr", "", // disable metrics so we don't fight another free port
		}
		args = append(args, extraArgs...)

		cmd := exec.Command(bin, args...)
		buf := &syncBuffer{}
		cmd.Stdout = buf
		cmd.Stderr = buf

		if err := cmd.Start(); err != nil {
			t.Fatalf("start shadowdns (attempt %d): %v", i, err)
		}

		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()

		outcome := awaitStartupOutcome(buf, waitCh)

		if outcome == startupSuccess {
			cleanup := func() {
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGTERM)
					<-waitCh
				}
			}
			return cmd, buf, cleanup
		}

		_ = cmd.Process.Signal(syscall.SIGKILL)
		<-waitCh
		attempts = append(attempts, attemptLog{port: port, outcome: outcome, out: buf.String()})
	}

	// Verbose dump so an operator can tell port contention from an
	// application-level bug: each attempt's outcome is the tell.
	var b strings.Builder
	fmt.Fprintf(&b, "shadowdns failed to start within %d attempts:\n", maxStartupAttempts)
	for i, a := range attempts {
		fmt.Fprintf(&b, "--- attempt %d (port %d, %s) ---\n%s\n", i+1, a.port, a.outcome, a.out)
	}
	t.Fatalf("%s", b.String())
	panic("unreachable")
}

// classifyStartupOutput returns (outcome, true) if buf contains a
// startup success or bind-failure signal, (0, false) otherwise.
func classifyStartupOutput(s string) (startupOutcome, bool) {
	switch {
	case strings.Contains(s, "shadowdns ready"):
		return startupSuccess, true
	case strings.Contains(s, "address already in use"),
		strings.Contains(s, "bind: no listeners bound"):
		return startupBindFailed, true
	default:
		return 0, false
	}
}

func awaitStartupOutcome(buf *syncBuffer, waitCh <-chan error) startupOutcome {
	deadline := time.Now().Add(startupWindow)
	for {
		if o, ok := classifyStartupOutput(buf.String()); ok {
			return o
		}
		select {
		case <-waitCh:
			// Re-check: the signal may have landed between poll and exit.
			if o, ok := classifyStartupOutput(buf.String()); ok {
				return o
			}
			return startupChildExit
		default:
		}
		if !time.Now().Before(deadline) {
			return startupHung
		}
		time.Sleep(startupPollEvery)
	}
}

// waitForLog polls buf until `substr` appears or timeout expires. Returns the
// captured output at the moment the check succeeds (or at timeout).
func waitForLog(t *testing.T, buf *syncBuffer, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := buf.String()
		if strings.Contains(s, substr) {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	return buf.String()
}

// readPidFile waits for the pid-file to appear and returns the PID.
func readPidFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, cerr := strconv.Atoi(strings.TrimSpace(string(data)))
			if cerr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s never became readable within %s", path, timeout)
	return 0
}

// syncBuffer is a thread-safe bytes.Buffer wrapper. The shadowdns child
// process writes to it asynchronously; tests read concurrently while waiting
// for specific log lines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestIntegration_NoNotifyFlag_SuppressesAllSends verifies that starting
// shadowdns with -no-notify prevents any NOTIFY goroutine from being spawned,
// even when the zone has an NS target that differs from SOA MNAME (and would
// otherwise trigger a send attempt to a non-resolvable host, producing a
// "NOTIFY failed" warning).
func TestIntegration_NoNotifyFlag_SuppressesAllSends(t *testing.T) {
	t.Parallel()

	_, namedConf, _ := setupNotifyFixture(t, "")

	_, buf, cleanup := startShadowDNS(t, namedConf, "-no-notify")
	defer cleanup()

	// Wait for the notify-state log line to appear.
	output := waitForLog(t, buf, "notify state resolved", 5*time.Second)

	if !strings.Contains(output, "notify state resolved") {
		t.Fatalf("expected notify-state log, got: %s", output)
	}
	if !strings.Contains(output, `"enabled": false`) {
		t.Errorf("expected enabled=false under -no-notify, got: %s", output)
	}
	if !strings.Contains(output, `"source": "flag"`) {
		t.Errorf("expected source=flag, got: %s", output)
	}

	// Wait long enough for a would-be NOTIFY attempt to surface: the first
	// attempt fails synchronously on DNS resolution (unresolvable hostname),
	// and the wrapper logs "NOTIFY failed" within a few ms. 500ms is well
	// past that window while keeping the test snappy.
	time.Sleep(500 * time.Millisecond)

	final := buf.String()
	if strings.Contains(final, "NOTIFY failed") {
		t.Errorf("expected NO `NOTIFY failed` log under -no-notify, but got one. Output: %s", final)
	}
	// Also confirm no send goroutine ran to the point of logging the outer
	// warning in main.go's dispatchNotifies wrapper.
	if strings.Contains(final, `msg="NOTIFY failed"`) {
		t.Errorf("expected no NOTIFY attempt, got wrapper log: %s", final)
	}
}

// TestIntegration_NoNotifyFlag_StickyAcrossSIGHUP proves the "CLI flag effect
// persists across SIGHUP reload" scenario: start with -no-notify and
// `options { notify yes; };`, SIGHUP after rewriting config to keep
// `notify yes;`, and confirm no NOTIFY is sent even though config says yes.
func TestIntegration_NoNotifyFlag_StickyAcrossSIGHUP(t *testing.T) {
	t.Parallel()

	_, namedConf, pidFile := setupNotifyFixture(t, "yes")

	_, buf, cleanup := startShadowDNS(t, namedConf, "-no-notify")
	defer cleanup()

	// Wait for initial startup log.
	waitForLog(t, buf, "notify state resolved", 5*time.Second)
	initial := buf.String()
	if !strings.Contains(initial, `"enabled": false`) || !strings.Contains(initial, `"source": "flag"`) {
		t.Fatalf("initial state should be flag-disabled; got: %s", initial)
	}

	// Find PID via pid-file and send SIGHUP.
	pid := readPidFile(t, pidFile, 3*time.Second)

	// Rewrite named.conf — still has `notify yes;`, but the reload path
	// must ignore it because the flag is sticky.
	reloadedConf := strings.Replace(
		func() string {
			data, err := os.ReadFile(namedConf)
			if err != nil {
				t.Fatalf("read named.conf: %v", err)
			}
			return string(data)
		}(),
		"notify yes;", "notify yes;  // rewritten on reload",
		1,
	)
	if err := os.WriteFile(namedConf, []byte(reloadedConf), 0o644); err != nil {
		t.Fatalf("rewrite named.conf: %v", err)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP to pid %d: %v", pid, err)
	}

	// Wait for reload-complete log.
	waitForLog(t, buf, "reload complete", 5*time.Second)

	// There must be TWO "notify state resolved" log lines now (startup + reload),
	// both showing enabled=false source=flag.
	final := buf.String()
	count := strings.Count(final, "notify state resolved")
	if count < 2 {
		t.Errorf("expected >=2 `notify state resolved` logs (startup + reload), got %d. Output: %s", count, final)
	}
	// Verify the reload-time resolution still says flag:
	if strings.Count(final, `"source": "flag"`) < 2 {
		t.Errorf("expected reload to still resolve source=flag (sticky), got: %s", final)
	}
	if strings.Contains(final, `"source": "config"`) {
		t.Errorf("reload must NOT switch to config source when flag was explicitly set; got: %s", final)
	}
	// And no NOTIFY attempt anywhere.
	if strings.Contains(final, "NOTIFY failed") {
		t.Errorf("expected no NOTIFY attempt across startup+reload, got: %s", final)
	}
}

// TestIntegration_NotifyDefault_SendsNotify is the baseline fixture-validity
// test: with no -no-notify flag and no `notify` directive in config, the
// shadowdns startup path MUST attempt to send NOTIFY to each NS target that
// differs from the SOA MNAME. The fixture's ns2.example.com. is unresolvable,
// so the attempt surfaces as a "NOTIFY failed" log within ~500ms.
//
// This test exists to prove the other tests' "no NOTIFY attempted" assertions
// are meaningful: if the fixture would never have triggered a NOTIFY under
// any circumstance, those assertions would be trivially satisfied.
func TestIntegration_NotifyDefault_SendsNotify(t *testing.T) {
	t.Parallel()

	_, namedConf, _ := setupNotifyFixture(t, "") // no notify directive

	_, buf, cleanup := startShadowDNS(t, namedConf)
	defer cleanup()

	output := waitForLog(t, buf, "notify state resolved", 5*time.Second)
	if !strings.Contains(output, `"enabled": true`) {
		t.Fatalf("expected enabled=true under default behavior, got: %s", output)
	}
	if !strings.Contains(output, `"source": "default"`) {
		t.Fatalf("expected source=default, got: %s", output)
	}

	// The first SendNOTIFY attempt fails synchronously on DNS resolution
	// (ns2.example.com. is unresolvable). waitForLog returns as soon as the
	// substring appears, or after the timeout; presence is all we need.
	output = waitForLog(t, buf, "NOTIFY failed", 3*time.Second)
	if !strings.Contains(output, "NOTIFY failed") {
		t.Errorf("expected NOTIFY attempt under default behavior (proves fixture surfaces NOTIFY), got: %s", output)
	}
}

// TestIntegration_ConfigNo_SuppressesAllSends covers the
// "NOTIFY disabled by config suppresses all sends" scenario. With
// `options { notify no; };` and no CLI flag, the guard MUST skip dispatch
// at startup; no goroutine is spawned, so no "NOTIFY failed" log appears.
func TestIntegration_ConfigNo_SuppressesAllSends(t *testing.T) {
	t.Parallel()

	_, namedConf, _ := setupNotifyFixture(t, "no")

	_, buf, cleanup := startShadowDNS(t, namedConf)
	defer cleanup()

	output := waitForLog(t, buf, "notify state resolved", 5*time.Second)
	if !strings.Contains(output, `"enabled": false`) {
		t.Errorf("expected enabled=false under `notify no;`, got: %s", output)
	}
	if !strings.Contains(output, `"source": "config"`) {
		t.Errorf("expected source=config, got: %s", output)
	}

	// Same wait budget as the flag-explicit test: NOTIFY failure, if it
	// were going to happen, would log within ~500ms of goroutine spawn.
	time.Sleep(500 * time.Millisecond)

	if strings.Contains(buf.String(), "NOTIFY failed") {
		t.Errorf("expected no NOTIFY attempt under `notify no;`, got: %s", buf.String())
	}
}

// TestIntegration_ConfigChangeOnSIGHUP_YesToNo covers the
// "Config change takes effect on SIGHUP reload" scenario: a process that
// started with `notify yes;` (and attempted NOTIFY at startup) MUST stop
// dispatching on a subsequent SIGHUP that loads `notify no;`.
//
// The observable distinction between startup-initiated goroutine retries
// (which continue in the background after reload) and a reload-time
// dispatch is the `attempt=1` counter logged by sendNotifyWithBackoff:
// each SendNOTIFY call increments from 1, so a reload-time spawn would
// produce a *second* attempt=1 entry. Startup contributes exactly one.
func TestIntegration_ConfigChangeOnSIGHUP_YesToNo(t *testing.T) {
	t.Parallel()

	_, namedConf, pidFile := setupNotifyFixture(t, "yes")

	_, buf, cleanup := startShadowDNS(t, namedConf)
	defer cleanup()

	// Wait for startup's notify-state log and the first NOTIFY failure.
	waitForLog(t, buf, "notify state resolved", 5*time.Second)
	initial := buf.String()
	if !strings.Contains(initial, `"enabled": true`) || !strings.Contains(initial, `"source": "config"`) {
		t.Fatalf("initial state should be config-enabled; got: %s", initial)
	}
	waitForLog(t, buf, `"attempt": 1`, 3*time.Second)

	// Rewrite config: notify yes; → notify no;
	pid := readPidFile(t, pidFile, 3*time.Second)
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	rewritten := strings.Replace(string(data), "notify yes;", "notify no;", 1)
	if err := os.WriteFile(namedConf, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("rewrite named.conf: %v", err)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP to pid %d: %v", pid, err)
	}
	waitForLog(t, buf, "reload complete", 5*time.Second)

	// Give any erroneous reload-time dispatch a chance to surface a new
	// attempt=1 entry.
	time.Sleep(500 * time.Millisecond)

	final := buf.String()

	// Reload-time notify-state must show enabled=false source=config.
	if strings.Count(final, "notify state resolved") < 2 {
		t.Fatalf("expected >=2 notify-state logs (startup + reload), got: %s", final)
	}
	if !strings.Contains(final, `"enabled": false`) || !strings.Contains(final, `"source": "config"`) {
		t.Errorf("expected reload to resolve enabled=false source=config; got: %s", final)
	}

	// Exactly one attempt=1: the one spawned at startup. A reload-time
	// dispatch (guard failure) would produce a second.
	if count := strings.Count(final, `"attempt": 1`); count != 1 {
		t.Errorf("expected exactly 1 `attempt=1` log (startup only; reload must skip dispatch), got %d. Output: %s", count, final)
	}
}
