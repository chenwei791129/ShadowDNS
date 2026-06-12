package alias

import (
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// buildZone creates a minimal Zone with the given origin and records.
func buildZone(origin string, rrs ...dns.RR) *zone.Zone {
	z := &zone.Zone{Origin: origin}
	for _, rr := range rrs {
		z.AddRR(rr)
	}
	return z
}

func TestResolve_OverrideTXT(t *testing.T) {
	// backupZone has its own TXT for backup.com.
	backupTXT := newTXT("backup.com.", "v=spf1 -all")
	backupZone := buildZone("backup.com.", backupTXT)

	// rootZone has a different TXT for root.com.
	rootTXT := newTXT("root.com.", "v=spf1 include:root.com. ~all")
	rootZone := buildZone("root.com.", rootTXT)

	rrs := Resolve("backup.com.", dns.TypeTXT, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	txt, ok := rrs[0].(*dns.TXT)
	if !ok {
		t.Fatalf("expected *dns.TXT, got %T", rrs[0])
	}
	if txt.Txt[0] != "v=spf1 -all" {
		t.Errorf("expected override TXT, got %q", txt.Txt[0])
	}
}

func TestResolve_NoOverride_InheritsMXWithRewrite(t *testing.T) {
	// backupZone has no MX override.
	backupZone := buildZone("backup.com.")

	// rootZone has an MX pointing within root.
	rootMX := newMX("root.com.", 10, "mail.root.com.")
	rootZone := buildZone("root.com.", rootMX)

	rrs := Resolve("backup.com.", dns.TypeMX, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	mx, ok := rrs[0].(*dns.MX)
	if !ok {
		t.Fatalf("expected *dns.MX, got %T", rrs[0])
	}
	if mx.Hdr.Name != "backup.com." {
		t.Errorf("MX owner: got %q, want backup.com.", mx.Hdr.Name)
	}
	if mx.Mx != "mail.backup.com." {
		t.Errorf("MX value: got %q, want mail.backup.com.", mx.Mx)
	}
}

func TestResolve_SRVOverride(t *testing.T) {
	backupSRV := newSRV("_sip._tcp.backup.com.", 10, 20, 5060, "sipserver.backup.com.")
	backupZone := buildZone("backup.com.", backupSRV)

	rootSRV := newSRV("_sip._tcp.root.com.", 0, 0, 5060, "sip.root.com.")
	rootZone := buildZone("root.com.", rootSRV)

	rrs := Resolve("_sip._tcp.backup.com.", dns.TypeSRV, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	srv, ok := rrs[0].(*dns.SRV)
	if !ok {
		t.Fatalf("expected *dns.SRV, got %T", rrs[0])
	}
	if srv.Priority != 10 || srv.Weight != 20 || srv.Port != 5060 {
		t.Errorf("SRV override fields: prio=%d weight=%d port=%d", srv.Priority, srv.Weight, srv.Port)
	}
}

func TestResolve_NilBackupZone_FallsThrough(t *testing.T) {
	rootA := newA("www.root.com.", "10.0.0.1")
	rootZone := buildZone("root.com.", rootA)

	rrs := Resolve("www.backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", rrs[0])
	}
	if a.Hdr.Name != "www.backup.com." {
		t.Errorf("A owner: got %q, want www.backup.com.", a.Hdr.Name)
	}
	if a.A.String() != "10.0.0.1" {
		t.Errorf("A IP: got %q, want 10.0.0.1", a.A.String())
	}
}

func TestResolve_OverrideExistsButQueryTypeIsA(t *testing.T) {
	// backupZone has a TXT override — but query is for A, so override is not consulted.
	backupTXT := newTXT("backup.com.", "override")
	backupZone := buildZone("backup.com.", backupTXT)

	rootA := newA("root.com.", "192.168.1.1")
	rootZone := buildZone("root.com.", rootA)

	rrs := Resolve("backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", rrs[0])
	}
	if a.Hdr.Name != "backup.com." {
		t.Errorf("A owner: got %q, want backup.com.", a.Hdr.Name)
	}
}

func TestResolve_NoMatchInRootZone(t *testing.T) {
	backupZone := buildZone("backup.com.")
	rootZone := buildZone("root.com.") // no records

	rrs := Resolve("www.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 0 {
		t.Errorf("expected empty result, got %d records", len(rrs))
	}
}

func TestResolve_CNAMESynthesis_BackupZone(t *testing.T) {
	// root zone has sub.root.com. → CNAME target.other.com.
	rootCNAME := newCNAME("sub.root.com.", "target.other.com.")
	rootZone := buildZone("root.com.", rootCNAME)

	backupZone := buildZone("backup.com.") // no overrides

	// Query A for sub.backup.com. → should get CNAME with owner rewritten.
	rrs := Resolve("sub.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 CNAME record, got %d", len(rrs))
	}
	cname, ok := rrs[0].(*dns.CNAME)
	if !ok {
		t.Fatalf("expected *dns.CNAME, got %T", rrs[0])
	}
	if cname.Hdr.Name != "sub.backup.com." {
		t.Errorf("CNAME owner: got %q, want sub.backup.com.", cname.Hdr.Name)
	}
	if cname.Target != "target.other.com." {
		t.Errorf("CNAME target: got %q, want target.other.com.", cname.Target)
	}
}

func TestResolve_CNAMESynthesis_NoCNAMENoA_ReturnsEmpty(t *testing.T) {
	rootZone := buildZone("root.com.") // no records at sub.root.com.
	backupZone := buildZone("backup.com.")

	rrs := Resolve("sub.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 0 {
		t.Errorf("expected empty result, got %d records", len(rrs))
	}
}

func TestResolve_NilRootZone_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resolve panicked with nil rootZone: %v", r)
		}
	}()
	_ = Resolve("backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, nil, false)
}

