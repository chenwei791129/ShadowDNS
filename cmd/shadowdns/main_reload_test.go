package main

// Tests for the reload-coverage-and-metrics change: GeoIP reload with
// deferred-by-one-generation close, RRL limiter rebuild, query-log re-apply,
// reload metrics, SIGUSR1-after-reload semantics, and the shutdown-order
// contract.
//
// All fixture domains use RFC 2606 names; all paths live under t.TempDir().

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter/mmdbtype"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/querylog"
	"github.com/chenwei791129/ShadowDNS/internal/ratelimit"
	"github.com/chenwei791129/ShadowDNS/internal/server"
)

// ---------------------------------------------------------------------------
// fixture helpers
// ---------------------------------------------------------------------------

// buildDataMMDBs writes country/ASN mmdbs into dir carrying a record for
// 203.0.113.0/24 (TEST-NET-3), so a Lookup distinguishes an open handle
// (returns iso, true) from a closed one (returns "", false).
func buildDataMMDBs(t *testing.T, dir, iso string, asnNum uint32) {
	t.Helper()
	buildMMDBPair(t, dir, "203.0.113.0/24",
		mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String(iso)}},
		mmdbtype.Map{"autonomous_system_number": mmdbtype.Uint32(asnNum)},
	)
}

// patchNamedConfOptions rewrites named.conf from the given original contents,
// inserting extra directives just before "recursion no;" in the options block.
func patchNamedConfOptions(t *testing.T, dir, orig, extra string) {
	t.Helper()
	patched := strings.Replace(orig, "recursion no;", extra+"\n    recursion no;", 1)
	if patched == orig {
		t.Fatal("patchNamedConfOptions: anchor 'recursion no;' not found")
	}
	if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched named.conf: %v", err)
	}
}

// readNamedConf returns the current named.conf contents in dir.
func readNamedConf(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "named.conf"))
	if err != nil {
		t.Fatalf("read named.conf: %v", err)
	}
	return string(data)
}

// attachQueryLog mirrors run()'s startup wiring for tests that call reload()
// directly: it opens the query log named by qlState's current cfg and installs
// the logger and sink on srv / qlState.
func attachQueryLog(t *testing.T, srv *server.Server, qlState *atomic.Pointer[queryLogState]) {
	t.Helper()
	st := qlState.Load()
	if st.cfg == nil {
		t.Fatal("attachQueryLog: fixture named.conf has no logging block")
	}
	lg, sink, err := querylog.New(st.cfg.FilePath, querylog.Config{
		PrintTime:     st.cfg.PrintTime,
		PrintCategory: st.cfg.PrintCategory,
		PrintSeverity: st.cfg.PrintSeverity,
	})
	if err != nil {
		t.Fatalf("attachQueryLog: %v", err)
	}
	srv.QueryLog.Store(lg)
	qlState.Store(&queryLogState{cfg: st.cfg, sink: sink})
	t.Cleanup(func() {
		if cur := qlState.Load(); cur.sink != nil {
			_ = cur.sink.Close()
		}
	})
}

// metricFamilies gathers all metric families from m keyed by name.
func metricFamilies(t *testing.T, m *metrics.Metrics) map[string]*dto.MetricFamily {
	t.Helper()
	mfs, err := m.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	out := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		out[mf.GetName()] = mf
	}
	return out
}

// reloadCounterValue returns shadowdns_reload_total{result=<result>}.
func reloadCounterValue(t *testing.T, m *metrics.Metrics, result string) float64 {
	t.Helper()
	mf, ok := metricFamilies(t, m)["shadowdns_reload_total"]
	if !ok {
		t.Fatal("shadowdns_reload_total not found")
	}
	for _, metric := range mf.GetMetric() {
		for _, l := range metric.GetLabel() {
			if l.GetName() == "result" && l.GetValue() == result {
				return metric.GetCounter().GetValue()
			}
		}
	}
	t.Fatalf("series shadowdns_reload_total{result=%q} not found", result)
	return 0
}

