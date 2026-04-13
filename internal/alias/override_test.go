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
		Records: make(map[string][]dns.RR),
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

	rrs := Resolve("backup.com.", dns.TypeTXT, backupZone, rootZone)

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

	rrs := Resolve("backup.com.", dns.TypeMX, backupZone, rootZone)

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

	rrs := Resolve("_sip._tcp.backup.com.", dns.TypeSRV, backupZone, rootZone)

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

	rrs := Resolve("www.backup.com.", dns.TypeA, nil, rootZone)

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

	rrs := Resolve("backup.com.", dns.TypeA, backupZone, rootZone)

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

	rrs := Resolve("www.backup.com.", dns.TypeA, backupZone, rootZone)

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
	_ = Resolve("backup.com.", dns.TypeA, nil, nil)
}