func TestResolveExact_NilRootZone_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ResolveExact panicked with nil rootZone: %v", r)
		}
	}()
	rrs := ResolveExact("backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, nil, false)
	if rrs != nil {
		t.Errorf("expected nil, got %v", rrs)
	}
}

func TestResolveExactNoCNAME_NilRootZone_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ResolveExactNoCNAME panicked with nil rootZone: %v", r)
		}
	}()
	rrs := ResolveExactNoCNAME("backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, nil, false)
	if rrs != nil {
		t.Errorf("expected nil, got %v", rrs)
	}
}

func TestResolveCNAMEFallback_NilRootZone_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ResolveCNAMEFallback panicked with nil rootZone: %v", r)
		}
	}()
	rrs := ResolveCNAMEFallback("backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, false)
	if rrs != nil {
		t.Errorf("expected nil, got %v", rrs)
	}
}

// TestResolveCNAMEFallback_CNAMEQtype_ReturnsNil verifies the fallback is
// scoped to non-CNAME qtypes; an explicit CNAME query must not trigger the
// fallback branch (it is handled by the exact-match pass instead).
func TestResolveCNAMEFallback_CNAMEQtype_ReturnsNil(t *testing.T) {
	rootZone := buildZone("root.com.",
		newCNAME("alias.root.com.", "target.root.com."),
	)
	rrs := ResolveCNAMEFallback("alias.backup.com.", dns.TypeCNAME, "backup.com.", "backup.com.", rootZone, false)
	if rrs != nil {
		t.Errorf("expected nil for CNAME qtype, got %v", rrs)
	}
}

func TestResolveWildcard_NilRootZone_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ResolveWildcard panicked with nil rootZone: %v", r)
		}
	}()
	rrs := ResolveWildcard("backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, false)
	if rrs != nil {
		t.Errorf("expected nil, got %v", rrs)
	}
}

// ---------------------------------------------------------------------------
// In-zone CNAME following in backup zone resolution
// ---------------------------------------------------------------------------

