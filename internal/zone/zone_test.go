package zone

import (
	"fmt"
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
		Records: make(map[string]*qtypeStore),
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
		Records: make(map[string]*qtypeStore),
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
		Records: make(map[string]*qtypeStore),
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
		Records: make(map[string]*qtypeStore),
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
		Records: make(map[string]*qtypeStore),
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
		Records: make(map[string]*qtypeStore),
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

func TestZone_LookupReturnsSharedBacking(t *testing.T) {
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string]*qtypeStore),
	}
	z.AddRR(newTestA("a.example.com.", "192.0.2.1"))
	z.AddRR(newTestA("a.example.com.", "192.0.2.2"))

	stored := z.Records["a.example.com."].rrs
	if len(stored) != 2 {
		t.Fatalf("stored: got %d records, want 2", len(stored))
	}

	result := z.Lookup("a.example.com.", dns.TypeA)
	if len(result) != 2 {
		t.Fatalf("Lookup: got %d records, want 2", len(result))
	}

	storedBase := &stored[:cap(stored)][0]
	resultBase := &result[:cap(result)][0]
	if storedBase != resultBase {
		t.Errorf("Lookup returned a copy; expected shared backing array\n stored base: %p\n result base: %p",
			storedBase, resultBase)
	}
}

// TestZone_AddRR_PreservesOwnerCase asserts the invariant documented on AddRR:
// the lowercase key is purely an index key, and the stored RR is the same
// pointer with Header().Name byte-for-byte unchanged.
func TestZone_AddRR_PreservesOwnerCase(t *testing.T) {
	z := &Zone{Origin: "root.com.", Records: make(map[string]*qtypeStore)}
	rr := newTestA("Service.Root.Com.", "1.2.3.4")
	z.AddRR(rr)

	// Lookup uses the lowercase-folded owner per RFC 4343.
	got := z.Lookup("service.root.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Lookup: got %d records, want 1", len(got))
	}
	if got[0] != dns.RR(rr) {
		t.Errorf("Lookup did not return the same RR pointer that was inserted")
	}
	if got[0].Header().Name != "Service.Root.Com." {
		t.Errorf("stored owner case: got %q, want %q (byte-for-byte)",
			got[0].Header().Name, "Service.Root.Com.")
	}

	// Lookup with the original mixed case must NOT match (Lookup expects
	// pre-folded keys per its doc comment); this guards against future
	// callers being tempted to skip the fold.
	if got := z.Lookup("Service.Root.Com.", dns.TypeA); len(got) != 0 {
		t.Errorf("Lookup with mixed-case key: got %d records, want 0 (caller must fold)", len(got))
	}
}

func TestQtypeStore_InlineToPromoted(t *testing.T) {
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string]*qtypeStore),
	}
	z.AddRR(newTestA("a.example.com.", "192.0.2.1"))

	s := z.Records["a.example.com."]
	if s == nil || !s.single || s.qtype != dns.TypeA || len(s.rrs) != 1 || s.sub != nil {
		t.Fatalf("after single A insert: expected inline{A, 1 rr}, got %+v", s)
	}

	z.AddRR(&dns.AAAA{
		Hdr:  dns.RR_Header{Name: "a.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
		AAAA: net.ParseIP("2001:db8::1"),
	})

	s = z.Records["a.example.com."]
	if s == nil || s.single || s.sub == nil {
		t.Fatalf("after A+AAAA insert: expected promoted, got %+v", s)
	}
	if len(s.sub[dns.TypeA]) != 1 || len(s.sub[dns.TypeAAAA]) != 1 {
		t.Errorf("promoted sub: got TypeA=%d TypeAAAA=%d, want both 1",
			len(s.sub[dns.TypeA]), len(s.sub[dns.TypeAAAA]))
	}

	if rrs := z.Lookup("a.example.com.", dns.TypeA); len(rrs) != 1 {
		t.Errorf("Lookup TypeA: got %d, want 1", len(rrs))
	}
	if rrs := z.Lookup("a.example.com.", dns.TypeAAAA); len(rrs) != 1 {
		t.Errorf("Lookup TypeAAAA: got %d, want 1", len(rrs))
	}
}

func TestZone_LookupReturnsSharedBacking_Promoted(t *testing.T) {
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string]*qtypeStore),
	}
	z.AddRR(newTestA("b.example.com.", "192.0.2.1"))
	z.AddRR(&dns.AAAA{
		Hdr:  dns.RR_Header{Name: "b.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
		AAAA: net.ParseIP("2001:db8::1"),
	})

	s := z.Records["b.example.com."]
	if s.single {
		t.Fatalf("expected promoted state after 2 qtypes")
	}

	stored := s.sub[dns.TypeA]
	result := z.Lookup("b.example.com.", dns.TypeA)
	if len(stored) != 1 || len(result) != 1 {
		t.Fatalf("got stored=%d result=%d, want both 1", len(stored), len(result))
	}
	if &stored[:cap(stored)][0] != &result[:cap(result)][0] {
		t.Errorf("promoted Lookup returned a copy; backing array differs")
	}
}