// lastReloadGauge returns shadowdns_config_last_reload_success_timestamp_seconds.
func lastReloadGauge(t *testing.T, m *metrics.Metrics) float64 {
	t.Helper()
	mf, ok := metricFamilies(t, m)["shadowdns_config_last_reload_success_timestamp_seconds"]
	if !ok {
		t.Fatal("last-reload-success gauge not found")
	}
	return mf.GetMetric()[0].GetGauge().GetValue()
}

// geoipGaugeSeries returns database → build_time label values of
// shadowdns_geoip_db_info, asserting at most one series per database.
func geoipGaugeSeries(t *testing.T, m *metrics.Metrics) map[string]string {
	t.Helper()
	out := map[string]string{}
	mf, ok := metricFamilies(t, m)["shadowdns_geoip_db_info"]
	if !ok {
		return out
	}
	for _, metric := range mf.GetMetric() {
		var db, bt string
		for _, l := range metric.GetLabel() {
			switch l.GetName() {
			case "database":
				db = l.GetValue()
			case "build_time":
				bt = l.GetValue()
			}
		}
		if prev, dup := out[db]; dup {
			t.Errorf("database %q has multiple build_time series (%q and %q); stale series must be deleted", db, prev, bt)
		}
		out[db] = bt
	}
	return out
}

// ---------------------------------------------------------------------------
// 9.3 GeoIP reload
// ---------------------------------------------------------------------------

func TestReloadGeoIP(t *testing.T) {
	dir := setupReloadTestDir(t)
	geoDir := filepath.Join(dir, "geoip")
	// Replace the empty fixture mmdbs with data-bearing ones so handle
	// closure is observable through Lookup.
	buildDataMMDBs(t, geoDir, "JP", 64500)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())
	m := metrics.New()
	srv.Metrics = m

	testIP := netip.MustParseAddr("203.0.113.50")
	gen1 := geo.country
	if iso, ok := gen1.Lookup(testIP); !ok || iso != "JP" {
		t.Fatalf("fixture lookup = (%q, %v), want (JP, true)", iso, ok)
	}

	// --- successful reload: new handles installed, old generation parked ---
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if geo.country == gen1 {
		t.Error("geo.country still points at the startup handle; reload must open a new one")
	}
	if geo.prevCountry != gen1 {
		t.Error("superseded country handle is not parked in the deferred-close slot")
	}
	if iso, ok := gen1.Lookup(testIP); !ok || iso != "JP" {
		t.Errorf("superseded handle lookup = (%q, %v); it must remain open until the next reload", iso, ok)
	}

	wantSeries := map[string]string{
		"country": time.Unix(int64(geo.country.Metadata().BuildEpoch), 0).UTC().Format(time.RFC3339),
		"asn":     time.Unix(int64(geo.asn.Metadata().BuildEpoch), 0).UTC().Format(time.RFC3339),
	}
	got := geoipGaugeSeries(t, m)
	for db, want := range wantSeries {
		if got[db] != want {
			t.Errorf("geoip_db_info{database=%q} build_time = %q, want %q", db, got[db], want)
		}
	}

	// --- second reload: generation 1 is closed ---
	gen2 := geo.country
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("second reload: %v", err)
	}
	if geo.prevCountry != gen2 {
		t.Error("generation 2 should now sit in the deferred-close slot")
	}
	if _, ok := gen1.Lookup(testIP); ok {
		t.Error("generation 1 still answers lookups after the second reload; it must be closed")
	}

	// --- GeoIP failure preserves existing handles and counts a failure ---
	cur := geo.country
	failuresBefore := reloadCounterValue(t, m, "failure")
	if err := os.RemoveAll(geoDir); err != nil {
		t.Fatalf("remove geoip dir: %v", err)
	}
	err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop())
	if err == nil {
		t.Fatal("expected reload error when the GeoIP directory is gone")
	}
	if geo.country != cur {
		t.Error("GeoIP handles changed despite reload failure")
	}
	if got := reloadCounterValue(t, m, "failure"); got != failuresBefore+1 {
		t.Errorf("failure counter = %v, want %v", got, failuresBefore+1)
	}

	// --- empty geoip-directory is an explicit configuration error ---
	orig := readNamedConf(t, dir)
	patched := strings.Replace(orig, fmt.Sprintf("geoip-directory %q;", geoDir), `geoip-directory "";`, 1)
	if patched == orig {
		t.Fatal("failed to blank geoip-directory in named.conf fixture")
	}
	if werr := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(patched), 0o644); werr != nil {
		t.Fatalf("write named.conf: %v", werr)
	}
	err = reload(context.Background(), opts, srv, geo, qlState, zap.NewNop())
	if err == nil || !strings.Contains(err.Error(), "geoip-directory") {
		t.Errorf("empty geoip-directory: err = %v, want explicit geoip-directory configuration error", err)
	}
}