func TestResolve_CNAMEFollowing_InZone(t *testing.T) {
	rootZone := buildZone("root.com.",
		newCNAME("app.root.com.", "service.root.com."),
		newA("service.root.com.", "10.0.0.1"),
	)
	backupZone := buildZone("backup.com.")

	rrs := Resolve("app.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 2 {
		t.Fatalf("expected 2 records (CNAME + A), got %d", len(rrs))
	}
	cname, ok := rrs[0].(*dns.CNAME)
	if !ok {
		t.Fatalf("rrs[0]: expected *dns.CNAME, got %T", rrs[0])
	}
	if cname.Hdr.Name != "app.backup.com." {
		t.Errorf("CNAME owner: got %q, want app.backup.com.", cname.Hdr.Name)
	}
	if cname.Target != "service.backup.com." {
		t.Errorf("CNAME target: got %q, want service.backup.com.", cname.Target)
	}
	a, ok := rrs[1].(*dns.A)
	if !ok {
		t.Fatalf("rrs[1]: expected *dns.A, got %T", rrs[1])
	}
	if a.Hdr.Name != "service.backup.com." {
		t.Errorf("A owner: got %q, want service.backup.com.", a.Hdr.Name)
	}
	if a.A.String() != "10.0.0.1" {
		t.Errorf("A IP: got %q, want 10.0.0.1", a.A.String())
	}
}

func TestResolve_CNAMEFollowing_Chain(t *testing.T) {
	rootZone := buildZone("root.com.",
		newCNAME("a.root.com.", "b.root.com."),
		newCNAME("b.root.com.", "c.root.com."),
		newA("c.root.com.", "9.8.7.6"),
	)
	backupZone := buildZone("backup.com.")

	rrs := Resolve("a.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 3 {
		t.Fatalf("expected 3 records (2 CNAME + 1 A), got %d", len(rrs))
	}
	cn1 := rrs[0].(*dns.CNAME)
	if cn1.Hdr.Name != "a.backup.com." || cn1.Target != "b.backup.com." {
		t.Errorf("rrs[0]: owner=%q target=%q", cn1.Hdr.Name, cn1.Target)
	}
	cn2 := rrs[1].(*dns.CNAME)
	if cn2.Hdr.Name != "b.backup.com." || cn2.Target != "c.backup.com." {
		t.Errorf("rrs[1]: owner=%q target=%q", cn2.Hdr.Name, cn2.Target)
	}
	a := rrs[2].(*dns.A)
	if a.Hdr.Name != "c.backup.com." || a.A.String() != "9.8.7.6" {
		t.Errorf("rrs[2]: owner=%q ip=%s", a.Hdr.Name, a.A)
	}
}

func TestResolve_CNAMEFollowing_OutOfBailiwick(t *testing.T) {
	rootZone := buildZone("root.com.",
		newCNAME("app.root.com.", "cdn.external.com."),
	)
	backupZone := buildZone("backup.com.")

	rrs := Resolve("app.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record (CNAME only), got %d", len(rrs))
	}
	cname := rrs[0].(*dns.CNAME)
	if cname.Hdr.Name != "app.backup.com." {
		t.Errorf("CNAME owner: got %q, want app.backup.com.", cname.Hdr.Name)
	}
	if cname.Target != "cdn.external.com." {
		t.Errorf("CNAME target: got %q, want cdn.external.com.", cname.Target)
	}
}

// ---------------------------------------------------------------------------
// rewrite_rdata_labels flag: owner stays suffix-only, RDATA picks the path
// ---------------------------------------------------------------------------

// TestResolve_OwnerRewrite_FlagTrueAndFalse confirms the owner-name rewrite
// rule is in-bailiwick suffix-only regardless of the rewriteRDATALabels flag,
// per the alias-resolver spec scenario "Owner rewrite ignores RDATA flag".
func TestResolve_OwnerRewrite_FlagTrueAndFalse(t *testing.T) {
	rootCNAME := newCNAME("www.root.com.", "target.amazonaws.com.")
	rootZone := buildZone("root.com.", rootCNAME)
	backupZone := buildZone("backup.com.")

	for _, flag := range []bool{false, true} {
		flag := flag
		name := "false"
		if flag {
			name = "true"
		}
		t.Run("flag="+name, func(t *testing.T) {
			rrs := Resolve("www.backup.com.", dns.TypeCNAME, "backup.com.", "backup.com.", backupZone, rootZone, flag)
			if len(rrs) != 1 {
				t.Fatalf("flag=%v: expected 1 record, got %d", flag, len(rrs))
			}
			c := rrs[0].(*dns.CNAME)
			if c.Hdr.Name != "www.backup.com." {
				t.Errorf("flag=%v: owner = %q, want www.backup.com.", flag, c.Hdr.Name)
			}
			// RDATA target points outside both zones: must be preserved on
			// both flag values (anywhere-match has no root sequence to find).
			if c.Target != "target.amazonaws.com." {
				t.Errorf("flag=%v: RDATA target = %q, want target.amazonaws.com.", flag, c.Target)
			}
		})
	}
}

