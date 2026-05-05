package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/api"
	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// writeShadowConf replaces the shadowdns.yaml in dir with the given contents.
func writeShadowConf(t *testing.T, dir, contents string) {
	t.Helper()
	path := filepath.Join(dir, "shadowdns.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}
}

// freePortAddr reserves an OS-assigned TCP port and returns the 127.0.0.1:PORT
// address. The listener is closed before returning so the caller can bind to
// the same port (small TOCTOU window but acceptable for tests).
func freePortAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// startEphemeralFixture starts a full ShadowDNS test server with an ephemeral
// store and an in-process HTTP API. Returns the DNS server, API base URL,
// ephemeral store, and cleanup func.
func startEphemeralFixture(t *testing.T, dir string) (*server.Server, string, *ephemeral.Store, *view.CountryDB, *view.ASNDB, runOptions, func()) {
	t.Helper()
	srv, country, asn, opts := startReloadTestServer(t, dir)

	// Load the unified config and ensure an ephemeral_api section is set so
	// the API server is started. Overwrite the fixture shadowdns.yaml to
	// include an ephemeral_api section bound to a free local port.
	apiAddr := freePortAddr(t)
	writeShadowConf(t, dir, fmt.Sprintf(`aliases: {}
ephemeral_api:
  listen: %q
  allow:
    - 127.0.0.1
`, apiAddr))

	shadowCfg, err := shadowdnscfg.Load(opts.ConfigPath, zap.NewNop())
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}

	store := ephemeral.NewStore()
	srv.EphemeralStore = store

	zoneLister := func() []string {
		st := srv.CurrentState()
		if st == nil {
			return nil
		}
		return st.AllOrigins()
	}
	apiSrv := api.NewServer(shadowCfg.EphemeralAPI, store, zoneLister, zap.NewNop())
	apiCtx, apiCancel := context.WithCancel(context.Background())
	apiDone := make(chan struct{})
	go func() {
		defer close(apiDone)
		_ = apiSrv.Run(apiCtx)
	}()

	// Wait briefly for the API listener to bind.
	baseURL := "http://" + apiAddr
	waitHTTPReady(t, baseURL)

	cleanup := func() {
		apiCancel()
		select {
		case <-apiDone:
		case <-time.After(6 * time.Second):
		}
		_ = country.Close()
		_ = asn.Close()
	}
	return srv, baseURL, store, country, asn, opts, cleanup
}

// waitHTTPReady polls the given base URL until a TCP connection succeeds or
// the timeout elapses. It does not care about HTTP status — only that the
// port is accepting connections.
func waitHTTPReady(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", baseURL[len("http://"):], 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("API server did not become ready at %s", baseURL)
}

func httpPutJSON(t *testing.T, url, body string) int {
	t.Helper()
	req, err := http.NewRequest("PUT", url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode
}

func httpDelete(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		t.Fatalf("NewRequest DELETE: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestEphemeralTxtApi_EndToEnd covers task 7.1: PUT via API → DNS query
// returns the record → after TTL expiry, DNS query returns empty.
func TestEphemeralTxtApi_EndToEnd(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, baseURL, _, _, _, _, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	// PUT a TXT record with TTL 1 so expiration happens within the test.
	code := httpPutJSON(t, baseURL+"/v1/txt/_acme-challenge.example.com",
		`{"value":"challenge-xyz","ttl":1}`)
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", code)
	}

	// DNS query should return the TXT record.
	resp := reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 TXT answer, got %d", len(resp.Answer))
	}
	if txt := resp.Answer[0].(*dns.TXT); txt.Txt[0] != "challenge-xyz" {
		t.Errorf("TXT value = %q, want challenge-xyz", txt.Txt[0])
	}

	// Wait for the TTL (1 s) to elapse; lazy eviction will drop the record.
	time.Sleep(1200 * time.Millisecond)

	// DNS query should now return NXDOMAIN.
	resp = reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("after expiry: Rcode = %s, want NXDOMAIN", dns.RcodeToString[resp.Rcode])
	}
}

// TestEphemeralTxtApi_MultiValueEndToEnd covers task 8.8: two PUTs under the
// same FQDN with distinct values, verify both appear as separate TXT RRs,
// then a single DELETE wipes all of them.
func TestEphemeralTxtApi_MultiValueEndToEnd(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, baseURL, _, _, _, _, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	code := httpPutJSON(t, baseURL+"/v1/txt/_acme-challenge.example.com",
		`{"value":"token-apex","ttl":120}`)
	if code != http.StatusOK {
		t.Fatalf("PUT 1 status = %d", code)
	}
	code = httpPutJSON(t, baseURL+"/v1/txt/_acme-challenge.example.com",
		`{"value":"token-wildcard","ttl":120}`)
	if code != http.StatusOK {
		t.Fatalf("PUT 2 status = %d", code)
	}

	resp := reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if len(resp.Answer) != 2 {
		t.Fatalf("expected 2 TXT RRs in answer, got %d: %v", len(resp.Answer), resp.Answer)
	}
	values := map[string]bool{}
	for _, rr := range resp.Answer {
		values[rr.(*dns.TXT).Txt[0]] = true
	}
	if !values["token-apex"] || !values["token-wildcard"] {
		t.Errorf("expected both apex + wildcard tokens in answer; got %v", values)
	}

	// Single DELETE must wipe both entries.
	if code := httpDelete(t, baseURL+"/v1/txt/_acme-challenge.example.com"); code != http.StatusOK {
		t.Fatalf("DELETE status = %d", code)
	}

	resp = reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("after DELETE: Rcode = %s, want NXDOMAIN (every entry must be cleared)", dns.RcodeToString[resp.Rcode])
	}
}