// ---------------------------------------------------------------------------
// 9.4 RRL limiter rebuild
// ---------------------------------------------------------------------------

func TestReloadRateLimiter(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())
	m := metrics.New()
	srv.Metrics = m
	orig := readNamedConf(t, dir)

	if srv.RateLimiter.Load() != nil {
		t.Fatal("fixture should start with no rate limiter")
	}

	// --- adding a rate-limit block installs a limiter wired to srv.Metrics ---
	patchNamedConfOptions(t, dir, orig,
		"rate-limit { responses-per-second 1; window 1; slip 0; ipv4-prefix-length 24; ipv6-prefix-length 56; };")
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("reload with rate-limit block: %v", err)
	}
	limiter1 := srv.RateLimiter.Load()
	if limiter1 == nil {
		t.Fatal("limiter not installed after reload")
	}
	// A fresh (empty) credit table allows the first response and drops the
	// second; the drop is recorded through the recorder, proving the rebuilt
	// limiter shares srv.Metrics.
	ip := netip.MustParseAddr("198.51.100.7")
	if got := limiter1.Decide(ip, ratelimit.CategoryResponses, "www.example.com."); got != ratelimit.Allow {
		t.Errorf("first decision = %v, want Allow (credit table must start empty)", got)
	}
	if got := limiter1.Decide(ip, ratelimit.CategoryResponses, "www.example.com."); got == ratelimit.Allow {
		t.Error("second decision = Allow, want over-limit (rps=1)")
	}
	if mf, ok := metricFamilies(t, m)["shadowdns_dns_rate_limit_total"]; !ok || len(mf.GetMetric()) == 0 {
		t.Error("rate-limit decision was not recorded on srv.Metrics; recorder must be preserved across rebuild")
	}

	// --- invalid rate-limit config fails the reload, old limiter unchanged ---
	patchNamedConfOptions(t, dir, orig,
		"rate-limit { responses-per-second 1; exempt-clients { not-an-ip; }; };")
	failuresBefore := reloadCounterValue(t, m, "failure")
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err == nil {
		t.Fatal("expected reload error for invalid exempt-clients entry")
	}
	if srv.RateLimiter.Load() != limiter1 {
		t.Error("limiter changed despite reload failure")
	}
	if got := reloadCounterValue(t, m, "failure"); got != failuresBefore+1 {
		t.Errorf("failure counter = %v, want %v", got, failuresBefore+1)
	}

	// --- removing the rate-limit block disables rate limiting ---
	if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(orig), 0o644); err != nil {
		t.Fatalf("restore named.conf: %v", err)
	}
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("reload without rate-limit block: %v", err)
	}
	if srv.RateLimiter.Load() != nil {
		t.Error("limiter should be nil after the rate-limit block is removed")
	}
}

// ---------------------------------------------------------------------------
// 9.5 / 6.x query-log re-apply state transitions
// ---------------------------------------------------------------------------

