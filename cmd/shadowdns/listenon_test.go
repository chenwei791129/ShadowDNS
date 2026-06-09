package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// setupListenOnTestDir builds a reload-test fixture via setupReloadTestDir
// and rewrites its listen-on directive to the caller-supplied token list.
// Returns the named.conf path.
func setupListenOnTestDir(t *testing.T, listenOnTokens string) string {
	t.Helper()
	dir := setupReloadTestDir(t)
	namedConf := filepath.Join(dir, "named.conf")
	data, err := os.ReadFile(namedConf)
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	const origListen = "listen-on { any; };"
	newListen := "listen-on " + listenOnTokens + ";"
	patched := strings.Replace(string(data), origListen, newListen, 1)
	if patched == string(data) {
		t.Fatalf("setupReloadTestDir named.conf does not contain %q; helper out of sync", origListen)
	}
	if err := os.WriteFile(namedConf, []byte(patched), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}
	return namedConf
}

// observer is internally synchronized, so writes from run()'s goroutines
// and reads from the test goroutine are safe under -race.
func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, recorded := observer.New(zapcore.DebugLevel)
	return zap.New(core), recorded
}

func formatObserved(logs *observer.ObservedLogs) string {
	var sb strings.Builder
	for _, e := range logs.All() {
		sb.WriteString(e.Message)
		for k, v := range e.ContextMap() {
			fmt.Fprintf(&sb, " %s=%v", k, v)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// TestRun_OverrideBranchUsesListenFlag: --listen carries a specific host, so
// listen-on in named.conf must be ignored. The run() must start successfully
// and the bound address must be the one from --listen.
func TestRun_OverrideBranchUsesListenFlag(t *testing.T) {
	namedConf := setupListenOnTestDir(t, `{ 10.255.255.255; }`) // unreachable IP
	ctx, cancel := context.WithCancel(context.Background())
	logger, observed := newObservedLogger()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(filepath.Dir(namedConf), "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0", // has host component → override
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	waitReady(t, readyCh)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run failed under override branch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s")
	}

	logOut := formatObserved(observed)
	// Override path must NOT have attempted to bind 10.255.255.255.
	if strings.Contains(logOut, "10.255.255.255") {
		t.Errorf("override path should ignore listen-on address, but log mentions it: %s", logOut)
	}
	// Must have successfully bound something on 127.0.0.1.
	if observed.FilterMessage("listener bound").Len() == 0 {
		t.Errorf("should have logged a successful bind, got: %s", logOut)
	}
}

// TestRun_ListenOnBranchBindsListenOnAddresses: --listen is :PORT form, so
// named.conf's listen-on drives the host. The bound address must come from
// listen-on, with the port from --listen.
func TestRun_ListenOnBranchBindsListenOnAddresses(t *testing.T) {
	namedConf := setupListenOnTestDir(t, `{ 127.0.0.1; }`)
	ctx, cancel := context.WithCancel(context.Background())
	logger, observed := newObservedLogger()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(filepath.Dir(namedConf), "shadowdns.yaml"),
		ListenAddr:    ":0", // port hint only; listen-on drives host
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	waitReady(t, readyCh)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run failed under listen-on branch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s")
	}

	logOut := formatObserved(observed)
	// Must have bound on 127.0.0.1 (from listen-on), not wildcard.
	if !strings.Contains(logOut, "127.0.0.1") {
		t.Errorf("listen-on branch should bind on 127.0.0.1, log: %s", logOut)
	}
	if observed.FilterMessage("listener bound").Len() == 0 {
		t.Errorf("should have logged a successful bind, got: %s", logOut)
	}
}

// TestRun_ReloadLogsListenAddrChangeHint: when named.conf's listen-on
// changes between startup and SIGHUP reload, reload() must log a hint that
// restart is required; it must NOT re-bind listeners.
func TestRun_ReloadLogsListenAddrChangeHint(t *testing.T) {
	// Start with listen-on { 127.0.0.1; }.
	namedConf := setupListenOnTestDir(t, `{ 127.0.0.1; }`)
	logger, observed := newObservedLogger()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(filepath.Dir(namedConf), "shadowdns.yaml"),
		ListenAddr:    ":0",
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	waitReady(t, readyCh)

	// Rewrite named.conf to change listen-on.
	dir := filepath.Dir(namedConf)
	masterZones := filepath.Join(dir, "master.zones")
	geoIPDir := filepath.Join(dir, "geoip")
	newContent := `options {
    directory "` + dir + `";
    geoip-directory "` + geoIPDir + `";
    listen-on { 127.0.0.2; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(newContent), 0o644); err != nil {
		t.Fatalf("rewrite named.conf: %v", err)
	}

	// Fire SIGHUP.
	if err := sendSIGHUPToSelf(); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-done

	logOut := formatObserved(observed)
	if !strings.Contains(logOut, "restart") {
		t.Errorf("expected reload log to mention 'restart', got: %s", logOut)
	}
}

// TestRun_ReloadLogsListenAddrChangeHint_IPv6 is the IPv6 analogue of the test
// above: it proves SIGHUP reload drift detection covers listen-on-v6, mirroring
// the v4 case while logging the same restart hint and not re-binding.
//
// Note on addresses: the spec scenario phrases the drift as 2001:db8::1 →
// 2001:db8::2, but neither documentation-prefix address is assigned to a local
// interface, so a startup bind on them would fail and the server would never
// serve them. To exercise the full bind→reload→drift path with a real v6
// listener, startup uses the always-bindable loopback ::1; the reload then
// rewrites listen-on-v6 to 2001:db8::1, which only needs to *resolve* (not
// bind) for drift detection to fire. This preserves the contract intent:
// drift detection covers IPv6 and reload does not re-bind.
func TestRun_ReloadLogsListenAddrChangeHint_IPv6(t *testing.T) {
	dir := setupReloadTestDir(t)
	namedConf := filepath.Join(dir, "named.conf")
	masterZones := filepath.Join(dir, "master.zones")
	geoIPDir := filepath.Join(dir, "geoip")

	// Start with listen-on { 127.0.0.1; } and listen-on-v6 { ::1; } so both a
	// v4 and a v6 listener actually bind.
	startContent := `options {
    directory "` + dir + `";
    geoip-directory "` + geoIPDir + `";
    listen-on { 127.0.0.1; };
    listen-on-v6 { ::1; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(startContent), 0o644); err != nil {
		t.Fatalf("write start named.conf: %v", err)
	}

	logger, observed := newObservedLogger()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: namedConf,
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    ":0",
		Logger:        logger,
		ReadyCh:       readyCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	waitReady(t, readyCh)

	// The v6 loopback listener must have bound end-to-end at startup.
	startupLog := formatObserved(observed)
	if !strings.Contains(startupLog, "[::1]") {
		t.Errorf("expected a bound v6 listener on [::1], log: %s", startupLog)
	}

	// Rewrite listen-on-v6 to a different (resolvable, non-local) v6 address.
	reloadContent := `options {
    directory "` + dir + `";
    geoip-directory "` + geoIPDir + `";
    listen-on { 127.0.0.1; };
    listen-on-v6 { 2001:db8::1; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(reloadContent), 0o644); err != nil {
		t.Fatalf("rewrite named.conf: %v", err)
	}

	if err := sendSIGHUPToSelf(); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-done

	logOut := formatObserved(observed)
	// Drift across the v6 family must trigger the restart hint.
	if !strings.Contains(logOut, "restart") {
		t.Errorf("expected reload log to mention 'restart' for v6 drift, got: %s", logOut)
	}
	// The reload must NOT have re-bound to the new v6 address: no "listener
	// bound" entry should reference 2001:db8::1. (The drift INFO line itself
	// does mention it under new_resolved — that is the report, not a rebind —
	// so we inspect bind events specifically rather than the whole log text.)
	for _, e := range observed.FilterMessage("listener bound").All() {
		if addr, ok := e.ContextMap()["addr"].(string); ok && strings.Contains(addr, "2001:db8::1") {
			t.Errorf("reload must not re-bind; found a bind on the new v6 address: %s", addr)
		}
	}
}

// sendSIGHUPToSelf raises SIGHUP on the current process, as a proxy for an
// operator triggering the reload path.
func sendSIGHUPToSelf() error {
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGHUP)
}