// TestEphemeralTxtApi_PerValueDeleteEndToEnd verifies the ?value= selector:
// PUT two values, delete one by value, only the other remains visible via
// DNS; then wipe-all DELETE clears the FQDN entirely.
func TestEphemeralTxtApi_PerValueDeleteEndToEnd(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, baseURL, _, _, _, _, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	if code := httpPutJSON(t, baseURL+"/v1/txt/_acme-challenge.example.com",
		`{"value":"token-A","ttl":120}`); code != http.StatusOK {
		t.Fatalf("PUT 1 status = %d", code)
	}
	if code := httpPutJSON(t, baseURL+"/v1/txt/_acme-challenge.example.com",
		`{"value":"token-B","ttl":120}`); code != http.StatusOK {
		t.Fatalf("PUT 2 status = %d", code)
	}

	if code := httpDelete(t, baseURL+"/v1/txt/_acme-challenge.example.com?value=token-A"); code != http.StatusOK {
		t.Fatalf("DELETE ?value=token-A status = %d", code)
	}

	resp := reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if len(resp.Answer) != 1 {
		t.Fatalf("after per-value delete: expected 1 RR, got %d: %v", len(resp.Answer), resp.Answer)
	}
	if txt := resp.Answer[0].(*dns.TXT); txt.Txt[0] != "token-B" {
		t.Errorf("surviving TXT = %q, want token-B", txt.Txt[0])
	}

	if code := httpDelete(t, baseURL+"/v1/txt/_acme-challenge.example.com"); code != http.StatusOK {
		t.Fatalf("DELETE wipe-all status = %d", code)
	}
	resp = reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("after wipe-all: Rcode = %s, want NXDOMAIN", dns.RcodeToString[resp.Rcode])
	}
}

// TestEphemeralTxtApi_PerValueDeleteLastEntryClearsFQDN verifies that when
// ?value= removes the last remaining entry, the FQDN is fully cleared and
// DNS returns NXDOMAIN (no empty-slice leak in the store).
func TestEphemeralTxtApi_PerValueDeleteLastEntryClearsFQDN(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, baseURL, _, _, _, _, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	if code := httpPutJSON(t, baseURL+"/v1/txt/_acme-challenge.example.com",
		`{"value":"only","ttl":120}`); code != http.StatusOK {
		t.Fatalf("PUT status = %d", code)
	}

	if code := httpDelete(t, baseURL+"/v1/txt/_acme-challenge.example.com?value=only"); code != http.StatusOK {
		t.Fatalf("DELETE ?value=only status = %d", code)
	}

	resp := reloadQuery(t, srv, "_acme-challenge.example.com.", dns.TypeTXT)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("after per-value delete of last entry: Rcode = %s, want NXDOMAIN", dns.RcodeToString[resp.Rcode])
	}
}

// TestEphemeralTxtApi_ReloadFailsOnAliasesKeepsOldState covers task 7.2:
// a reload with an invalid aliases section must not swap state and must not
// clear the ephemeral store.
func TestEphemeralTxtApi_ReloadFailsOnAliasesKeepsOldState(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, _, store, country, asn, opts, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	// Seed an ephemeral record so we can detect whether reload cleared it.
	store.Put("_acme.example.com.", "v", 300)
	prevState := srv.CurrentState()

	// Write a broken aliases section (self-alias is invalid).
	writeShadowConf(t, dir, `
aliases:
  loop.example.com:
    members:
      - loop.example.com
ephemeral_api:
  listen: "127.0.0.1:18053"
  allow:
    - 127.0.0.1
`)

	err := reload(context.Background(), opts, srv, country, asn, zap.NewNop())
	if err == nil {
		t.Fatal("expected reload error for invalid aliases section")
	}

	if srv.CurrentState() != prevState {
		t.Error("state was swapped despite reload error")
	}
	if _, ok := store.Lookup("_acme.example.com."); !ok {
		t.Error("ephemeral record was cleared despite reload error")
	}
}

