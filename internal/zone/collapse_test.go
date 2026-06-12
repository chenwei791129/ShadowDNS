package zone

import (
	"fmt"
	"net"
	"testing"

	"github.com/miekg/dns"
)

// TTL-aware record constructors. newTestCNAME / newTestA in zone_test.go pin
// TTL to 300; collapse tests assert min-TTL math so they need explicit TTLs.

func cnameTTL(owner, target string, ttl uint32) *dns.CNAME {
	return &dns.CNAME{
		Hdr:    dns.RR_Header{Name: owner, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttl},
		Target: target,
	}
}

func aTTL(owner, ip string, ttl uint32) *dns.A {
	return &dns.A{
		Hdr: dns.RR_Header{Name: owner, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		A:   net.ParseIP(ip).To4(),
	}
}

func txtTTL(owner, text string, ttl uint32) *dns.TXT {
	return &dns.TXT{
		Hdr: dns.RR_Header{Name: owner, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl},
		Txt: []string{text},
	}
}

func newCollapseZone() *Zone {
	return &Zone{Origin: "example.com.", Records: make(map[string]*qtypeStore)}
}

// multiHopZone holds the spec's canonical chain:
// www 300 CNAME lb, lb 60 CNAME pool-a, pool-a 600 A 192.0.2.10.
func multiHopZone() *Zone {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "lb.example.com.", 300))
	z.AddRR(cnameTTL("lb.example.com.", "pool-a.example.com.", 60))
	z.AddRR(aTTL("pool-a.example.com.", "192.0.2.10", 600))
	return z
}

// ---------------------------------------------------------------------------
// Outcome: Records
// ---------------------------------------------------------------------------

// An in-zone chain ending at records of the requested qtype yields
// CollapseRecords with the terminal records and the chain-minimum TTL
// (300/60/600 -> 60 per the spec example).
func TestCollapseCNAME_Records_MinTTL(t *testing.T) {
	z := multiHopZone()
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseRecords {
		t.Fatalf("Outcome = %v, want CollapseRecords", res.Outcome)
	}
	if len(res.RRs) != 1 {
		t.Fatalf("len(RRs) = %d, want 1", len(res.RRs))
	}
	if res.MinTTL != 60 {
		t.Errorf("MinTTL = %d, want 60 (min of 300,60,600)", res.MinTTL)
	}
	a, ok := res.RRs[0].(*dns.A)
	if !ok {
		t.Fatalf("RRs[0]: got %T, want *dns.A", res.RRs[0])
	}
	if a.A.String() != "192.0.2.10" {
		t.Errorf("A = %s, want 192.0.2.10", a.A)
	}
}

// CollapseRecords returns the zone-stored slice as-is: the same RR pointers
// the zone holds, with owner and TTL untouched (the caller rewrites on a
// dns.Copy, never in place).
func TestCollapseCNAME_Records_ReturnsStoredSliceUnmodified(t *testing.T) {
	z := multiHopZone()
	stored := z.Lookup("pool-a.example.com.", dns.TypeA)
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseRecords {
		t.Fatalf("Outcome = %v, want CollapseRecords", res.Outcome)
	}
	if res.RRs[0] != stored[0] {
		t.Errorf("RRs[0] is not the zone-stored record (got %p, want %p)", res.RRs[0], stored[0])
	}
	if got := stored[0].Header().Name; got != "pool-a.example.com." {
		t.Errorf("stored owner mutated to %q, want pool-a.example.com.", got)
	}
	if got := stored[0].Header().Ttl; got != 600 {
		t.Errorf("stored TTL mutated to %d, want 600", got)
	}
}

// A chain whose target is the zone apex resolves the apex records, matching
// FollowCNAME's in-bailiwick boundary semantics.
func TestCollapseCNAME_Records_TargetEqualsOrigin(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("alias.example.com.", "example.com.", 300))
	z.AddRR(aTTL("example.com.", "192.0.2.9", 120))
	initial := z.Lookup("alias.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseRecords {
		t.Fatalf("Outcome = %v, want CollapseRecords", res.Outcome)
	}
	if res.MinTTL != 120 {
		t.Errorf("MinTTL = %d, want 120", res.MinTTL)
	}
}