func TestReloadQueryLog(t *testing.T) {
	t.Run("Unchanged", func(t *testing.T) {
		qlPath := filepath.Join(t.TempDir(), "queries.log")
		dir := setupReloadTestDir(t)
		addLoggingBlock(t, dir, qlPath)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)
		defer geo.closeAll(zap.NewNop())
		attachQueryLog(t, srv, qlState)

		stBefore := qlState.Load()
		lgBefore := srv.QueryLog.Load()
		inodeBefore := inode(t, qlPath)

		if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if qlState.Load() != stBefore {
			t.Error("qlState replaced although the config is unchanged")
		}
		if srv.QueryLog.Load() != lgBefore {
			t.Error("query-log logger replaced although the config is unchanged")
		}
		if got := inode(t, qlPath); got != inodeBefore {
			t.Errorf("sink inode changed (%d → %d); unchanged config must perform no file operations", inodeBefore, got)
		}
	})

	t.Run("PathChanged", func(t *testing.T) {
		qlPathA := filepath.Join(t.TempDir(), "a.log")
		qlPathB := filepath.Join(t.TempDir(), "b.log")
		dir := setupReloadTestDir(t)
		addLoggingBlock(t, dir, qlPathA)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)
		defer geo.closeAll(zap.NewNop())
		attachQueryLog(t, srv, qlState)
		oldSink := qlState.Load().sink
		lgBefore := srv.QueryLog.Load()

		conf := readNamedConf(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "named.conf"),
			[]byte(strings.ReplaceAll(conf, qlPathA, qlPathB)), 0o644); err != nil {
			t.Fatalf("patch named.conf: %v", err)
		}

		if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if srv.QueryLog.Load() == lgBefore {
			t.Error("logger not replaced after path change")
		}
		if got := qlState.Load().cfg.FilePath; got != qlPathB {
			t.Errorf("qlState path = %q, want %q", got, qlPathB)
		}
		if _, err := os.Stat(qlPathB); err != nil {
			t.Errorf("new sink file not created: %v", err)
		}
		if _, err := oldSink.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
			t.Errorf("old sink Write err = %v, want os.ErrClosed (old sink must be closed)", err)
		}
	})

	t.Run("Removed", func(t *testing.T) {
		qlPath := filepath.Join(t.TempDir(), "queries.log")
		dir := setupReloadTestDir(t)
		origConf := readNamedConf(t, dir) // before the logging block
		addLoggingBlock(t, dir, qlPath)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)
		defer geo.closeAll(zap.NewNop())
		attachQueryLog(t, srv, qlState)
		oldSink := qlState.Load().sink

		if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(origConf), 0o644); err != nil {
			t.Fatalf("restore named.conf: %v", err)
		}
		if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if srv.QueryLog.Load() != nil {
			t.Error("logger should be nil after the logging block is removed")
		}
		st := qlState.Load()
		if st.cfg != nil || st.sink != nil {
			t.Errorf("qlState = %+v, want nil cfg and nil sink", st)
		}
		if _, err := oldSink.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
			t.Errorf("old sink Write err = %v, want os.ErrClosed", err)
		}
	})

	t.Run("Added", func(t *testing.T) {
		qlPath := filepath.Join(t.TempDir(), "queries.log")
		dir := setupReloadTestDir(t)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)
		defer geo.closeAll(zap.NewNop())
		defer func() {
			if st := qlState.Load(); st.sink != nil {
				_ = st.sink.Close()
			}
		}()

		addLoggingBlock(t, dir, qlPath)
		if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if srv.QueryLog.Load() == nil {
			t.Error("logger not installed after the logging block was introduced")
		}
		st := qlState.Load()
		if st.cfg == nil || st.sink == nil {
			t.Fatalf("qlState = %+v, want non-nil cfg and sink", st)
		}
		if _, err := os.Stat(qlPath); err != nil {
			t.Errorf("query log file not created: %v", err)
		}
	})

	t.Run("FailedOpen", func(t *testing.T) {
		qlPath := filepath.Join(t.TempDir(), "queries.log")
		badPath := filepath.Join(t.TempDir(), "missing-subdir", "queries.log")
		dir := setupReloadTestDir(t)
		addLoggingBlock(t, dir, qlPath)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)
		defer geo.closeAll(zap.NewNop())
		attachQueryLog(t, srv, qlState)
		stBefore := qlState.Load()
		lgBefore := srv.QueryLog.Load()
		m := metrics.New()
		srv.Metrics = m

		conf := readNamedConf(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "named.conf"),
			[]byte(strings.ReplaceAll(conf, qlPath, badPath)), 0o644); err != nil {
			t.Fatalf("patch named.conf: %v", err)
		}

		err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop())
		if err == nil {
			t.Fatal("expected reload error for unopenable query-log path")
		}
		if srv.QueryLog.Load() != lgBefore {
			t.Error("logger replaced despite sink open failure")
		}
		if qlState.Load() != stBefore {
			t.Error("qlState replaced despite sink open failure")
		}
		if got := reloadCounterValue(t, m, "failure"); got != 1 {
			t.Errorf("failure counter = %v, want 1", got)
		}
		if _, serr := stBefore.sink.Write([]byte("still writable\n")); serr != nil {
			t.Errorf("previous sink must remain usable, Write err = %v", serr)
		}
	})

	t.Run("RotationWarning", func(t *testing.T) {
		qlPath := filepath.Join(t.TempDir(), "queries.log")
		dir := setupReloadTestDir(t)
		origConf := readNamedConf(t, dir)
		addLoggingBlock(t, dir, qlPath)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)
		defer geo.closeAll(zap.NewNop())
		attachQueryLog(t, srv, qlState)
		oldSink := qlState.Load().sink

		// Rotation-only change: same path and print options, versions/size added.
		if err := os.WriteFile(filepath.Join(dir, "named.conf"), []byte(origConf), 0o644); err != nil {
			t.Fatalf("restore named.conf: %v", err)
		}
		addLoggingBlock(t, dir, qlPath, "versions 3 size 5m")

		buf := &threadSafeBuffer{}
		logger := newBufferLogger(zapcore.AddSync(buf))
		if err := reload(context.Background(), opts, srv, geo, qlState, logger); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if !strings.Contains(buf.String(), "rotation parameters") {
			t.Error("rotation-ignored warning was not re-emitted on the rotation-only change path")
		}
		if qlState.Load().sink == oldSink {
			t.Error("rotation-only change must fall on the replace path (new sink)")
		}
		if !qlState.Load().cfg.RotationIgnored {
			t.Error("qlState.cfg.RotationIgnored not updated")
		}

		// Unchanged follow-up reload must not re-warn.
		buf2 := &threadSafeBuffer{}
		logger2 := newBufferLogger(zapcore.AddSync(buf2))
		if err := reload(context.Background(), opts, srv, geo, qlState, logger2); err != nil {
			t.Fatalf("second reload: %v", err)
		}
		if strings.Contains(buf2.String(), "rotation parameters") {
			t.Error("rotation-ignored warning re-emitted although the config is unchanged")
		}
	})
}