// TestResolve_RDATARewrite_FlagControlsMidLabel confirms the flag controls
// whether mid-label root sequences in RDATA get rewritten. With flag=false
// the templated CNAME target keeps its root.com. middle label; with flag=true
// it is replaced with backup.com.
func TestResolve_RDATARewrite_FlagControlsMidLabel(t *testing.T) {
	rootCNAME := newCNAME("host.root.com.", "host.root.com.cdn.example.net.")
	rootZone := buildZone("root.com.", rootCNAME)
	backupZone := buildZone("backup.com.")

	tests := []struct {
		flag       bool
		wantTarget string
	}{
		{flag: false, wantTarget: "host.root.com.cdn.example.net."},
		{flag: true, wantTarget: "host.backup.com.cdn.example.net."},
	}

	for _, tc := range tests {
		tc := tc
		name := "false"
		if tc.flag {
			name = "true"
		}
		t.Run("flag="+name, func(t *testing.T) {
			rrs := Resolve("host.backup.com.", dns.TypeCNAME, "backup.com.", "backup.com.", backupZone, rootZone, tc.flag)
			if len(rrs) != 1 {
				t.Fatalf("expected 1 record, got %d", len(rrs))
			}
			c := rrs[0].(*dns.CNAME)
			if c.Hdr.Name != "host.backup.com." {
				t.Errorf("owner = %q, want host.backup.com.", c.Hdr.Name)
			}
			if c.Target != tc.wantTarget {
				t.Errorf("target = %q, want %q", c.Target, tc.wantTarget)
			}
		})
	}
}

// TestResolve_BackupOriginalCase_PreservedOnWire verifies the case contract
// documented on Resolve: the alias config writes `Example.com` (mixed case),
// the lookup-fold form `example.com.` keys the maps, and the rewritten owner
// names emitted on the wire carry the operator-authored case verbatim. RDATA
// names rewritten via the in-bailiwick suffix rule carry the same case.
func TestResolve_BackupOriginalCase_PreservedOnWire(t *testing.T) {
	const (
		backupOrigin = "example.com." // lookup-fold key (DNS comparison)
		backupOnWire = "Example.com." // operator-authored YAML case
		rootOrigin   = "root.com."
	)

	rootZone := buildZone(rootOrigin,
		newCNAME("svc.root.com.", "target.root.com."),
		newA("target.root.com.", "10.0.0.1"),
	)
	backupZone := buildZone(backupOrigin)

	// Mixed-case query (DNS-0x20 randomized) — the handler folds qname for
	// the ResolveExact lookup, so callers pass the lookup-fold form here.
	rrs := Resolve("svc.example.com.", dns.TypeA, backupOrigin, backupOnWire, backupZone, rootZone, false)

	if len(rrs) != 2 {
		t.Fatalf("expected 2 records (CNAME + A), got %d", len(rrs))
	}
	cname, ok := rrs[0].(*dns.CNAME)
	if !ok {
		t.Fatalf("rrs[0]: expected *dns.CNAME, got %T", rrs[0])
	}
	if cname.Hdr.Name != "svc.Example.com." {
		t.Errorf("CNAME owner: got %q, want svc.Example.com.", cname.Hdr.Name)
	}
	if cname.Target != "target.Example.com." {
		t.Errorf("CNAME target: got %q, want target.Example.com.", cname.Target)
	}
	a, ok := rrs[1].(*dns.A)
	if !ok {
		t.Fatalf("rrs[1]: expected *dns.A, got %T", rrs[1])
	}
	if a.Hdr.Name != "target.Example.com." {
		t.Errorf("A owner: got %q, want target.Example.com.", a.Hdr.Name)
	}
}