func TestZone_LookupSharedBackingAcrossPromotion(t *testing.T) {
	z := &Zone{
		Origin:  "example.com.",
		Records: make(map[string]*qtypeStore),
	}
	z.AddRR(newTestA("c.example.com.", "192.0.2.1"))

	before := z.Lookup("c.example.com.", dns.TypeA)
	if len(before) != 1 {
		t.Fatalf("pre-promotion Lookup TypeA: got %d, want 1", len(before))
	}
	beforeBase := &before[:cap(before)][0]

	z.AddRR(&dns.AAAA{
		Hdr:  dns.RR_Header{Name: "c.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
		AAAA: net.ParseIP("2001:db8::1"),
	})

	s := z.Records["c.example.com."]
	if s.single {
		t.Fatalf("expected promoted state after AAAA insert")
	}

	stored := s.sub[dns.TypeA]
	if &stored[:cap(stored)][0] != beforeBase {
		t.Errorf("promotion broke backing array of pre-existing slice")
	}

	after := z.Lookup("c.example.com.", dns.TypeA)
	if &after[:cap(after)][0] != beforeBase {
		t.Errorf("post-promotion Lookup returned different backing than pre-promotion")
	}
}

func TestZone_LookupNoMatch_ReturnsEmptyLen(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	// Non-existent owner — callers rely on len() == 0, not nil-ness.
	if rrs := z.Lookup("nonexistent.root.com.", dns.TypeA); len(rrs) != 0 {
		t.Errorf("Lookup for missing owner returned %d records, want 0", len(rrs))
	}

	// Existing owner but wrong type.
	if rrs := z.Lookup("root.com.", dns.TypeA); len(rrs) != 0 {
		t.Errorf("Lookup for wrong type returned %d records, want 0", len(rrs))
	}
}

// ---------------------------------------------------------------------------
// FollowCNAME
// ---------------------------------------------------------------------------

func newTestCNAME(owner, target string) *dns.CNAME {
	return &dns.CNAME{
		Hdr:    dns.RR_Header{Name: owner, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: target,
	}
}

func newTestA(owner, ip string) *dns.A {
	return &dns.A{
		Hdr: dns.RR_Header{Name: owner, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP(ip).To4(),
	}
}

func TestFollowCNAME_InZoneSingleHop(t *testing.T) {
	z := &Zone{Origin: "root.com.", Records: make(map[string]*qtypeStore)}
	z.AddRR(newTestCNAME("alias.root.com.", "target.root.com."))
	z.AddRR(newTestA("target.root.com.", "1.2.3.4"))

	initial := z.Lookup("alias.root.com.", dns.TypeCNAME)
	result := z.FollowCNAME(nil, initial, dns.TypeA)

	if len(result) != 2 {
		t.Fatalf("got %d records, want 2", len(result))
	}
	if _, ok := result[0].(*dns.CNAME); !ok {
		t.Fatalf("result[0]: got %T, want *dns.CNAME", result[0])
	}
	a, ok := result[1].(*dns.A)
	if !ok {
		t.Fatalf("result[1]: got %T, want *dns.A", result[1])
	}
	if a.A.String() != "1.2.3.4" {
		t.Errorf("A: got %s, want 1.2.3.4", a.A)
	}
}

func TestFollowCNAME_Chain(t *testing.T) {
	z := &Zone{Origin: "root.com.", Records: make(map[string]*qtypeStore)}
	z.AddRR(newTestCNAME("a.root.com.", "b.root.com."))
	z.AddRR(newTestCNAME("b.root.com.", "c.root.com."))
	z.AddRR(newTestA("c.root.com.", "5.6.7.8"))

	initial := z.Lookup("a.root.com.", dns.TypeCNAME)
	result := z.FollowCNAME(nil, initial, dns.TypeA)

	if len(result) != 3 {
		t.Fatalf("got %d records, want 3 (2 CNAME + 1 A)", len(result))
	}
}

func TestFollowCNAME_OutOfBailiwick(t *testing.T) {
	z := &Zone{Origin: "root.com.", Records: make(map[string]*qtypeStore)}
	z.AddRR(newTestCNAME("ext.root.com.", "target.other.com."))

	initial := z.Lookup("ext.root.com.", dns.TypeCNAME)
	result := z.FollowCNAME(nil, initial, dns.TypeA)

	if len(result) != 1 {
		t.Fatalf("got %d records, want 1 (CNAME only, out-of-bailiwick)", len(result))
	}
}

func TestFollowCNAME_DepthLimit(t *testing.T) {
	z := &Zone{Origin: "root.com.", Records: make(map[string]*qtypeStore)}
	for i := 1; i <= 10; i++ {
		next := i%10 + 1
		z.AddRR(newTestCNAME(
			fmt.Sprintf("c%d.root.com.", i),
			fmt.Sprintf("c%d.root.com.", next),
		))
	}

	initial := z.Lookup("c1.root.com.", dns.TypeCNAME)
	result := z.FollowCNAME(nil, initial, dns.TypeA)

	if len(result) > MaxCNAMEDepth {
		t.Errorf("got %d records, want at most %d (depth limit)", len(result), MaxCNAMEDepth)
	}
	if len(result) < 1 {
		t.Fatal("got 0 records, want at least 1")
	}
}
