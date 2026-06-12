// End-to-end coverage for DNS name case preservation. These tests build a
// mixed-case fixture (alias root `originzone.com` + backup yaml `Example.com`,
// plus a non-alias `root.com` with a mixed-case stored owner and a wildcard)
// in an isolated temp dir, then exercise the full server pipeline:
//
//   - 6.1: alias path with lowercase / mixed-case / all-uppercase qnames —
//     Question section echoes the on-wire qname byte-for-byte; Answer owner
//     uses the zone-file storage prefix + operator-authored backup yaml suffix.
//   - 6.2: root-zone exact match with mixed-case stored owner — Answer owner
//     comes from zone-file storage, regardless of qname case.
//   - 6.3: wildcard synthesis — Answer owner takes the prefix from the qname
//     (the only path where qname case flows into the synthesized owner).
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
	"github.com/chenwei791129/ShadowDNS/internal/view"
)

// buildCasePreservationServer builds a self-contained test fixture in a fresh
// TempDir and starts a server bound to a loopback OS-assigned port. The
// fixture intentionally diverges from testdata/integration/ so it can mix
// operator-authored cases without disturbing other integration tests.
func buildCasePreservationServer(t *testing.T) (*server.Server, func()) {
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

	// Root zone for the alias path. All owners stored lowercase so the
	// alias rewrite output prefix is unambiguously the lowercase
	// zone-file case.
	writeCaseFile(t, filepath.Join(masterDir, "originzone.com.fwd"), `$TTL 300
@ IN SOA ns1.originzone.com. hostmaster.originzone.com. (
    2024010101 3600 600 604800 300
)
@   IN NS    ns1.originzone.com.
@   IN A     198.51.100.10
ns1 IN A     198.51.100.1
www IN A     198.51.100.30
www IN AAAA  2001:db8:1::30
`)

	// Backup zone — present so BIND named.conf parsing succeeds and the
	// alias dispatcher can route queries here. The body is a minimal
	// SOA + NS; everything under example.com. resolves via the alias
	// path against originzone.com.
	writeCaseFile(t, filepath.Join(masterDir, "example.com.fwd"), `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2024010101 3600 600 604800 300
)
@   IN NS    ns1.example.com.
ns1 IN A     198.51.100.2
`)

	// Standalone root zone (no alias) for the non-alias case-preservation
	// tests. `Service.Root.Com.` is written with mixed case as an absolute
	// FQDN so the parser stores the byte-for-byte case. The wildcard owner
	// is lowercase to keep its synthesis-derived case unambiguous.
	writeCaseFile(t, filepath.Join(masterDir, "root.com.fwd"), `$TTL 300
@ IN SOA ns1.root.com. hostmaster.root.com. (
    2024010101 3600 600 604800 300
)
@                 IN NS    ns1.root.com.
ns1               IN A     198.51.100.3
Service.Root.Com. IN A     198.51.100.40
*                 IN A     198.51.100.50
`)

	writeCaseFile(t, filepath.Join(tmpDir, "master.zones"),
		`view "view-other" {
    match-clients { any; };
    recursion no;
    zone "originzone.com" { type master; file "`+filepath.Join(masterDir, "originzone.com.fwd")+`"; };
    zone "example.com"    { type master; file "`+filepath.Join(masterDir, "example.com.fwd")+`"; };
    zone "root.com"       { type master; file "`+filepath.Join(masterDir, "root.com.fwd")+`"; };
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

	// Backup yaml uses mixed case `Example.com` so the alias rewrite suffix
	// must match this byte-for-byte. The lowercase-fold internal map key is
	// what the runtime looks up; the original case is preserved in the
	// AliasGroup struct and threaded through to RewriteName as the suffix.
	writeCaseFile(t, filepath.Join(tmpDir, "shadowdns.yaml"),
		`aliases:
  originzone.com:
    members:
      - Example.com
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

	srv := server.NewServer(state, logger)
	_, srvCleanup := bindAndServe(t, srv, nil)
	return srv, func() {
		srvCleanup()
		_ = country.Close()
		_ = asn.Close()
	}
}

func writeCaseFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// assertQuestionEcho fences the Question section against any case mutation
// (DNS-0x20 randomized queries depend on byte-for-byte echo to authenticate).
func assertQuestionEcho(t *testing.T, resp *dns.Msg, want string) {
	t.Helper()
	if len(resp.Question) != 1 {
		t.Fatalf("expected 1 question, got %d", len(resp.Question))
	}
	if got := resp.Question[0].Name; got != want {
		t.Errorf("Question section: got %q, want %q (byte-for-byte echo required)", got, want)
	}
}

// assertAnswerOwnerExact requires byte-for-byte ownership case (not the
// case-insensitive comparison used by the existing assertHasA helpers).
func assertAnswerOwnerExact(t *testing.T, resp *dns.Msg, qtype uint16, wantOwner string) {
	t.Helper()
	for _, rr := range resp.Answer {
		if rr.Header().Rrtype == qtype && rr.Header().Name == wantOwner {
			return
		}
	}
	t.Errorf("expected %s record with owner exactly %q; got: %v",
		dns.TypeToString[qtype], wantOwner, resp.Answer)
}

// ---------------------------------------------------------------------------
// 6.1 Alias-path case preservation (mixed-case backup yaml `Example.com`)
// ---------------------------------------------------------------------------

// TestCasePreservation_Alias_LowercaseQuery verifies that a fully lowercase
// qname under the backup namespace is answered with:
//   - Question section echoing the lowercase qname.
//   - Answer owner whose prefix comes from zone-file storage (lowercase
//     `www`) and whose suffix comes from the operator-authored backup yaml
//     (`Example.com.`, mixed case).
func TestCasePreservation_Alias_LowercaseQuery(t *testing.T) {
	srv, cancel := buildCasePreservationServer(t)
	defer cancel()
	addr := udpAddr(srv)

	const qname = "www.example.com."
	resp := queryUDP(t, addr, qname, dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertQuestionEcho(t, resp, qname)
	assertAnswerOwnerExact(t, resp, dns.TypeA, "www.Example.com.")
}

// TestCasePreservation_Alias_MixedCaseQuery verifies that a mixed-case qname
// (DNS-0x20 style) is echoed verbatim in the Question section, while the
// Answer owner still uses the zone-file storage prefix + backup yaml suffix
// — this is the exact-match path, so the qname case does NOT flow into the
// owner.
func TestCasePreservation_Alias_MixedCaseQuery(t *testing.T) {
	srv, cancel := buildCasePreservationServer(t)
	defer cancel()
	addr := udpAddr(srv)

	const qname = "WwW.ExAmPlE.cOm."
	resp := queryUDP(t, addr, qname, dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertQuestionEcho(t, resp, qname)
	assertAnswerOwnerExact(t, resp, dns.TypeA, "www.Example.com.")
}

// TestCasePreservation_Alias_AllUppercaseQuery verifies the all-uppercase
// edge case — the Question section must echo the uppercase qname (this is
// the canonical DNS-0x20 case that motivated the change).
func TestCasePreservation_Alias_AllUppercaseQuery(t *testing.T) {
	srv, cancel := buildCasePreservationServer(t)
	defer cancel()
	addr := udpAddr(srv)

	const qname = "WWW.EXAMPLE.COM."
	resp := queryUDP(t, addr, qname, dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertQuestionEcho(t, resp, qname)
	assertAnswerOwnerExact(t, resp, dns.TypeA, "www.Example.com.")
}

// ---------------------------------------------------------------------------
// 6.2 Root-zone exact-match case preservation (non-alias path)
// ---------------------------------------------------------------------------

// TestCasePreservation_RootZone_StoresMixedCaseOwner verifies that an exact
// match against a zone-file owner stored as `Service.Root.Com.` returns that
// case in the Answer owner regardless of the qname's case. The Question
// section still echoes the on-wire qname.
func TestCasePreservation_RootZone_StoresMixedCaseOwner(t *testing.T) {
	srv, cancel := buildCasePreservationServer(t)
	defer cancel()
	addr := udpAddr(srv)

	const qname = "service.root.com."
	resp := queryUDP(t, addr, qname, dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertQuestionEcho(t, resp, qname)
	assertAnswerOwnerExact(t, resp, dns.TypeA, "Service.Root.Com.")
}

// ---------------------------------------------------------------------------
// 6.3 Wildcard-synthesis case preservation
// ---------------------------------------------------------------------------

// TestCasePreservation_Wildcard_QueryCasePreserved verifies the wildcard
// synthesis path: with `*.root.com. A 198.51.100.50` in the zone, a query
// for `WWW.Root.Com. A` returns the synthesized record with owner =
// `WWW.Root.Com.` (qname case). This is the only path where qname case
// flows into the Answer owner — there is no zone-file storage to draw from
// for synthesized owners.
func TestCasePreservation_Wildcard_QueryCasePreserved(t *testing.T) {
	srv, cancel := buildCasePreservationServer(t)
	defer cancel()
	addr := udpAddr(srv)

	const qname = "WWW.Root.Com."
	resp := queryUDP(t, addr, qname, dns.TypeA)

	assertNoError(t, resp)
	assertAuthoritative(t, resp)
	assertQuestionEcho(t, resp, qname)
	assertAnswerOwnerExact(t, resp, dns.TypeA, qname)
}