// A wildcard CNAME in the middle of the chain is a hop like any other: the
// chase continues through it to the terminal records.
func TestCollapseCNAME_Records_WildcardIntermediateHop(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "host.pool.example.com.", 300))
	z.AddRR(cnameTTL("*.pool.example.com.", "final.example.com.", 120))
	z.AddRR(aTTL("final.example.com.", "192.0.2.20", 600))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseRecords {
		t.Fatalf("Outcome = %v, want CollapseRecords", res.Outcome)
	}
	if res.MinTTL != 120 {
		t.Errorf("MinTTL = %d, want 120 (min of 300,120,600)", res.MinTTL)
	}
}

// A chain tail covered by a wildcard that holds the requested qtype yields
// the wildcard's stored records; the stored slice keeps its "*." owner (the
// caller rewrites owner on a copy).
func TestCollapseCNAME_Records_WildcardTerminal(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "host.pool.example.com.", 300))
	z.AddRR(aTTL("*.pool.example.com.", "192.0.2.30", 600))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseRecords {
		t.Fatalf("Outcome = %v, want CollapseRecords", res.Outcome)
	}
	stored, found := z.LookupWildcard("host.pool.example.com.", dns.TypeA)
	if !found || len(stored) != 1 {
		t.Fatalf("test setup: wildcard lookup found=%v len=%d", found, len(stored))
	}
	if res.RRs[0] != stored[0] {
		t.Errorf("RRs[0] is not the zone-stored wildcard record")
	}
	if got := stored[0].Header().Name; got != "*.pool.example.com." {
		t.Errorf("stored wildcard owner mutated to %q", got)
	}
	if res.MinTTL != 300 {
		t.Errorf("MinTTL = %d, want 300 (min of 300,600)", res.MinTTL)
	}
}

// ---------------------------------------------------------------------------
// Outcome: Tail (chain leaves the zone or budget exhausted)
// ---------------------------------------------------------------------------

// An out-of-zone target ends the chase with CollapseTail; MinTTL spans every
// consumed CNAME including the one pointing out.
func TestCollapseCNAME_Tail_OutOfZone(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "lb.example.com.", 300))
	z.AddRR(cnameTTL("lb.example.com.", "pool-a.example.com.", 60))
	z.AddRR(cnameTTL("pool-a.example.com.", "cdn.external-vendor.example.org.", 600))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseTail {
		t.Fatalf("Outcome = %v, want CollapseTail", res.Outcome)
	}
	if res.Target != "cdn.external-vendor.example.org." {
		t.Errorf("Target = %q, want cdn.external-vendor.example.org.", res.Target)
	}
	if res.MinTTL != 60 {
		t.Errorf("MinTTL = %d, want 60 (min of 300,60,600)", res.MinTTL)
	}
	if len(res.RRs) != 0 {
		t.Errorf("len(RRs) = %d, want 0 for Tail outcome", len(res.RRs))
	}
}

// The synthesized target preserves the zone-file original case byte-for-byte.
func TestCollapseCNAME_Tail_TargetPreservesZoneFileCase(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("ext.example.com.", "CDN.Vendor.Example.ORG.", 300))
	initial := z.Lookup("ext.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseTail {
		t.Fatalf("Outcome = %v, want CollapseTail", res.Outcome)
	}
	if res.Target != "CDN.Vendor.Example.ORG." {
		t.Errorf("Target = %q, want CDN.Vendor.Example.ORG. (original case)", res.Target)
	}
}

// Depth budget: exactly 8 consumed CNAMEs whose 8th target holds terminal
// records still resolves (terminal resolution does not consume budget).
func TestCollapseCNAME_DepthBudget_Exactly8ResolvesTerminal(t *testing.T) {
	z := newCollapseZone()
	for i := 1; i < MaxCNAMEDepth; i++ {
		z.AddRR(cnameTTL(
			fmt.Sprintf("c%d.example.com.", i),
			fmt.Sprintf("c%d.example.com.", i+1), 300))
	}
	z.AddRR(cnameTTL(fmt.Sprintf("c%d.example.com.", MaxCNAMEDepth), "final.example.com.", 300))
	z.AddRR(aTTL("final.example.com.", "192.0.2.40", 600))
	initial := z.Lookup("c1.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseRecords {
		t.Fatalf("Outcome = %v, want CollapseRecords (8 consumed CNAMEs, terminal free)", res.Outcome)
	}
	if res.RRs[0].(*dns.A).A.String() != "192.0.2.40" {
		t.Errorf("terminal A = %s, want 192.0.2.40", res.RRs[0].(*dns.A).A)
	}
}

