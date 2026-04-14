package zone

import (
	"testing"

	"github.com/miekg/dns"
)

func TestZone_LookupByOwner(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
@ IN A 9.10.11.12
www IN A 1.2.3.4
mail IN A 5.6.7.8
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	cases := []struct {
		owner string
		qtype uint16
		want  int
	}{
		{"www.root.com.", dns.TypeA, 1},
		{"mail.root.com.", dns.TypeA, 1},
		{"root.com.", dns.TypeA, 1},
	}
	for _, tc := range cases {
		rrs := z.Lookup(tc.owner, tc.qtype)
		if len(rrs) != tc.want {
			t.Errorf("Lookup(%q, A): got %d records, want %d", tc.owner, len(rrs), tc.want)
		}
	}
}

func TestZone_SOAPointerSet(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 42 300 120 86400 3600 )
@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	if z.SOA == nil {
		t.Fatal("expected SOA pointer to be set, got nil")
	}
	if z.SOA.Serial != 42 {
		t.Errorf("SOA.Serial: got %d, want 42", z.SOA.Serial)
	}

	// SOA must also appear in Records[Origin].
	soaRRs := z.Lookup(z.Origin, dns.TypeSOA)
	if len(soaRRs) != 1 {
		t.Fatalf("expected SOA in Records[Origin], got %d entries", len(soaRRs))
	}
}

func TestZone_DefaultTTLFromDirective(t *testing.T) {
	content := `$TTL 300
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
www IN A 1.2.3.4
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	aRRs := z.Lookup("www.root.com.", dns.TypeA)
	if len(aRRs) != 1 {
		t.Fatalf("expected 1 A record, got %d", len(aRRs))
	}
	if aRRs[0].Header().Ttl != 300 {
		t.Errorf("TTL: got %d, want 300", aRRs[0].Header().Ttl)
	}
}

func TestZone_PerRecordTTLOverridesDefault(t *testing.T) {
	content := `$TTL 300
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
www 600 IN A 1.2.3.4
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	aRRs := z.Lookup("www.root.com.", dns.TypeA)
	if len(aRRs) != 1 {
		t.Fatalf("expected 1 A record, got %d", len(aRRs))
	}
	if aRRs[0].Header().Ttl != 600 {
		t.Errorf("TTL: got %d, want 600 (per-record override)", aRRs[0].Header().Ttl)
	}
}

func TestZone_LookupNoMatch_ReturnsEmptySlice(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	// Non-existent owner — must return empty slice, not nil.
	rrs := z.Lookup("nonexistent.root.com.", dns.TypeA)
	if rrs == nil {
		t.Error("Lookup for missing owner returned nil, want empty slice")
	}
	if len(rrs) != 0 {
		t.Errorf("Lookup for missing owner returned %d records, want 0", len(rrs))
	}

	// Existing owner but wrong type.
	rrs = z.Lookup("root.com.", dns.TypeA)
	if rrs == nil {
		t.Error("Lookup for wrong type returned nil, want empty slice")
	}
}
