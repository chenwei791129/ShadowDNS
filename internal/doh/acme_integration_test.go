package doh

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

// TestLegoObtainer_Integration drives the production ACME obtain path
// (newLegoObtainer + the long-lived challengeResponder) against an injectable
// ACME directory — a local pebble. It is skipped unless SHADOWDNS_ACME_PEBBLE_DIR
// is set, so `make test` stays green without external infrastructure.
//
// To run it: start pebble, then run this test binary inside pebble's network
// namespace with:
//
//	SHADOWDNS_ACME_PEBBLE_DIR=https://localhost:14000/dir
//	LEGO_CA_CERTIFICATES=/path/to/pebble.minica.pem
//
// pebble's HTTP-01 validator dials the identifier on port 5002, so the
// responder binds :5002 and the identifier is loopback (reachable in-namespace).
func TestLegoObtainer_Integration(t *testing.T) {
	dirURL := os.Getenv("SHADOWDNS_ACME_PEBBLE_DIR")
	if dirURL == "" {
		t.Skip("set SHADOWDNS_ACME_PEBBLE_DIR (and LEGO_CA_CERTIFICATES) to run the pebble ACME integration test")
	}

	responder := newChallengeResponder(nil)
	ln, err := net.Listen("tcp", ":5002")
	if err != nil {
		t.Fatalf("bind http-01 listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	srv := &http.Server{Handler: responder.Handler()}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	cfg := shadowdnscfg.DoHACMEConfig{
		DirectoryURL:   dirURL,
		IP:             netip.MustParseAddr("127.0.0.1"),
		HTTP01Listen:   ":5002",
		AccountKeyFile: filepath.Join(t.TempDir(), "account.key"),
	}
	obtain, err := newLegoObtainer(cfg, responder)
	if err != nil {
		t.Fatalf("newLegoObtainer: %v", err)
	}

	cert, err := obtain(context.Background())
	if err != nil {
		t.Fatalf("obtain: %v", err)
	}
	if cert.Leaf == nil {
		t.Fatal("issued cert has no parsed leaf")
	}
	if len(cert.Leaf.IPAddresses) != 1 || !cert.Leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Errorf("issued cert IP SANs = %v, want [127.0.0.1]", cert.Leaf.IPAddresses)
	}
	validity := cert.Leaf.NotAfter.Sub(cert.Leaf.NotBefore)
	// shortlived profile ≈ 6 days (518400s); allow a generous window.
	if validity < 5*24*time.Hour || validity > 8*24*time.Hour {
		t.Errorf("issued cert validity = %v, want ~6 days (shortlived profile)", validity)
	}

	// The first newLegoObtainer above took the generate-and-persist branch.
	// Build a second obtainer from the same config and confirm the production
	// path reuses the persisted account key (rather than minting a new one) —
	// the load-and-reuse behavior that is the whole point of this change.
	keyBefore, err := os.ReadFile(cfg.AccountKeyFile)
	if err != nil {
		t.Fatalf("account key was not persisted by the obtainer: %v", err)
	}
	if _, err := newLegoObtainer(cfg, responder); err != nil {
		t.Fatalf("second newLegoObtainer: %v", err)
	}
	keyAfter, err := os.ReadFile(cfg.AccountKeyFile)
	if err != nil {
		t.Fatalf("read account key after second build: %v", err)
	}
	if !bytes.Equal(keyBefore, keyAfter) {
		t.Error("account key file changed on second obtainer build; key was regenerated instead of reused")
	}
}