// TestResolveWildcard_BackupOriginalCase confirms wildcard synthesis emits the
// operator-authored backup case in the rewritten owner name.
func TestResolveWildcard_BackupOriginalCase(t *testing.T) {
	rootZone := buildZone("root.com.")
	rootZone.AddRR(newA("*.root.com.", "10.0.0.42"))

	rrs := ResolveWildcard("any.example.com.", dns.TypeA, "example.com.", "Example.com.", rootZone, false)

	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", rrs[0])
	}
	if a.Hdr.Name != "any.Example.com." {
		t.Errorf("A owner: got %q, want any.Example.com.", a.Hdr.Name)
	}
}

func TestResolve_CNAMEFollowing_WildcardInZone(t *testing.T) {
	rootZone := buildZone("root.com.",
		newA("service.root.com.", "10.0.0.1"),
	)
	rootZone.AddRR(newCNAME("*.root.com.", "service.root.com."))

	backupZone := buildZone("backup.com.")

	rrs := Resolve("any.backup.com.", dns.TypeA, "backup.com.", "backup.com.", backupZone, rootZone, false)

	if len(rrs) != 2 {
		t.Fatalf("expected 2 records (CNAME + A), got %d", len(rrs))
	}
	cname := rrs[0].(*dns.CNAME)
	if cname.Hdr.Name != "any.backup.com." {
		t.Errorf("CNAME owner: got %q, want any.backup.com.", cname.Hdr.Name)
	}
	if cname.Target != "service.backup.com." {
		t.Errorf("CNAME target: got %q, want service.backup.com.", cname.Target)
	}
	a := rrs[1].(*dns.A)
	if a.Hdr.Name != "service.backup.com." {
		t.Errorf("A owner: got %q, want service.backup.com.", a.Hdr.Name)
	}
}

// ---------------------------------------------------------------------------
// CNAME chain collapsing — backup-path collapse entry points (design D4/D5)
// ---------------------------------------------------------------------------

func cnameWithTTL(owner, target string, ttl uint32) *dns.CNAME {
	return &dns.CNAME{
		Hdr:    dns.RR_Header{Name: owner, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttl},
		Target: target,
	}
}

func aWithTTL(owner, ip string, ttl uint32) *dns.A {
	rr := newA(owner, ip)
	rr.Hdr.Ttl = ttl
	return rr
}

// collapseRootZone returns the canonical chain in the root.com. namespace:
// www 300 CNAME lb, lb 60 CNAME pool-a, pool-a 600 A 192.0.2.10.
func collapseRootZone() *zone.Zone {
	return buildZone("root.com.",
		cnameWithTTL("www.root.com.", "lb.root.com.", 300),
		cnameWithTTL("lb.root.com.", "pool-a.root.com.", 60),
		aWithTTL("pool-a.root.com.", "192.0.2.10", 600),
	)
}

// The collapsed terminal records carry the backup-namespace on-wire qname as
// owner and the chain-minimum TTL, both written on a copy: the zone-stored
// record must stay untouched.
func TestResolveCNAMEFallbackCollapse_RecordsOwnerCaseTTLAndImmutability(t *testing.T) {
	rootZone := collapseRootZone()
	stored := rootZone.Lookup("pool-a.root.com.", dns.TypeA)

	rrs, nodata := ResolveCNAMEFallbackCollapse("WwW.BaCkup.Com.", dns.TypeA, "backup.com.", "Backup.Com.", rootZone, false)

	if nodata {
		t.Fatal("nodata = true, want false")
	}
	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d: %v", len(rrs), rrs)
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", rrs[0])
	}
	if a.Hdr.Name != "WwW.BaCkup.Com." {
		t.Errorf("owner = %q, want on-wire qname WwW.BaCkup.Com.", a.Hdr.Name)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (min of 300,60,600)", a.Hdr.Ttl)
	}
	if rrs[0] == stored[0] {
		t.Error("returned record is the zone-stored pointer; must be a copy")
	}
	if stored[0].Header().Name != "pool-a.root.com." || stored[0].Header().Ttl != 600 {
		t.Errorf("zone-stored record mutated: owner=%q ttl=%d", stored[0].Header().Name, stored[0].Header().Ttl)
	}
}