// ---------------------------------------------------------------------------
// 9.6 reload metrics integration
// ---------------------------------------------------------------------------

func TestReloadMetrics_Integration(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())
	m := metrics.New()
	srv.Metrics = m

	// --- success path: success counter +1, gauge set to ~now ---
	before := time.Now()
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloadCounterValue(t, m, "success"); got != 1 {
		t.Errorf("success counter = %v, want 1", got)
	}
	if got := reloadCounterValue(t, m, "failure"); got != 0 {
		t.Errorf("failure counter = %v, want 0", got)
	}
	gauge := lastReloadGauge(t, m)
	if diff := gauge - float64(before.Unix()); diff < 0 || diff > 2 {
		t.Errorf("gauge = %v, want within 2s after %v", gauge, before.Unix())
	}

	// --- failure path: failure counter +1, success counter and gauge frozen ---
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte("NOT A ZONE"), 0o644); err != nil {
		t.Fatalf("break zone: %v", err)
	}
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err == nil {
		t.Fatal("expected reload failure for broken zone")
	}
	if got := reloadCounterValue(t, m, "failure"); got != 1 {
		t.Errorf("failure counter = %v, want 1", got)
	}
	if got := reloadCounterValue(t, m, "success"); got != 1 {
		t.Errorf("success counter = %v, want 1 (unchanged)", got)
	}
	if got := lastReloadGauge(t, m); got != gauge {
		t.Errorf("gauge = %v after failed reload, want unchanged %v", got, gauge)
	}
}

