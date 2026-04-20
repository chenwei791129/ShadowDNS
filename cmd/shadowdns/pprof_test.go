package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func runServerAsync(t *testing.T, opts runOptions) (cancel func()) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// 150 ms covers config load + zone parse + DNS/HTTP listener bind for the
	// minimal fixture used here. Shorter delays race the HTTP bind under the
	// race detector.
	time.Sleep(150 * time.Millisecond)

	return func() {
		stop()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("run returned: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("run did not return within 2s after cancel")
		}
	}
}

func httpGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, body
}

func TestPProf_DisabledByDefault(t *testing.T) {
	dir := setupReloadTestDir(t)
	addr := freePort(t)

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   addr,
		Logger:        zap.NewNop(),
	}

	teardown := runServerAsync(t, opts)
	defer teardown()

	metricsURL := "http://" + addr + "/metrics"
	if code, _ := httpGet(t, metricsURL); code != http.StatusOK {
		t.Errorf("GET /metrics: got %d, want 200", code)
	}

	pprofURL := "http://" + addr + "/debug/pprof/"
	if code, _ := httpGet(t, pprofURL); code != http.StatusNotFound {
		t.Errorf("GET /debug/pprof/ with pprof disabled: got %d, want 404", code)
	}
}

func TestPProf_EnabledViaFlag(t *testing.T) {
	dir := setupReloadTestDir(t)
	addr := freePort(t)

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   addr,
		PProfEnable:   true,
		Logger:        zap.NewNop(),
	}

	teardown := runServerAsync(t, opts)
	defer teardown()

	indexURL := "http://" + addr + "/debug/pprof/"
	code, body := httpGet(t, indexURL)
	if code != http.StatusOK {
		t.Errorf("GET /debug/pprof/: got %d, want 200", code)
	}
	if !strings.Contains(string(body), "Types of profiles available") {
		t.Errorf("pprof index body missing expected heading; got %d bytes", len(body))
	}

	heapURL := "http://" + addr + "/debug/pprof/heap"
	code, heap := httpGet(t, heapURL)
	if code != http.StatusOK {
		t.Errorf("GET /debug/pprof/heap: got %d, want 200", code)
	}
	// pprof binary payload is a gzipped protobuf: magic bytes 0x1f 0x8b.
	if len(heap) < 2 || heap[0] != 0x1f || heap[1] != 0x8b {
		t.Errorf("/debug/pprof/heap did not return gzipped pprof protobuf (first bytes: % x)", heap[:min(len(heap), 4)])
	}

	goroutineURL := "http://" + addr + "/debug/pprof/goroutine?debug=1"
	code, goroutine := httpGet(t, goroutineURL)
	if code != http.StatusOK {
		t.Errorf("GET /debug/pprof/goroutine?debug=1: got %d, want 200", code)
	}
	if !strings.Contains(string(goroutine), "goroutine profile") {
		t.Errorf("goroutine text dump missing expected header")
	}
}

func TestPProf_SymbolResolvesPC(t *testing.T) {
	dir := setupReloadTestDir(t)
	addr := freePort(t)

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   addr,
		PProfEnable:   true,
		Logger:        zap.NewNop(),
	}

	teardown := runServerAsync(t, opts)
	defer teardown()

	// Use a live PC from this test binary so runtime.FuncForPC resolves to
	// a real symbol rather than returning nil.
	pc := reflect.ValueOf(TestPProf_SymbolResolvesPC).Pointer()
	url := fmt.Sprintf("http://%s/debug/pprof/symbol?%#x", addr, pc)

	code, body := httpGet(t, url)
	if code != http.StatusOK {
		t.Fatalf("GET %s: got %d, want 200", url, code)
	}
	txt := string(body)
	if !strings.Contains(txt, "num_symbols: 1") {
		t.Errorf("symbol response missing num_symbols: 1 header; got:\n%s", firstLines(txt, 4))
	}
	if !strings.Contains(txt, "TestPProf_SymbolResolvesPC") {
		t.Errorf("symbol response did not resolve PC to caller's name; got:\n%s", firstLines(txt, 4))
	}
}

func TestPProf_ConflictingFlags_FatalStartupError(t *testing.T) {
	dir := setupReloadTestDir(t)

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   "",
		PProfEnable:   true,
		Logger:        zap.NewNop(),
	}

	err := run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for -pprof-enable with empty -metrics-addr, got nil")
	}
	if !strings.Contains(err.Error(), "pprof") || !strings.Contains(err.Error(), "metrics-addr") {
		t.Errorf("error message should explain the conflict; got: %v", err)
	}
}

func TestPProf_DefaultServeMuxNotPolluted(t *testing.T) {
	dir := setupReloadTestDir(t)
	addr := freePort(t)

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   addr,
		PProfEnable:   true,
		Logger:        zap.NewNop(),
	}

	teardown := runServerAsync(t, opts)
	defer teardown()

	// A second HTTP server wrapping http.DefaultServeMux. If any import had
	// pulled net/http/pprof's init() side effects, this mux would answer 200
	// for /debug/pprof/ instead of the expected 404.
	probe := httptest.NewServer(http.DefaultServeMux)
	defer probe.Close()

	code, _ := httpGet(t, probe.URL+"/debug/pprof/")
	if code != http.StatusNotFound {
		t.Errorf("DefaultServeMux answered %d for /debug/pprof/; expected 404 (blank import leaked)", code)
	}
}

func TestPProf_BlockAndMutexProfilesEmpty(t *testing.T) {
	dir := setupReloadTestDir(t)
	addr := freePort(t)

	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   addr,
		PProfEnable:   true,
		Logger:        zap.NewNop(),
	}

	teardown := runServerAsync(t, opts)
	defer teardown()

	// runtime/pprof prints the block profile under its internal name
	// "contention"; mutex stays as "mutex".
	cases := []struct{ path, marker string }{
		{"block", "contention"},
		{"mutex", "mutex"},
	}
	for _, c := range cases {
		url := "http://" + addr + "/debug/pprof/" + c.path + "?debug=1"
		code, body := httpGet(t, url)
		if code != http.StatusOK {
			t.Errorf("GET /debug/pprof/%s?debug=1: got %d, want 200", c.path, code)
			continue
		}
		txt := string(body)
		if !strings.Contains(txt, c.marker) {
			t.Errorf("%s debug=1 dump missing %q header; got:\n%s", c.path, c.marker, firstLines(txt, 3))
		}
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