// The terminal records' RDATA name fields receive exactly the RewriteRR RDATA
// treatment (in-bailiwick by default, label-anywhere with the flag).
func TestResolveCNAMEFallbackCollapse_TerminalRDATAMatchesRewriteRR(t *testing.T) {
	for _, flag := range []bool{false, true} {
		rootZone := buildZone("root.com.",
			cnameWithTTL("www.root.com.", "mxhost.root.com.", 300),
			newMX("mxhost.root.com.", 10, "mail.root.com."),
		)
		storedMX := rootZone.Lookup("mxhost.root.com.", dns.TypeMX)[0]
		want := RewriteRR(storedMX, "root.com.", "Backup.Com.", flag).(*dns.MX).Mx

		rrs, nodata := ResolveCNAMEFallbackCollapse("www.backup.com.", dns.TypeMX, "backup.com.", "Backup.Com.", rootZone, flag)

		if nodata || len(rrs) != 1 {
			t.Fatalf("flag=%v: nodata=%v len=%d, want answer with 1 record", flag, nodata, len(rrs))
		}
		mx, ok := rrs[0].(*dns.MX)
		if !ok {
			t.Fatalf("flag=%v: expected *dns.MX, got %T", flag, rrs[0])
		}
		if mx.Mx != want {
			t.Errorf("flag=%v: MX RDATA = %q, want %q (RewriteRR parity)", flag, mx.Mx, want)
		}
	}
}

// Spec example "label-anywhere rewrite applies to the synthesized target":
// a templated out-of-zone target embedding the root origin as a middle label
// is rewritten when rewrite_rdata_labels is on, and emitted verbatim when off.
func TestResolveCNAMEFallbackCollapse_TailTemplatedTarget(t *testing.T) {
	build := func() *zone.Zone {
		return buildZone("example.com.",
			cnameWithTTL("www.example.com.", "edge.example.com.", 300),
			cnameWithTTL("edge.example.com.", "www.example.com.cdn-vendor.example.org.", 120),
		)
	}

	t.Run("rewrite_rdata_labels=true", func(t *testing.T) {
		rrs, nodata := ResolveCNAMEFallbackCollapse("www.example.net.", dns.TypeA, "example.net.", "example.net.", build(), true)
		if nodata || len(rrs) != 1 {
			t.Fatalf("nodata=%v len=%d, want 1 synthesized CNAME", nodata, len(rrs))
		}
		cn := rrs[0].(*dns.CNAME)
		if cn.Hdr.Name != "www.example.net." {
			t.Errorf("owner = %q, want www.example.net.", cn.Hdr.Name)
		}
		if cn.Hdr.Ttl != 120 {
			t.Errorf("TTL = %d, want 120 (min of 300,120)", cn.Hdr.Ttl)
		}
		if cn.Target != "www.example.net.cdn-vendor.example.org." {
			t.Errorf("target = %q, want www.example.net.cdn-vendor.example.org.", cn.Target)
		}
	})

	t.Run("rewrite_rdata_labels=false", func(t *testing.T) {
		rrs, nodata := ResolveCNAMEFallbackCollapse("www.example.net.", dns.TypeA, "example.net.", "example.net.", build(), false)
		if nodata || len(rrs) != 1 {
			t.Fatalf("nodata=%v len=%d, want 1 synthesized CNAME", nodata, len(rrs))
		}
		cn := rrs[0].(*dns.CNAME)
		if cn.Target != "www.example.com.cdn-vendor.example.org." {
			t.Errorf("target = %q, want verbatim www.example.com.cdn-vendor.example.org.", cn.Target)
		}
	})
}