// TestReloadMetrics_StartupGaugeInitialised covers the startup-initialisation
// clause: before any SIGHUP, the last-reload-success gauge equals the startup
// time, not 0.
func TestReloadMetrics_StartupGaugeInitialised(t *testing.T) {
	dir := setupReloadTestDir(t)
	metricsAddr := freePortAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readyCh := make(chan struct{})
	opts := runOptions{
		NamedConfPath: filepath.Join(dir, "named.conf"),
		ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
		ListenAddr:    "127.0.0.1:0",
		MetricsAddr:   metricsAddr,
		Logger:        zap.NewNop(),
		ReadyCh:       readyCh,
	}
	start := time.Now()

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()
	waitReady(t, readyCh)

	// Scrape /metrics once the HTTP listener accepts connections.
	waitHTTPReady(t, "http://"+metricsAddr)
	resp, err := http.Get("http://" + metricsAddr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	body := string(b)

	var gauge float64
	found := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "shadowdns_config_last_reload_success_timestamp_seconds ") {
			if _, err := fmt.Sscanf(line, "shadowdns_config_last_reload_success_timestamp_seconds %g", &gauge); err == nil {
				found = true
			}
			break
		}
	}
	if !found {
		t.Fatalf("gauge not found in /metrics output:\n%s", body)
	}
	if gauge == 0 {
		t.Error("gauge = 0 before first SIGHUP; it must be initialised to the startup time")
	}
	if diff := gauge - float64(start.Unix()); diff < -2 || diff > 5 {
		t.Errorf("gauge = %v, want within a few seconds of startup %v", gauge, start.Unix())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2s after cancel")
	}
}

// ---------------------------------------------------------------------------
// 9.7 SIGUSR1 after reload
// ---------------------------------------------------------------------------

func TestSigusr1AfterReload(t *testing.T) {
	t.Run("path change redirects reopen to the new sink", func(t *testing.T) {
		mainLog := filepath.Join(t.TempDir(), "main.log")
		qlPathA := filepath.Join(t.TempDir(), "a.log")
		qlPathB := filepath.Join(t.TempDir(), "b.log")
		dir := setupReloadTestDir(t)
		addLoggingBlock(t, dir, qlPathA)

		cancel, done := startServerWithQueryLog(t, dir, mainLog, qlPathA)
		defer cancel()

		// Reload onto path B.
		conf := readNamedConf(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "named.conf"),
			[]byte(strings.ReplaceAll(conf, qlPathA, qlPathB)), 0o644); err != nil {
			t.Fatalf("patch named.conf: %v", err)
		}
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("send SIGHUP: %v", err)
		}
		waitForFile(t, qlPathB)

		// The retired path-A sink is closed; remove its file so any
		// erroneous reopen of the old sink would be visible as a recreation.
		if err := os.Remove(qlPathA); err != nil {
			t.Fatalf("remove old query log: %v", err)
		}
		// Rotate B and ask for reopen.
		if err := os.Rename(qlPathB, qlPathB+".1"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
			t.Fatalf("send SIGUSR1: %v", err)
		}
		waitForFile(t, qlPathB)
		if _, err := os.Stat(qlPathA); err == nil {
			t.Error("old query log path was recreated by SIGUSR1; the retired sink must not be touched")
		}

		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("run did not exit within 2s after cancel")
		}
	})

	t.Run("query log introduced by reload is reopenable", func(t *testing.T) {
		qlPath := filepath.Join(t.TempDir(), "queries.log")
		dir := setupReloadTestDir(t) // no logging block at startup

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		readyCh := make(chan struct{})
		opts := runOptions{
			NamedConfPath: filepath.Join(dir, "named.conf"),
			ConfigPath:    filepath.Join(dir, "shadowdns.yaml"),
			ListenAddr:    freePortAddr(t),
			Logger:        zap.NewNop(),
			// No LogReopener and no query log at startup: registration must
			// not depend on a startup sink.
			ReadyCh: readyCh,
		}
		done := make(chan error, 1)
		go func() { done <- run(ctx, opts) }()
		waitReady(t, readyCh)

		// Introduce the query log via reload.
		addLoggingBlock(t, dir, qlPath)
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("send SIGHUP: %v", err)
		}
		waitForFile(t, qlPath)

		// Rotate it and confirm SIGUSR1 reopens the reload-created sink.
		if err := os.Rename(qlPath, qlPath+".1"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
			t.Fatalf("send SIGUSR1: %v", err)
		}
		waitForFile(t, qlPath)

		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("run did not exit within 2s after cancel")
		}
	})
}

