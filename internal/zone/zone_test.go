package zone

import (
	"net"
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

// ---------------------------------------------------------------------------
// LookupWildcard tests (RFC 4592 wildcard matching)
// ---------------------------------------------------------------------------

func TestZone_LookupWildcard_SingleLevel(t *testing.T) {
	// *.example.com. A 1.2.3.4 — query foo.example.com. should match.
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string][]dns.RR),
	}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4").To4(),
	})
	z.AddRR(makeTestSOA("example.com."))

	rrs, found := z.LookupWildcard("foo.example.com.", dns.TypeA)
	if !found {
		t.Fatal("expected wildcard match, got not found")
	}
	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatal("record is not *dns.A")
	}
	if a.A.String() != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", a.A.String())
	}
}

func TestZone_LookupWildcard_MultiLevel(t *testing.T) {
	// *.example.com. A 1.2.3.4 — query foo.bar.example.com. should match
	// when bar.example.com. does not exist.
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string][]dns.RR),
	}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4").To4(),
	})
	z.AddRR(makeTestSOA("example.com."))

	rrs, found := z.LookupWildcard("foo.bar.example.com.", dns.TypeA)
	if !found {
		t.Fatal("expected wildcard match, got not found")
	}
	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
}

func TestZone_LookupWildcard_ENTBlocking(t *testing.T) {
	// *.example.com. exists, but sub.example.com. also exists (ENT).
	// Query other.sub.example.com. should NOT match because sub.example.com.
	// is an ENT blocker.
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string][]dns.RR),
	}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4").To4(),
	})
	z.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: "sub.example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"exists"},
	})
	z.AddRR(makeTestSOA("example.com."))

	rrs, found := z.LookupWildcard("other.sub.example.com.", dns.TypeA)
	if found {
		t.Error("expected ENT blocking to prevent wildcard match")
	}
	if len(rrs) != 0 {
		t.Errorf("expected 0 records, got %d", len(rrs))
	}
}

func TestZone_LookupWildcard_MoreSpecificWins(t *testing.T) {
	// *.example.com. A 1.1.1.1 and *.sub.example.com. A 2.2.2.2
	// Query foo.sub.example.com. should match *.sub.example.com.
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string][]dns.RR),
	}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.1.1.1").To4(),
	})
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.sub.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("2.2.2.2").To4(),
	})
	z.AddRR(makeTestSOA("example.com."))

	rrs, found := z.LookupWildcard("foo.sub.example.com.", dns.TypeA)
	if !found {
		t.Fatal("expected wildcard match")
	}
	if len(rrs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rrs))
	}
	a, ok := rrs[0].(*dns.A)
	if !ok {
		t.Fatal("record is not *dns.A")
	}
	if a.A.String() != "2.2.2.2" {
		t.Errorf("expected 2.2.2.2 (more specific wildcard), got %s", a.A.String())
	}
}

func TestZone_LookupWildcard_NoWildcard(t *testing.T) {
	// Zone with no wildcard records — lookup should return empty.
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string][]dns.RR),
	}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4").To4(),
	})
	z.AddRR(makeTestSOA("example.com."))

	rrs, found := z.LookupWildcard("foo.example.com.", dns.TypeA)
	if found {
		t.Error("expected no wildcard match")
	}
	if len(rrs) != 0 {
		t.Errorf("expected 0 records, got %d", len(rrs))
	}
}

func TestZone_LookupWildcard_QtypeMismatch_NODATA(t *testing.T) {
	// *.example.com. has only A records — query AAAA should return empty (NODATA).
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string][]dns.RR),
	}
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4").To4(),
	})
	z.AddRR(makeTestSOA("example.com."))

	rrs, found := z.LookupWildcard("foo.example.com.", dns.TypeAAAA)
	if !found {
		t.Fatal("expected wildcard match (found=true with empty records for NODATA)")
	}
	if len(rrs) != 0 {
		t.Errorf("expected 0 records for qtype mismatch, got %d", len(rrs))
	}
}

// makeTestSOA builds a minimal SOA for zone test fixtures.
func makeTestSOA(origin string) *dns.SOA {
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: origin, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:      "ns1." + origin,
		Mbox:    "admin." + origin,
		Serial:  1,
		Refresh: 300,
		Retry:   120,
		Expire:  86400,
		Minttl:  300,
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
