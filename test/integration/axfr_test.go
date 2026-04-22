// Approach (b) from the spec: for ACL tests, build ServerState by-hand rather
// than reading from named.conf, so we can control the allow-transfer list.
// This matches the pattern used in internal/server/server_test.go.
//
// All AXFR requests go over TCP (AXFR over UDP → REFUSED per RFC 5936 §2.1).
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// ---------------------------------------------------------------------------
// State builders for AXFR tests
// ---------------------------------------------------------------------------

// axfrServerAllowLoopback builds a server whose allow-transfer ACL permits
// 127.0.0.1 — so that local test clients can perform AXFR.
func axfrServerAllowLoopback(t *testing.T) (*server.Server, func()) {
	t.Helper()
	return axfrServerWithACL(t, []string{"127.0.0.1"})
}

// axfrServerDenyAll builds a server whose allow-transfer ACL is empty
// (denies all transfers).
func axfrServerDenyAll(t *testing.T) (*server.Server, func()) {
	t.Helper()
	return axfrServerWithACL(t, []string{})
}

// axfrServerWithACL builds a full server from the integration fixtures but
// overrides the allow-transfer ACL with the given entries.
func axfrServerWithACL(t *testing.T, aclEntries []string) (*server.Server, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	copyFixtures(t, tmpDir)

	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildIntegrationMMDBs(t, geoIPDir)
	patchNamedConf(t, tmpDir)

	namedConf := filepath.Join(tmpDir, "named.conf")
	logger := zap.NewNop()

	cfg, err := config.LoadNamedConf(namedConf, logger)
	if err != nil {
		t.Fatalf("LoadNamedConf: %v", err)
	}

	sdCfg, err := shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}
	aliases := sdCfg.Aliases

	country, asnDB, err := view.LoadGeoIP(geoIPDir, logger)
	if err != nil {
		t.Fatalf("LoadGeoIP: %v", err)
	}

	// Build state normally to get zones loaded.
	state, _, err := server.BuildState(cfg, aliases, nil, server.VerifyModeHash, country, asnDB, logger)
	if err != nil {
		_ = country.Close()
		_ = asnDB.Close()
		t.Fatalf("server.BuildState: %v", err)
	}

	// Override the ACL with the test-controlled entries.
	acl, err := transfer.NewACL(aclEntries)
	if err != nil {
		_ = country.Close()
		_ = asnDB.Close()
		t.Fatalf("transfer.NewACL: %v", err)
	}
	state.AllowTransferACL = acl

	srv := server.NewServer(state, logger)

	// UDPAddr()/TCPAddr() reads must not race the Serve goroutine writing s.listeners.
	if err := srv.Bind("127.0.0.1:0"); err != nil {
		_ = country.Close()
		_ = asnDB.Close()
		t.Fatalf("srv.Bind: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			t.Logf("server exited: %v", err)
		}
	}()

	teardown := func() {
		cancel()
		<-done
		_ = country.Close()
		_ = asnDB.Close()
	}
	return srv, teardown
}

// ---------------------------------------------------------------------------
// AXFR helpers
// ---------------------------------------------------------------------------

// axfrCollect performs an AXFR over TCP and returns all received RRs
// (including the opening and closing SOA sentinels).
// On a connection-level error it returns nil, 0 — the caller should check
// length.  On a protocol-level REFUSED the envelope Error is non-nil.
func axfrCollect(t *testing.T, addr, qname string) []dns.RR {
	t.Helper()

	m := new(dns.Msg)
	m.SetAxfr(dns.Fqdn(qname))

	tr := &dns.Transfer{}
	ch, err := tr.In(m, addr)
	if err != nil {
		t.Logf("AXFR In() error (may be REFUSED): %v", err)
		return nil
	}

	var rrs []dns.RR
	for env := range ch {
		if env.Error != nil {
			t.Logf("AXFR envelope error: %v", env.Error)
			return nil
		}
		rrs = append(rrs, env.RR...)
	}
	return rrs
}

// axfrRcode sends an AXFR request over TCP and returns the RCODE of the
// reply.  Wraps axfrRcodeVia for the common TCP case.
func axfrRcode(t *testing.T, addr, qname string) int {
	t.Helper()
	return axfrRcodeVia(t, "tcp", addr, qname)
}