// Depth budget: a 9-CNAME chain exhausts the budget after the 8th record;
// the outcome is Tail with target = the 8th CNAME's target (the 9th's owner).
func TestCollapseCNAME_DepthBudget_9thCNAMEIsTail(t *testing.T) {
	z := newCollapseZone()
	for i := 1; i <= MaxCNAMEDepth+1; i++ {
		z.AddRR(cnameTTL(
			fmt.Sprintf("c%d.example.com.", i),
			fmt.Sprintf("c%d.example.com.", i+1), 300))
	}
	z.AddRR(aTTL(fmt.Sprintf("c%d.example.com.", MaxCNAMEDepth+2), "192.0.2.50", 600))
	initial := z.Lookup("c1.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseTail {
		t.Fatalf("Outcome = %v, want CollapseTail (budget exhausted)", res.Outcome)
	}
	want := fmt.Sprintf("c%d.example.com.", MaxCNAMEDepth+1)
	if res.Target != want {
		t.Errorf("Target = %q, want %q (the 8th CNAME's target)", res.Target, want)
	}
}

// A two-record loop exhausts the budget and yields a self-referential Tail
// when the cutoff target equals the query name (documented loop artifact).
func TestCollapseCNAME_Loop_SelfReferentialCutoff(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("a.example.com.", "b.example.com.", 300))
	z.AddRR(cnameTTL("b.example.com.", "a.example.com.", 300))
	initial := z.Lookup("a.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseTail {
		t.Fatalf("Outcome = %v, want CollapseTail (loop budget exhaustion)", res.Outcome)
	}
	// Consumption sequence a,b,a,b,a,b,a,b: the 8th consumed CNAME is b's,
	// whose target is the query name itself.
	if res.Target != "a.example.com." {
		t.Errorf("Target = %q, want a.example.com. (self-referential cutoff)", res.Target)
	}
}

// ---------------------------------------------------------------------------
// Outcome: NoData
// ---------------------------------------------------------------------------

// A chain ending at an in-zone name lacking the requested qtype is NODATA.
func TestCollapseCNAME_NoData_ExactTailWithoutQtype(t *testing.T) {
	z := multiHopZone()
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeAAAA)

	if res.Outcome != CollapseNoData {
		t.Fatalf("Outcome = %v, want CollapseNoData", res.Outcome)
	}
	if len(res.RRs) != 0 {
		t.Errorf("len(RRs) = %d, want 0", len(res.RRs))
	}
}

// RFC 4592 wildcard-NODATA at the tail: the wildcard covers the tail name but
// supplies neither the qtype nor a CNAME.
func TestCollapseCNAME_NoData_WildcardTailWithoutQtypeOrCNAME(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "host.pool.example.com.", 300))
	z.AddRR(txtTTL("*.pool.example.com.", "pool", 600))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseNoData {
		t.Fatalf("Outcome = %v, want CollapseNoData (wildcard-NODATA tail)", res.Outcome)
	}
}

// A dangling tail (target name absent from the zone) is NODATA, never an
// error: the original query name exists, so NXDOMAIN would be wrong.
func TestCollapseCNAME_NoData_DanglingTail(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "ghost.example.com.", 300))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeA)

	if res.Outcome != CollapseNoData {
		t.Fatalf("Outcome = %v, want CollapseNoData (dangling tail)", res.Outcome)
	}
}

// ---------------------------------------------------------------------------
// qtype = CNAME: records are hops only
// ---------------------------------------------------------------------------

// Direct CNAME queries never surface a stored CNAME as terminal data: a chain
// that stays in-zone ends NODATA.
func TestCollapseCNAME_QtypeCNAME_InZoneTailIsNoData(t *testing.T) {
	z := multiHopZone()
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeCNAME)

	if res.Outcome != CollapseNoData {
		t.Fatalf("Outcome = %v, want CollapseNoData (CNAMEs are hops when qtype=CNAME)", res.Outcome)
	}
}

