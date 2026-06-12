// End-to-end coverage for per-alias-group CNAME chain collapsing. These tests
// build a self-contained fixture (alias root `example.com` with backup
// `example.net` and collapse_cname_chain enabled, plus a control group
// `plain.example.org` without the flag) in an isolated temp dir — the shared
// testdata/integration/ fixtures are deliberately untouched so their response
// assertions keep proving the flag-off default. The six query shapes of the
// design's Implementation Contract table are reproduced over the wire, plus
// the AXFR-never-collapses and opt-in-default-off spec requirements.
package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// buildCollapseServer builds the collapse fixture and starts a server on a
// loopback OS-assigned port. The allow-transfer ACL admits 127.0.0.1 so the
// AXFR test can run against the same instance.
func buildCollapseServer(t *testing.T) (*server.Server, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	masterDir := filepath.Join(tmpDir, "master")
	geoIPDir := filepath.Join(tmpDir, "geoip")
	if err := os.MkdirAll(masterDir, 0o755); err != nil {
		t.Fatalf("mkdir master: %v", err)
	}
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}

	// Root zone for the collapse-enabled group. Holds the contract table's
	// canonical chain (www → lb → pool-a → A), an out-of-zone chain
	// (www2 → lb2 → pool-b → external CNAME), and infrastructure records.
	writeCaseFile(t, filepath.Join(masterDir, "example.com.fwd"), `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2024010101 3600 600 604800 300
)
@      IN NS    ns1.example.com.
ns1    IN A     198.51.100.1
www    300 IN CNAME lb
lb     60  IN CNAME pool-a
pool-a 600 IN A     192.0.2.10
www2   300 IN CNAME lb2
lb2    60  IN CNAME pool-b
pool-b 600 IN CNAME cdn.external-vendor.example.org.
`)

	// Control root zone: same chain shape, alias group WITHOUT the collapse
	// flag — must keep emitting the full chain (opt-in defaults to off).
	writeCaseFile(t, filepath.Join(masterDir, "plain.example.org.fwd"), `$TTL 300
@ IN SOA ns1.plain.example.org. hostmaster.plain.example.org. (
    2024010101 3600 600 604800 300
)
@      IN NS    ns1.plain.example.org.
ns1    IN A     198.51.100.2
www    300 IN CNAME lb
lb     60  IN CNAME pool-a
pool-a 600 IN A     192.0.2.20
`)

	writeCaseFile(t, filepath.Join(tmpDir, "master.zones"),
		`view "view-other" {
    match-clients { any; };
    recursion no;
    zone "example.com" { type master; file "`+filepath.Join(masterDir, "example.com.fwd")+`"; };
    zone "plain.example.org"   { type master; file "`+filepath.Join(masterDir, "plain.example.org.fwd")+`"; };
};
`)

	writeCaseFile(t, filepath.Join(tmpDir, "named.conf"),
		`options {
    directory "`+tmpDir+`";
    geoip-directory "`+geoIPDir+`";
    listen-on { any; };
    recursion no;
};
include "`+filepath.Join(tmpDir, "master.zones")+`";
`)

	// example.net inherits example.com's collapse flag; mirror.example.org inherits
	// plain.example.org's absence of it. Neither backup has its own zone file — the
	// alias origin registration in BuildState routes their queries.
	writeCaseFile(t, filepath.Join(tmpDir, "shadowdns.yaml"),
		`aliases:
  example.com:
    members:
      - example.net
    collapse_cname_chain: true
  plain.example.org:
    members:
      - mirror.example.org
`)

	buildIntegrationMMDBs(t, geoIPDir)

	logger := zap.NewNop()
	cfg, err := config.LoadNamedConf(filepath.Join(tmpDir, "named.conf"), logger)
	if err != nil {
		t.Fatalf("LoadNamedConf: %v", err)
	}

	sdCfg, err := shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)
	if err != nil {
		t.Fatalf("shadowdnscfg.Load: %v", err)
	}

	country, asn, err := view.LoadGeoIP(geoIPDir, logger)
	if err != nil {
		t.Fatalf("LoadGeoIP: %v", err)
	}

	state, _, err := server.BuildState(cfg, sdCfg.Aliases, sdCfg.AliasFlags, sdCfg.CollapseFlags, sdCfg.BackupOriginalCase, nil, server.VerifyModeHash, country, asn, logger)
	if err != nil {
		_ = country.Close()
		_ = asn.Close()
		t.Fatalf("BuildState: %v", err)
	}

	acl, err := transfer.NewACL([]string{"127.0.0.1"})
	if err != nil {
		_ = country.Close()
		_ = asn.Close()
		t.Fatalf("transfer.NewACL: %v", err)
	}
	state.AllowTransferACL = acl

	srv := server.NewServer(state, logger)
	_, srvCleanup := bindAndServe(t, srv, nil)
	return srv, func() {
		srvCleanup()
		_ = country.Close()
		_ = asn.Close()
	}
}

// assertSingleAnswer fails unless the response holds exactly one answer
// record, returning it for further inspection.
func assertSingleAnswer(t *testing.T, resp *dns.Msg) dns.RR {
	t.Helper()
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d: %v", len(resp.Answer), resp.Answer)
	}
	return resp.Answer[0]
}

// assertNoDataWithSOA fails unless the response is NODATA: NOERROR, zero
// answers, and the SOA of soaOwner's zone in the authority section.
func assertNoDataWithSOA(t *testing.T, resp *dns.Msg, soaOwner string) {
	t.Helper()
	assertNoError(t, resp)
	if len(resp.Answer) != 0 {
		t.Fatalf("expected zero answers, got %d: %v", len(resp.Answer), resp.Answer)
	}
	assertAuthoritySOA(t, resp, soaOwner)
}

