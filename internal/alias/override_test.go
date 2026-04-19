package alias

import (
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// buildZone creates a minimal Zone with the given origin and records.
func buildZone(origin string, rrs ...dns.RR) *zone.Zone {
	z := &zone.Zone{
		Origin:  origin,
		Records: make(map[string]map[uint16][]dns.RR),
	}
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

	rrs := Resolve("backup.com.", dns.TypeTXT, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("backup.com.", dns.TypeMX, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("_sip._tcp.backup.com.", dns.TypeSRV, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("www.backup.com.", dns.TypeA, "backup.com.", nil, rootZone)

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

	rrs := Resolve("backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("www.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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
	rrs := Resolve("sub.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("sub.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

	if len(rrs) != 0 {
		t.Errorf("expected empty result, got %d records", len(rrs))
	}
}

func TestResolve_NilRootZone_DoesNotPanic(t *testing.T) {
	// Should not panic even with nil rootZone.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resolve panicked with nil rootZone: %v", r)
		}
	}()
	_ = Resolve("backup.com.", dns.TypeA, "backup.com.", nil, nil)
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

	rrs := Resolve("app.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("a.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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

	rrs := Resolve("app.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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

func TestResolve_CNAMEFollowing_WildcardInZone(t *testing.T) {
	rootZone := buildZone("root.com.",
		newA("service.root.com.", "10.0.0.1"),
	)
	rootZone.AddRR(newCNAME("*.root.com.", "service.root.com."))

	backupZone := buildZone("backup.com.")

	rrs := Resolve("any.backup.com.", dns.TypeA, "backup.com.", backupZone, rootZone)

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