// Direct CNAME queries over an out-of-zone tail yield the synthesized Tail.
func TestCollapseCNAME_QtypeCNAME_OutOfZoneTail(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "lb.example.com.", 300))
	z.AddRR(cnameTTL("lb.example.com.", "pool-a.example.com.", 60))
	z.AddRR(cnameTTL("pool-a.example.com.", "cdn.external-vendor.example.org.", 600))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeCNAME)

	if res.Outcome != CollapseTail {
		t.Fatalf("Outcome = %v, want CollapseTail", res.Outcome)
	}
	if res.Target != "cdn.external-vendor.example.org." {
		t.Errorf("Target = %q, want cdn.external-vendor.example.org.", res.Target)
	}
}

// A wildcard CNAME hop is still a hop when qtype=CNAME (the wildcard qtype
// step is skipped, the wildcard CNAME step is not).
func TestCollapseCNAME_QtypeCNAME_WildcardHopStillHops(t *testing.T) {
	z := newCollapseZone()
	z.AddRR(cnameTTL("www.example.com.", "host.pool.example.com.", 300))
	z.AddRR(cnameTTL("*.pool.example.com.", "out.example.org.", 120))
	initial := z.Lookup("www.example.com.", dns.TypeCNAME)

	res := z.CollapseCNAME(initial, dns.TypeCNAME)

	if res.Outcome != CollapseTail {
		t.Fatalf("Outcome = %v, want CollapseTail", res.Outcome)
	}
	if res.Target != "out.example.org." {
		t.Errorf("Target = %q, want out.example.org.", res.Target)
	}
}

// ---------------------------------------------------------------------------
// Robustness
// ---------------------------------------------------------------------------

// CollapseCNAME MUST NOT panic for any input shape.
func TestCollapseCNAME_NoPanicOnDegenerateInputs(t *testing.T) {
	z := multiHopZone()
	inputs := []struct {
		name    string
		initial []dns.RR
		qtype   uint16
	}{
		{"nil initial", nil, dns.TypeA},
		{"empty initial", []dns.RR{}, dns.TypeA},
		{"non-CNAME initial", []dns.RR{aTTL("www.example.com.", "192.0.2.1", 300)}, dns.TypeA},
		{"qtype zero", z.Lookup("www.example.com.", dns.TypeCNAME), 0},
		{"meta qtype ANY", z.Lookup("www.example.com.", dns.TypeCNAME), dns.TypeANY},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("CollapseCNAME panicked: %v", r)
				}
			}()
			_ = z.CollapseCNAME(tc.initial, tc.qtype)
		})
	}
}

// ---------------------------------------------------------------------------
// Parity with FollowCNAME
// ---------------------------------------------------------------------------

// For chains of length <= 7 that terminate in-zone, the collapse terminal
// must equal the records FollowCNAME appends after the chain — the two
// chases agree on the endpoint. Chains >= 8 are excluded: FollowCNAME's
// iteration budget stops before resolving the terminal there (see design D3).
func TestCollapseCNAME_ParityWithFollowCNAME(t *testing.T) {
	for length := 1; length <= MaxCNAMEDepth-1; length++ {
		t.Run(fmt.Sprintf("chain-length-%d", length), func(t *testing.T) {
			z := newCollapseZone()
			for i := 1; i < length; i++ {
				z.AddRR(cnameTTL(
					fmt.Sprintf("c%d.example.com.", i),
					fmt.Sprintf("c%d.example.com.", i+1), 300))
			}
			z.AddRR(cnameTTL(fmt.Sprintf("c%d.example.com.", length), "final.example.com.", 300))
			z.AddRR(aTTL("final.example.com.", "192.0.2.60", 600))
			initial := z.Lookup("c1.example.com.", dns.TypeCNAME)

			followed := z.FollowCNAME(nil, initial, dns.TypeA)
			res := z.CollapseCNAME(initial, dns.TypeA)

			if res.Outcome != CollapseRecords {
				t.Fatalf("Outcome = %v, want CollapseRecords", res.Outcome)
			}
			terminal := followed[len(followed)-len(res.RRs):]
			for i := range res.RRs {
				if res.RRs[i] != terminal[i] {
					t.Errorf("RRs[%d] = %v, want FollowCNAME terminal %v", i, res.RRs[i], terminal[i])
				}
			}
		})
	}
}