// Contract row 1: root A query collapses to the single terminal record.
func TestCollapse_RootAQuery(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	resp := queryUDP(t, udpAddr(srv), "www.example.com.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	rr := assertSingleAnswer(t, resp)
	a, ok := rr.(*dns.A)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.A", rr)
	}
	if a.Hdr.Name != "www.example.com." {
		t.Errorf("owner = %q, want www.example.com.", a.Hdr.Name)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (min of 300,60,600)", a.Hdr.Ttl)
	}
	if a.A.String() != "192.0.2.10" {
		t.Errorf("A = %s, want 192.0.2.10", a.A)
	}
}

// Contract row 2: backup A query collapses with the backup-namespace owner.
func TestCollapse_BackupAQuery(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	resp := queryUDP(t, udpAddr(srv), "www.example.net.", dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	rr := assertSingleAnswer(t, resp)
	a, ok := rr.(*dns.A)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.A", rr)
	}
	if a.Hdr.Name != "www.example.net." {
		t.Errorf("owner = %q, want www.example.net.", a.Hdr.Name)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60", a.Hdr.Ttl)
	}
	if a.A.String() != "192.0.2.10" {
		t.Errorf("A = %s, want 192.0.2.10", a.A)
	}
}

// Contract row 3: AAAA over the A-only tail is NODATA.
func TestCollapse_AAAAIsNoData(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	resp := queryUDP(t, udpAddr(srv), "www.example.com.", dns.TypeAAAA)
	assertNoDataWithSOA(t, resp, "example.com.")
}

// Contract row 4: a direct CNAME query over a fully in-zone chain is NODATA.
func TestCollapse_DirectCNAMEIsNoData(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	resp := queryUDP(t, udpAddr(srv), "www.example.com.", dns.TypeCNAME)
	assertNoDataWithSOA(t, resp, "example.com.")
}

// Contract row 5: an out-of-zone tail collapses to one synthesized CNAME and
// no intermediate name leaks.
func TestCollapse_OutOfZoneSynthesizedCNAME(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	resp := queryUDP(t, udpAddr(srv), "www2.example.com.", dns.TypeA)

	assertNoError(t, resp)
	rr := assertSingleAnswer(t, resp)
	cn, ok := rr.(*dns.CNAME)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.CNAME", rr)
	}
	if cn.Hdr.Name != "www2.example.com." {
		t.Errorf("owner = %q, want www2.example.com.", cn.Hdr.Name)
	}
	if cn.Target != "cdn.external-vendor.example.org." {
		t.Errorf("target = %q, want cdn.external-vendor.example.org.", cn.Target)
	}
	if cn.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (min of 300,60,600)", cn.Hdr.Ttl)
	}
	for _, rr := range resp.Answer {
		name := rr.Header().Name
		if name == "lb2.example.com." || name == "pool-b.example.com." {
			t.Errorf("intermediate name %q leaked into the response", name)
		}
	}
}

// Contract row 6: an intermediate chain name stays directly queryable and its
// response collapses too.
func TestCollapse_IntermediateNameQuery(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	resp := queryUDP(t, udpAddr(srv), "lb.example.com.", dns.TypeA)

	assertNoError(t, resp)
	rr := assertSingleAnswer(t, resp)
	a, ok := rr.(*dns.A)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.A", rr)
	}
	if a.Hdr.Name != "lb.example.com." {
		t.Errorf("owner = %q, want lb.example.com.", a.Hdr.Name)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (min of 60,600)", a.Hdr.Ttl)
	}
}

// Spec "Zone transfers are never collapsed": AXFR of the collapse-enabled
// zone still carries the stored chain records verbatim.
func TestCollapse_AXFRCarriesRawChain(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	rrs := axfrCollect(t, tcpAddr(srv), "example.com.")
	if len(rrs) == 0 {
		t.Fatal("AXFR returned no records (transfer refused?)")
	}
	found := false
	for _, rr := range rrs {
		cn, ok := rr.(*dns.CNAME)
		if !ok {
			continue
		}
		if cn.Hdr.Name == "www.example.com." && cn.Target == "lb.example.com." && cn.Hdr.Ttl == 300 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AXFR must include the stored `www.example.com. 300 CNAME lb.example.com.`; got %d records: %v", len(rrs), rrs)
	}
}

// Spec "Collapse is a per-alias-group opt-in that defaults to off": the group
// without the flag keeps emitting the full chain, both on the root and via
// its backup member.
func TestCollapse_OptOutGroupEmitsFullChain(t *testing.T) {
	srv, cancel := buildCollapseServer(t)
	defer cancel()

	t.Run("root", func(t *testing.T) {
		resp := queryUDP(t, udpAddr(srv), "www.plain.example.org.", dns.TypeA)
		assertNoError(t, resp)
		if len(resp.Answer) != 3 {
			t.Fatalf("expected the full 3-record chain, got %d: %v", len(resp.Answer), resp.Answer)
		}
		if _, ok := resp.Answer[0].(*dns.CNAME); !ok {
			t.Errorf("Answer[0]: got %T, want *dns.CNAME (chain emission unchanged)", resp.Answer[0])
		}
	})

	t.Run("backup member", func(t *testing.T) {
		resp := queryUDP(t, udpAddr(srv), "www.mirror.example.org.", dns.TypeA)
		assertNoError(t, resp)
		if len(resp.Answer) != 3 {
			t.Fatalf("expected the full 3-record chain, got %d: %v", len(resp.Answer), resp.Answer)
		}
	})
}
