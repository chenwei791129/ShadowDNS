package main

// Tests for DoH reload semantics: the doh section is re-validated on SIGHUP
// (errors keep the running server), but a change to doh.listen / doh.acme.*
// is not applied live — it logs a restart advisory, mirroring the existing
// DNS listen-address drift behavior.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

const dohYAMLValid = `doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
    account_key_file: "/var/lib/shadowdns/acme/account.key"
`

// writeShadowYAML overwrites the dir's shadowdns.yaml with body.
func writeShadowYAML(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "shadowdns.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write shadowdns.yaml: %v", err)
	}
}

// bootDoHFrom parses body and returns its DoH config, used to seed opts.BootDoH
// as if the server had bound that config at startup.
func bootDoHFrom(t *testing.T, dir, body string) *shadowdnscfg.DoHConfig {
	t.Helper()
	writeShadowYAML(t, dir, body)
	cfg, err := shadowdnscfg.Load(filepath.Join(dir, "shadowdns.yaml"), nil)
	if err != nil {
		t.Fatalf("load shadowdns.yaml: %v", err)
	}
	return cfg.DoH
}

func TestReload_InvalidDoHKeepsRunningServer(t *testing.T) {
	dir := setupReloadTestDir(t)
	bootDoH := bootDoHFrom(t, dir, dohYAMLValid)
	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	opts.BootDoH = bootDoH
	prevState := srv.CurrentState()

	// Remove only the required acme.ip field and reload. account_key_file is
	// kept present so the load fails for exactly one reason (missing ip),
	// independent of buildDoHACME's field validation order.
	writeShadowYAML(t, dir, `doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    http01_listen: "203.0.113.10:80"
    account_key_file: "/var/lib/shadowdns/acme/account.key"
`)
	_, err := observedReload(t, opts, srv, geo, qlState)
	if err == nil {
		t.Fatal("reload succeeded, want error from invalid doh config")
	}
	if !strings.Contains(err.Error(), "ip") {
		t.Errorf("error = %q, want it to name the missing ip field", err.Error())
	}
	if srv.CurrentState() != prevState {
		t.Error("server state was swapped despite the doh validation failure")
	}
}

func TestReload_ChangedDoHListenRequiresRestart(t *testing.T) {
	dir := setupReloadTestDir(t)
	bootDoH := bootDoHFrom(t, dir, dohYAMLValid)
	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	opts.BootDoH = bootDoH

	// Change doh.listen to a different address and reload.
	writeShadowYAML(t, dir, strings.Replace(dohYAMLValid, "203.0.113.10:443", "203.0.113.20:443", 1))
	logs, err := observedReload(t, opts, srv, geo, qlState)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	found := false
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "DoH") && strings.Contains(e.Message, "restart") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a log entry advising a process restart for the changed DoH listener")
	}
}

func TestReload_UnchangedDoHNoRestartAdvisory(t *testing.T) {
	dir := setupReloadTestDir(t)
	bootDoH := bootDoHFrom(t, dir, dohYAMLValid)
	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	opts.BootDoH = bootDoH

	// Reload the same doh config; no restart advisory should be logged.
	logs, err := observedReload(t, opts, srv, geo, qlState)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "DoH") && strings.Contains(e.Message, "restart") {
			t.Errorf("unexpected restart advisory for unchanged doh config: %q", e.Message)
		}
	}
}