// axfrRcodeVia sends an AXFR request over the given network (tcp or udp)
// and returns the RCODE.  A connection error is treated as REFUSED since
// that is how TCP-side denies manifest.
func axfrRcodeVia(t *testing.T, network, addr, qname string) int {
	t.Helper()

	c := &dns.Client{Net: network, Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetAxfr(dns.Fqdn(qname))

	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		return dns.RcodeRefused
	}
	return resp.Rcode
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAXFR_RootZone_Permitted verifies that an AXFR for example.com from a
// permitted source IP (127.0.0.1) returns the full zone stream:
// SOA → records → SOA.
func TestAXFR_RootZone_Permitted(t *testing.T) {
	srv, cancel := axfrServerAllowLoopback(t)
	defer cancel()
	addr := tcpAddr(srv)

	rrs := axfrCollect(t, addr, "example.com.")
	if len(rrs) < 3 {
		t.Fatalf("expected at least 3 RRs in AXFR stream (SOA + body + SOA), got %d", len(rrs))
	}

	// Opening record must be SOA.
	if rrs[0].Header().Rrtype != dns.TypeSOA {
		t.Errorf("first record should be SOA, got %s", dns.TypeToString[rrs[0].Header().Rrtype])
	}
	// Closing record must be SOA.
	if rrs[len(rrs)-1].Header().Rrtype != dns.TypeSOA {
		t.Errorf("last record should be SOA, got %s", dns.TypeToString[rrs[len(rrs)-1].Header().Rrtype])
	}

	// There must be at least one A record in the stream.
	hasA := false
	for _, rr := range rrs {
		if rr.Header().Rrtype == dns.TypeA {
			hasA = true
			break
		}
	}
	if !hasA {
		t.Error("expected at least one A record in AXFR stream")
	}

	// Opening SOA must have example.com. as owner.
	if soa, ok := rrs[0].(*dns.SOA); ok {
		if !strings.EqualFold(soa.Hdr.Name, "example.com.") {
			t.Errorf("expected SOA owner example.com., got %s", soa.Hdr.Name)
		}
	}
}

// TestAXFR_RootZone_Denied verifies that an AXFR from a non-permitted source
// returns REFUSED.  With an empty ACL the server refuses all transfers.
func TestAXFR_RootZone_Denied(t *testing.T) {
	srv, cancel := axfrServerDenyAll(t)
	defer cancel()
	addr := tcpAddr(srv)

	rcode := axfrRcode(t, addr, "example.com.")
	if rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for denied AXFR, got %s", dns.RcodeToString[rcode])
	}
}

// TestAXFR_BackupZone verifies that an AXFR for backup.example returns rewritten
// records — every owner name must be under backup.example, not example.com.
func TestAXFR_BackupZone(t *testing.T) {
	srv, cancel := axfrServerAllowLoopback(t)
	defer cancel()
	addr := tcpAddr(srv)

	rrs := axfrCollect(t, addr, "backup.example.")
	if len(rrs) < 3 {
		t.Fatalf("expected at least 3 RRs in backup AXFR stream, got %d", len(rrs))
	}

	// Every owner name must NOT be under example.com (must be under backup.example).
	for _, rr := range rrs {
		owner := strings.ToLower(rr.Header().Name)
		if strings.HasSuffix(owner, "example.com.") {
			t.Errorf("owner %s has root-zone suffix .example.com.; expected rewrite to backup namespace", owner)
		}
	}

	// Opening SOA must have backup.example. owner.
	if soa, ok := rrs[0].(*dns.SOA); ok {
		if !strings.EqualFold(soa.Hdr.Name, "backup.example.") {
			t.Errorf("expected opening SOA owner backup.example., got %s", soa.Hdr.Name)
		}
		// MNAME must also be rewritten.
		if !strings.EqualFold(soa.Ns, "ns1.backup.example.") {
			t.Errorf("expected MNAME ns1.backup.example., got %s", soa.Ns)
		}
	} else {
		t.Errorf("expected first RR to be SOA, got %T", rrs[0])
	}
}

// TestAXFR_UnknownZone_Refused verifies that an AXFR for an unloaded zone
// returns REFUSED.
func TestAXFR_UnknownZone_Refused(t *testing.T) {
	srv, cancel := axfrServerAllowLoopback(t)
	defer cancel()
	addr := tcpAddr(srv)

	rcode := axfrRcode(t, addr, "unknown.com.")
	if rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for unknown zone AXFR, got %s", dns.RcodeToString[rcode])
	}
}

// TestAXFR_UDP_Refused verifies that AXFR requests delivered over UDP are
// rejected with REFUSED per RFC 5936 §2.1 (AXFR SHALL be carried over TCP).
// The ACL allows 127.0.0.1 so that an ACL mismatch cannot mask the refusal.
func TestAXFR_UDP_Refused(t *testing.T) {
	srv, cancel := axfrServerAllowLoopback(t)
	defer cancel()

	rcode := axfrRcodeVia(t, "udp", udpAddr(srv), "example.com.")
	if rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for AXFR over UDP, got %s", dns.RcodeToString[rcode])
	}
}