// Chain-derived NODATA is expressed as nodata=true with zero records
// (invariant: nodata=true ⇒ len(rrs)==0).
func TestResolveCNAMEFallbackCollapse_NoData(t *testing.T) {
	rrs, nodata := ResolveCNAMEFallbackCollapse("www.backup.com.", dns.TypeAAAA, "backup.com.", "backup.com.", collapseRootZone(), false)
	if !nodata {
		t.Fatal("nodata = false, want true (AAAA over A-only tail)")
	}
	if len(rrs) != 0 {
		t.Errorf("len(rrs) = %d, want 0 (invariant nodata=true ⇒ no records)", len(rrs))
	}
}

// No CNAME at the rewritten qname: (nil, false) so the caller continues to
// the next stage.
func TestResolveCNAMEFallbackCollapse_NoCNAMEFallsThrough(t *testing.T) {
	rootZone := buildZone("root.com.", aWithTTL("plain.root.com.", "192.0.2.1", 300))
	rrs, nodata := ResolveCNAMEFallbackCollapse("other.backup.com.", dns.TypeA, "backup.com.", "backup.com.", rootZone, false)
	if nodata {
		t.Error("nodata = true, want false (stage miss, not chain NODATA)")
	}
	if len(rrs) != 0 {
		t.Errorf("len(rrs) = %d, want 0", len(rrs))
	}
}

// Direct CNAME-type queries through the exact collapse entry follow the
// unified rule: in-zone walk-to-end is NODATA, out-of-zone tail synthesizes.
func TestResolveExactCollapse_DirectCNAME(t *testing.T) {
	t.Run("in-zone tail is NODATA", func(t *testing.T) {
		rrs, nodata := ResolveExactCollapse("www.backup.com.", dns.TypeCNAME, "backup.com.", "backup.com.", nil, collapseRootZone(), false)
		if !nodata {
			t.Fatal("nodata = false, want true")
		}
		if len(rrs) != 0 {
			t.Errorf("len(rrs) = %d, want 0", len(rrs))
		}
	})

	t.Run("out-of-zone tail synthesizes", func(t *testing.T) {
		rootZone := buildZone("root.com.",
			cnameWithTTL("www.root.com.", "lb.root.com.", 300),
			cnameWithTTL("lb.root.com.", "cdn.external-vendor.example.org.", 60),
		)
		rrs, nodata := ResolveExactCollapse("www.backup.com.", dns.TypeCNAME, "backup.com.", "backup.com.", nil, rootZone, false)
		if nodata || len(rrs) != 1 {
			t.Fatalf("nodata=%v len=%d, want 1 synthesized CNAME", nodata, len(rrs))
		}
		cn := rrs[0].(*dns.CNAME)
		if cn.Hdr.Name != "www.backup.com." {
			t.Errorf("owner = %q, want www.backup.com.", cn.Hdr.Name)
		}
		if cn.Target != "cdn.external-vendor.example.org." {
			t.Errorf("target = %q, want cdn.external-vendor.example.org.", cn.Target)
		}
		if cn.Hdr.Ttl != 60 {
			t.Errorf("TTL = %d, want 60 (min of 300,60)", cn.Hdr.Ttl)
		}
	})
}

// For non-CNAME qtypes the exact collapse entry behaves exactly like
// ResolveExactNoCNAME (no chain is involved at an exact qtype hit).
func TestResolveExactCollapse_NonCNAMEDelegates(t *testing.T) {
	rootZone := buildZone("root.com.", aWithTTL("www.root.com.", "192.0.2.7", 300))
	want := ResolveExactNoCNAME("www.backup.com.", dns.TypeA, "backup.com.", "Backup.Com.", nil, rootZone, false)

	rrs, nodata := ResolveExactCollapse("www.backup.com.", dns.TypeA, "backup.com.", "Backup.Com.", nil, rootZone, false)

	if nodata {
		t.Fatal("nodata = true, want false")
	}
	if len(rrs) != len(want) || len(rrs) != 1 {
		t.Fatalf("len(rrs) = %d, want %d", len(rrs), len(want))
	}
	if rrs[0].String() != want[0].String() {
		t.Errorf("rrs[0] = %v, want ResolveExactNoCNAME parity %v", rrs[0], want[0])
	}
}