// waitForFile polls until path exists or fails the test after 2s.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s did not appear within 2s", path)
}

// ---------------------------------------------------------------------------
// 9.8 reload with metrics disabled
// ---------------------------------------------------------------------------

func TestReloadNoMetrics(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())
	if srv.Metrics != nil {
		t.Fatal("fixture must run with metrics disabled")
	}

	// Success path: swaps state, no panic on the nil-receiver metrics calls.
	prevState := srv.CurrentState()
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("reload with metrics disabled: %v", err)
	}
	if srv.CurrentState() == prevState {
		t.Error("state not swapped by successful reload")
	}

	// Failure path: preserves state, still no panic.
	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte("NOT A ZONE"), 0o644); err != nil {
		t.Fatalf("break zone: %v", err)
	}
	prevState = srv.CurrentState()
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err == nil {
		t.Fatal("expected reload failure for broken zone")
	}
	if srv.CurrentState() != prevState {
		t.Error("state swapped despite reload failure")
	}
}

// ---------------------------------------------------------------------------
// 8.2 shutdown-order contract
// ---------------------------------------------------------------------------

func TestShutdownDuringReload(t *testing.T) {
	t.Run("parent ctx cancelled while reload runs", func(t *testing.T) {
		dir := setupReloadTestDir(t)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)

		ctx, cancel := context.WithCancel(context.Background())
		shutdown := runSignalHandlers(ctx, opts, srv, geo, qlState, zap.NewNop())

		// Race a reload against the shutdown sequence.
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("send SIGHUP: %v", err)
		}
		cancel()

		finished := make(chan struct{})
		go func() { shutdown(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("shutdown did not complete within 5s")
		}

		// After the join, closing the resources must be race-free and safe.
		geo.closeAll(zap.NewNop())
		if st := qlState.Load(); st.sink != nil {
			_ = st.sink.Close()
		}
		// closeAll clears its fields; a second call must be a no-op.
		geo.closeAll(zap.NewNop())
	})

	t.Run("listener error path: parent ctx alive, shutdown must not deadlock", func(t *testing.T) {
		dir := setupReloadTestDir(t)
		srv, geo, qlState, opts := startReloadTestServer(t, dir)

		// Parent ctx stays alive — mimicking srv.Serve returning because a
		// listener died. Only the shutdown sequence cancels the child context.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		shutdown := runSignalHandlers(ctx, opts, srv, geo, qlState, zap.NewNop())

		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("send SIGHUP: %v", err)
		}

		finished := make(chan struct{})
		go func() { shutdown(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("shutdown deadlocked on the listener-error path (child context not cancelled?)")
		}

		geo.closeAll(zap.NewNop())
		if st := qlState.Load(); st.sink != nil {
			_ = st.sink.Close()
		}
	})
}