// TestEphemeralTxtApi_ReloadFailsOnEphemeralApiKeepsOldState covers task 7.3.
func TestEphemeralTxtApi_ReloadFailsOnEphemeralApiKeepsOldState(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, _, store, country, asn, opts, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	store.Put("_acme.example.com.", "v", 300)
	prevState := srv.CurrentState()

	// Invalid CIDR in allow list.
	writeShadowConf(t, dir, `
aliases: {}
ephemeral_api:
  listen: "127.0.0.1:18054"
  allow:
    - "not-an-ip"
`)

	err := reload(context.Background(), opts, srv, country, asn, zap.NewNop())
	if err == nil {
		t.Fatal("expected reload error for invalid ephemeral_api section")
	}
	if srv.CurrentState() != prevState {
		t.Error("state was swapped despite reload error")
	}
	if _, ok := store.Lookup("_acme.example.com."); !ok {
		t.Error("ephemeral record was cleared despite reload error")
	}
}

// TestEphemeralTxtApi_ReloadAllValidClearsStore covers task 7.4: a successful
// reload atomically swaps state and clears the ephemeral store.
func TestEphemeralTxtApi_ReloadAllValidClearsStore(t *testing.T) {
	dir := setupReloadTestDir(t)
	srv, _, store, country, asn, opts, cleanup := startEphemeralFixture(t, dir)
	defer cleanup()

	store.Put("_acme.example.com.", "v", 300)
	prevState := srv.CurrentState()

	// Valid new config with an added alias.
	writeShadowConf(t, dir, `
aliases:
  example.com:
    members:
      - backup.example.com
ephemeral_api:
  listen: "127.0.0.1:18055"
  allow:
    - 127.0.0.1
`)

	if err := reload(context.Background(), opts, srv, country, asn, zap.NewNop()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if srv.CurrentState() == prevState {
		t.Error("state was not swapped after successful reload")
	}
	if got := srv.CurrentState().Aliases["backup.example.com."]; got != "example.com." {
		t.Errorf("new alias map missing entry; got map = %+v", srv.CurrentState().Aliases)
	}
	if _, ok := store.Lookup("_acme.example.com."); ok {
		t.Error("ephemeral record was NOT cleared after successful reload")
	}
}

// TestEphemeralTxtApi_ZoneListerSnapshotIsDynamic exercises the spec scenarios
// "Zone added via SIGHUP reload becomes acceptable on the next PUT" and the
// symmetric removal case. It starts an API server with a mutable zone lister,
// which models what the real lister does (read state.RootZones/BackupZones
// on every call) without needing to craft two named.conf files. Mutation is
// guarded by atomic.Pointer so the test stays race-detector clean.
func TestEphemeralTxtApi_ZoneListerSnapshotIsDynamic(t *testing.T) {
	apiAddr := freePortAddr(t)

	var zonesPtr atomic.Pointer[[]string]
	empty := []string{}
	zonesPtr.Store(&empty)
	lister := func() []string {
		s := zonesPtr.Load()
		if s == nil {
			return nil
		}
		return *s
	}

	store := ephemeral.NewStore()
	cfg := &shadowdnscfg.EphemeralAPIConfig{
		Listen: apiAddr,
		Allow:  []netip.Prefix{netip.PrefixFrom(netip.MustParseAddr("127.0.0.1"), 32)},
	}
	apiSrv := api.NewServer(cfg, store, lister, zap.NewNop())

	apiCtx, apiCancel := context.WithCancel(context.Background())
	apiDone := make(chan struct{})
	go func() {
		defer close(apiDone)
		_ = apiSrv.Run(apiCtx)
	}()
	t.Cleanup(func() {
		apiCancel()
		select {
		case <-apiDone:
		case <-time.After(6 * time.Second):
		}
	})

	baseURL := "http://" + apiAddr
	waitHTTPReady(t, baseURL)

	// 1. Empty lister rejects the PUT.
	putURL := baseURL + "/v1/txt/foo.newzone.com"
	body := `{"value":"tok","ttl":30}`
	if code := httpPutJSON(t, putURL, body); code != http.StatusUnprocessableEntity {
		t.Fatalf("empty lister: PUT status = %d, want 422", code)
	}
	if _, ok := store.Lookup("foo.newzone.com."); ok {
		t.Error("empty lister: store must not be modified on 422")
	}

	// 2. Simulate SIGHUP adding newzone.com.
	added := []string{"newzone.com."}
	zonesPtr.Store(&added)
	if code := httpPutJSON(t, putURL, body); code != http.StatusOK {
		t.Fatalf("after zone add: PUT status = %d, want 200", code)
	}
	if _, ok := store.Lookup("foo.newzone.com."); !ok {
		t.Error("after zone add: expected record to be stored")
	}

	// 3. Simulate SIGHUP removing newzone.com.
	removed := []string{"other.com."}
	zonesPtr.Store(&removed)
	if code := httpPutJSON(t, putURL, body); code != http.StatusUnprocessableEntity {
		t.Fatalf("after zone remove: PUT status = %d, want 422", code)
	}
}