// A wildcard CNAME chain start collapses through the wildcard collapse entry
// with the backup-namespace on-wire owner and chain-minimum TTL.
func TestResolveWildcardCollapse_WildcardCNAMEStart(t *testing.T) {
	rootZone := buildZone("root.com.",
		cnameWithTTL("*.w.root.com.", "pool-a.root.com.", 300),
		aWithTTL("pool-a.root.com.", "192.0.2.10", 600),
	)
	storedWildcard, found := rootZone.LookupWildcard("host.w.root.com.", dns.TypeCNAME)
	if !found || len(storedWildcard) != 1 {
		t.Fatalf("test setup: wildcard CNAME lookup found=%v len=%d", found, len(storedWildcard))
	}

	rrs, nodata := ResolveWildcardCollapse("HoSt.w.BaCkup.Com.", dns.TypeA, "backup.com.", "Backup.Com.", rootZone, false)

	if nodata {
		t.Fatal("nodata = true, want false")
	}
	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d: %v", len(rrs), rrs)
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", rrs[0])
	}
	if a.Hdr.Name != "HoSt.w.BaCkup.Com." {
		t.Errorf("owner = %q, want on-wire qname HoSt.w.BaCkup.Com.", a.Hdr.Name)
	}
	if a.Hdr.Ttl != 300 {
		t.Errorf("TTL = %d, want 300 (min of 300,600)", a.Hdr.Ttl)
	}
	if storedWildcard[0].Header().Name != "*.w.root.com." {
		t.Errorf("zone-stored wildcard owner mutated to %q", storedWildcard[0].Header().Name)
	}
}

// A dangling wildcard CNAME chain yields nodata=true through the wildcard
// collapse entry.
func TestResolveWildcardCollapse_NoData(t *testing.T) {
	rootZone := buildZone("root.com.",
		cnameWithTTL("*.w.root.com.", "ghost.root.com.", 300),
	)
	rrs, nodata := ResolveWildcardCollapse("host.w.backup.com.", dns.TypeA, "backup.com.", "backup.com.", rootZone, false)
	if !nodata {
		t.Fatal("nodata = false, want true (dangling wildcard chain tail)")
	}
	if len(rrs) != 0 {
		t.Errorf("len(rrs) = %d, want 0", len(rrs))
	}
}

// A plain wildcard hit of the requested qtype involves no chain: the wildcard
// collapse entry matches ResolveWildcard's output.
func TestResolveWildcardCollapse_PlainQtypeMatchesResolveWildcard(t *testing.T) {
	build := func() *zone.Zone {
		return buildZone("root.com.", aWithTTL("*.root.com.", "192.0.2.5", 300))
	}
	want := ResolveWildcard("any.Backup.Com.", dns.TypeA, "backup.com.", "Backup.Com.", build(), false)

	rrs, nodata := ResolveWildcardCollapse("any.Backup.Com.", dns.TypeA, "backup.com.", "Backup.Com.", build(), false)

	if nodata {
		t.Fatal("nodata = true, want false")
	}
	if len(rrs) != len(want) || len(rrs) != 1 {
		t.Fatalf("len(rrs) = %d, want %d", len(rrs), len(want))
	}
	if rrs[0].String() != want[0].String() {
		t.Errorf("rrs[0] = %v, want ResolveWildcard parity %v", rrs[0], want[0])
	}
}

// All collapse entries MUST NOT panic on nil zones.
func TestResolveCollapse_NilRootZone_DoesNotPanic(t *testing.T) {
	if rrs, nodata := ResolveExactCollapse("q.backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, nil, false); rrs != nil || nodata {
		t.Errorf("ResolveExactCollapse(nil root) = (%v, %v), want (nil, false)", rrs, nodata)
	}
	if rrs, nodata := ResolveCNAMEFallbackCollapse("q.backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, false); rrs != nil || nodata {
		t.Errorf("ResolveCNAMEFallbackCollapse(nil root) = (%v, %v), want (nil, false)", rrs, nodata)
	}
	if rrs, nodata := ResolveWildcardCollapse("q.backup.com.", dns.TypeA, "backup.com.", "backup.com.", nil, false); rrs != nil || nodata {
		t.Errorf("ResolveWildcardCollapse(nil root) = (%v, %v), want (nil, false)", rrs, nodata)
	}
}
