package alias

import (
	"fmt"
	"net"
	"testing"

	"github.com/miekg/dns"
)

// ---- RewriteQName (5.2) ----

func TestRewriteQName(t *testing.T) {
	tests := []struct {
		name   string
		qname  string
		backup string
		root   string
		want   string
	}{
		{
			name:   "apex backup → apex root",
			qname:  "backup.com.",
			backup: "backup.com.",
			root:   "root.com.",
			want:   "root.com.",
		},
		{
			name:   "subdomain backup → subdomain root",
			qname:  "www.backup.com.",
			backup: "backup.com.",
			root:   "root.com.",
			want:   "www.root.com.",
		},
		{
			name:   "deep subdomain rewritten correctly",
			qname:  "a.b.c.backup.com.",
			backup: "backup.com.",
			root:   "root.com.",
			want:   "a.b.c.root.com.",
		},
		{
			name:   "qname equals backup zone exactly → root zone",
			qname:  "backup.net.",
			backup: "backup.net.",
			root:   "primary.net.",
			want:   "primary.net.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteQName(tc.qname, tc.backup, tc.root)
			if got != tc.want {
				t.Errorf("RewriteQName(%q, %q, %q) = %q, want %q",
					tc.qname, tc.backup, tc.root, got, tc.want)
			}
		})
	}
}

// ---- RewriteName (5.3 / in-bailiwick rule) ----

func TestRewriteName(t *testing.T) {
	tests := []struct {
		name   string
		n      string
		root   string
		backup string
		want   string
	}{
		{
			name:   "apex root rewritten to apex backup",
			n:      "root.com.",
			root:   "root.com.",
			backup: "backup.com.",
			want:   "backup.com.",
		},
		{
			name:   "subdomain of root rewritten to subdomain of backup",
			n:      "www.root.com.",
			root:   "root.com.",
			backup: "backup.com.",
			want:   "www.backup.com.",
		},
		{
			name:   "third-party name preserved",
			n:      "cdn.amazonaws.com.",
			root:   "root.com.",
			backup: "backup.com.",
			want:   "cdn.amazonaws.com.",
		},
		{
			name:   "partial suffix not rewritten (e.g. notroot.com. vs root.com.)",
			n:      "notroot.com.",
			root:   "root.com.",
			backup: "backup.com.",
			want:   "notroot.com.",
		},
		{
			name:   "deep subdomain rewritten",
			n:      "a.b.root.com.",
			root:   "root.com.",
			backup: "backup.com.",
			want:   "a.b.backup.com.",
		},
		{
			name:   "empty name returned as-is",
			n:      "",
			root:   "root.com.",
			backup: "backup.com.",
			want:   "",
		},
		// Case-preservation contract: root is lookup-fold (lowercase), backup
		// is original yaml case, n is the on-wire query/RDATA name. Output
		// preserves n's original-case prefix and emits backup verbatim.
		{
			name:   "mixed-case query of apex root rewritten to original-case backup",
			n:      "RoOt.CoM.",
			root:   "root.com.",
			backup: "BackUp.Com.",
			want:   "BackUp.Com.",
		},
		{
			name:   "mixed-case query of subdomain preserves prefix case and emits original-case backup",
			n:      "WwW.RoOt.CoM.",
			root:   "root.com.",
			backup: "BackUp.Com.",
			want:   "WwW.BackUp.Com.",
		},
		{
			name:   "all-uppercase query under root preserves prefix and emits original-case backup",
			n:      "WWW.ROOT.COM.",
			root:   "root.com.",
			backup: "BackUp.Com.",
			want:   "WWW.BackUp.Com.",
		},
		{
			name:   "non-matching mixed-case name preserved unchanged",
			n:      "Cdn.AmazonAWS.com.",
			root:   "root.com.",
			backup: "BackUp.Com.",
			want:   "Cdn.AmazonAWS.com.",
		},
		{
			name:   "deep mixed-case subdomain rewritten with prefix preserved",
			n:      "A.b.RooT.cOm.",
			root:   "root.com.",
			backup: "BACKUP.COM.",
			want:   "A.b.BACKUP.COM.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteName(tc.n, tc.root, tc.backup)
			if got != tc.want {
				t.Errorf("RewriteName(%q, %q, %q) = %q, want %q",
					tc.n, tc.root, tc.backup, got, tc.want)
			}
		})
	}
}

func TestRewriteName_BoundaryCases(t *testing.T) {
	const root = "alias.com."
	const backup = "real.com."
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "subdomain rewritten", in: "WWW.alias.com.", want: "WWW.real.com."},
		{name: "apex rewritten", in: "alias.com.", want: "real.com."},
		{name: "unrelated preserved", in: "other.com.", want: "other.com."},
		{name: "boundary missing dot", in: "XXalias.com.", want: "XXalias.com."},
		{name: "single label boundary hit", in: "a.alias.com.", want: "a.real.com."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteName(tc.in, root, backup)
			if got != tc.want {
				t.Errorf("RewriteName(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

var benchRewriteNameSink string

func BenchmarkRewriteName_SuffixMatch(b *testing.B) {
	const root = "alias.com."
	const backup = "real.com."
	in := "www.alias.com."
	b.ReportAllocs()
	var sink string
	for range b.N {
		sink = RewriteName(in, root, backup)
	}
	benchRewriteNameSink = sink
}

// BenchmarkRewriteName_NoMatch records the no-match baseline.
func BenchmarkRewriteName_NoMatch(b *testing.B) {
	const root = "alias.com."
	const backup = "real.com."
	in := "other.example.com."
	b.ReportAllocs()
	var sink string
	for range b.N {
		sink = RewriteName(in, root, backup)
	}
	benchRewriteNameSink = sink
}

// ---- RewriteRR (5.4) ----

func newA(owner string, ip string) *dns.A {
	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: net.ParseIP(ip).To4(),
	}
	return rr
}

func newAAAA(owner string, ip string) *dns.AAAA {
	rr := &dns.AAAA{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeAAAA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		AAAA: net.ParseIP(ip),
	}
	return rr
}

func newCNAME(owner, target string) *dns.CNAME {
	return &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Target: target,
	}
}

func newNS(owner, ns string) *dns.NS {
	return &dns.NS{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeNS,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ns: ns,
	}
}

func newMX(owner string, pref uint16, mx string) *dns.MX {
	return &dns.MX{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeMX,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Preference: pref,
		Mx:         mx,
	}
}

func newPTR(owner, ptr string) *dns.PTR {
	return &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ptr: ptr,
	}
}

func newSRV(owner string, prio, weight, port uint16, target string) *dns.SRV {
	return &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Priority: prio,
		Weight:   weight,
		Port:     port,
		Target:   target,
	}
}

func newSOA(owner, ns, mbox string, serial, refresh, retry, expire, minttl uint32) *dns.SOA {
	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ns:      ns,
		Mbox:    mbox,
		Serial:  serial,
		Refresh: refresh,
		Retry:   retry,
		Expire:  expire,
		Minttl:  minttl,
	}
}

func newTXT(owner string, txts ...string) *dns.TXT {
	return &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Txt: txts,
	}
}

const (
	root   = "root.com."
	backup = "backup.com."
)

func TestRewriteRR_A(t *testing.T) {
	orig := newA("www.root.com.", "1.2.3.4")
	got := RewriteRR(orig, root, backup, false)

	// owner rewritten
	if got.Header().Name != "www.backup.com." {
		t.Errorf("A owner: got %q, want %q", got.Header().Name, "www.backup.com.")
	}
	// IP unchanged
	a := got.(*dns.A)
	if a.A.String() != "1.2.3.4" {
		t.Errorf("A IP: got %q, want 1.2.3.4", a.A.String())
	}
	// original not mutated
	if orig.Header().Name != "www.root.com." {
		t.Errorf("original A mutated: name = %q", orig.Header().Name)
	}
}

func TestRewriteRR_AAAA(t *testing.T) {
	orig := newAAAA("v6.root.com.", "2001:db8::1")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "v6.backup.com." {
		t.Errorf("AAAA owner: got %q, want %q", got.Header().Name, "v6.backup.com.")
	}
	aaaa := got.(*dns.AAAA)
	if aaaa.AAAA.String() != "2001:db8::1" {
		t.Errorf("AAAA IP: got %q, want 2001:db8::1", aaaa.AAAA.String())
	}
	// original not mutated
	if orig.Header().Name != "v6.root.com." {
		t.Errorf("original AAAA mutated")
	}
}

func TestRewriteRR_TXT(t *testing.T) {
	// TXT strings that contain root domain should NOT be rewritten.
	orig := newTXT("root.com.", "v=spf1 include:root.com. ~all", "plain text")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "backup.com." {
		t.Errorf("TXT owner: got %q, want %q", got.Header().Name, "backup.com.")
	}
	txt := got.(*dns.TXT)
	if txt.Txt[0] != "v=spf1 include:root.com. ~all" {
		t.Errorf("TXT string[0] modified: got %q", txt.Txt[0])
	}
	if txt.Txt[1] != "plain text" {
		t.Errorf("TXT string[1] modified: got %q", txt.Txt[1])
	}
}

func TestRewriteRR_CNAME_inBailiwick(t *testing.T) {
	orig := newCNAME("alias.root.com.", "canonical.root.com.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "alias.backup.com." {
		t.Errorf("CNAME owner: got %q, want %q", got.Header().Name, "alias.backup.com.")
	}
	c := got.(*dns.CNAME)
	if c.Target != "canonical.backup.com." {
		t.Errorf("CNAME target: got %q, want %q", c.Target, "canonical.backup.com.")
	}
}

func TestRewriteRR_CNAME_external(t *testing.T) {
	orig := newCNAME("alias.root.com.", "target.amazonaws.com.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "alias.backup.com." {
		t.Errorf("CNAME owner: got %q, want %q", got.Header().Name, "alias.backup.com.")
	}
	c := got.(*dns.CNAME)
	if c.Target != "target.amazonaws.com." {
		t.Errorf("CNAME external target modified: got %q", c.Target)
	}
}

func TestRewriteRR_NS_inBailiwick(t *testing.T) {
	orig := newNS("root.com.", "ns1.root.com.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "backup.com." {
		t.Errorf("NS owner: got %q, want %q", got.Header().Name, "backup.com.")
	}
	n := got.(*dns.NS)
	if n.Ns != "ns1.backup.com." {
		t.Errorf("NS ns: got %q, want %q", n.Ns, "ns1.backup.com.")
	}
}

func TestRewriteRR_NS_external(t *testing.T) {
	orig := newNS("root.com.", "ns1.externaldns.net.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "backup.com." {
		t.Errorf("NS owner: got %q, want %q", got.Header().Name, "backup.com.")
	}
	n := got.(*dns.NS)
	if n.Ns != "ns1.externaldns.net." {
		t.Errorf("NS external ns modified: got %q", n.Ns)
	}
}

func TestRewriteRR_MX_inBailiwick(t *testing.T) {
	orig := newMX("root.com.", 10, "mail.root.com.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "backup.com." {
		t.Errorf("MX owner: got %q, want %q", got.Header().Name, "backup.com.")
	}
	m := got.(*dns.MX)
	if m.Mx != "mail.backup.com." {
		t.Errorf("MX mx: got %q, want %q", m.Mx, "mail.backup.com.")
	}
	if m.Preference != 10 {
		t.Errorf("MX preference changed: got %d, want 10", m.Preference)
	}
}

func TestRewriteRR_PTR(t *testing.T) {
	orig := newPTR("4.3.2.1.in-addr.arpa.", "www.root.com.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "4.3.2.1.in-addr.arpa." {
		t.Errorf("PTR owner should not change (out of bailiwick): got %q", got.Header().Name)
	}
	p := got.(*dns.PTR)
	if p.Ptr != "www.backup.com." {
		t.Errorf("PTR ptr: got %q, want %q", p.Ptr, "www.backup.com.")
	}
}

func TestRewriteRR_SRV_inBailiwick(t *testing.T) {
	orig := newSRV("_http._tcp.root.com.", 10, 20, 80, "app.root.com.")
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "_http._tcp.backup.com." {
		t.Errorf("SRV owner: got %q, want %q", got.Header().Name, "_http._tcp.backup.com.")
	}
	s := got.(*dns.SRV)
	if s.Target != "app.backup.com." {
		t.Errorf("SRV target: got %q, want %q", s.Target, "app.backup.com.")
	}
	if s.Priority != 10 || s.Weight != 20 || s.Port != 80 {
		t.Errorf("SRV numeric fields changed: %d/%d/%d", s.Priority, s.Weight, s.Port)
	}
}

func TestRewriteRR_SOA(t *testing.T) {
	orig := newSOA("root.com.", "ns1.root.com.", "admin.root.com.", 2024010101, 3600, 900, 604800, 300)
	got := RewriteRR(orig, root, backup, false)

	if got.Header().Name != "backup.com." {
		t.Errorf("SOA owner: got %q, want %q", got.Header().Name, "backup.com.")
	}
	s := got.(*dns.SOA)
	if s.Ns != "ns1.backup.com." {
		t.Errorf("SOA MNAME: got %q, want %q", s.Ns, "ns1.backup.com.")
	}
	if s.Mbox != "admin.backup.com." {
		t.Errorf("SOA RNAME: got %q, want %q", s.Mbox, "admin.backup.com.")
	}
	// Numeric fields must be verbatim.
	if s.Serial != 2024010101 {
		t.Errorf("SOA serial changed: got %d", s.Serial)
	}
	if s.Refresh != 3600 || s.Retry != 900 || s.Expire != 604800 || s.Minttl != 300 {
		t.Errorf("SOA numeric fields changed")
	}
	// Original not mutated.
	if orig.Header().Name != "root.com." {
		t.Errorf("original SOA mutated")
	}
}

// ---- RewriteRR with rewriteRDATALabels=true ----
//
// These tests pin the contract that the flag dispatches RDATA name fields
// through RewriteNameAnywhere (label-anywhere) rather than RewriteName
// (in-bailiwick suffix-only). Any future regression in the switch in
// RewriteRR would surface here at the unit-test boundary.

func TestRewriteRR_CNAME_FlagTrue_MidLabelRewritten(t *testing.T) {
	orig := newCNAME("host.root.com.", "host.root.com.cdn.example.net.")
	got := RewriteRR(orig, root, backup, true)

	c := got.(*dns.CNAME)
	if c.Hdr.Name != "host.backup.com." {
		t.Errorf("CNAME owner: got %q, want host.backup.com.", c.Hdr.Name)
	}
	if c.Target != "host.backup.com.cdn.example.net." {
		t.Errorf("CNAME target: got %q, want host.backup.com.cdn.example.net.", c.Target)
	}
}

func TestRewriteRR_NS_FlagTrue_MidLabelRewritten(t *testing.T) {
	orig := newNS("root.com.", "ns1.root.com.cdn.example.net.")
	got := RewriteRR(orig, root, backup, true)

	n := got.(*dns.NS)
	if n.Ns != "ns1.backup.com.cdn.example.net." {
		t.Errorf("NS Ns: got %q, want ns1.backup.com.cdn.example.net.", n.Ns)
	}
}

func TestRewriteRR_MX_FlagTrue_MidLabelRewritten(t *testing.T) {
	orig := newMX("root.com.", 10, "mail.root.com.relay.example.net.")
	got := RewriteRR(orig, root, backup, true)

	m := got.(*dns.MX)
	if m.Mx != "mail.backup.com.relay.example.net." {
		t.Errorf("MX Mx: got %q, want mail.backup.com.relay.example.net.", m.Mx)
	}
}

func TestRewriteRR_SRV_FlagTrue_MidLabelRewritten(t *testing.T) {
	orig := newSRV("_http._tcp.root.com.", 10, 20, 80, "app.root.com.cdn.example.net.")
	got := RewriteRR(orig, root, backup, true)

	s := got.(*dns.SRV)
	if s.Target != "app.backup.com.cdn.example.net." {
		t.Errorf("SRV target: got %q, want app.backup.com.cdn.example.net.", s.Target)
	}
}

func TestRewriteRR_PTR_FlagTrue_MidLabelRewritten(t *testing.T) {
	orig := newPTR("4.3.2.1.in-addr.arpa.", "host.root.com.cdn.example.net.")
	got := RewriteRR(orig, root, backup, true)

	p := got.(*dns.PTR)
	if p.Ptr != "host.backup.com.cdn.example.net." {
		t.Errorf("PTR Ptr: got %q, want host.backup.com.cdn.example.net.", p.Ptr)
	}
}

func TestRewriteRR_SOA_FlagTrue_MidLabelRewritten(t *testing.T) {
	orig := newSOA("root.com.", "ns1.root.com.zone.example.net.", "admin.root.com.zone.example.net.",
		2024010101, 3600, 900, 604800, 300)
	got := RewriteRR(orig, root, backup, true)

	s := got.(*dns.SOA)
	if s.Ns != "ns1.backup.com.zone.example.net." {
		t.Errorf("SOA MNAME: got %q, want ns1.backup.com.zone.example.net.", s.Ns)
	}
	if s.Mbox != "admin.backup.com.zone.example.net." {
		t.Errorf("SOA RNAME: got %q, want admin.backup.com.zone.example.net.", s.Mbox)
	}
	if s.Serial != 2024010101 {
		t.Errorf("SOA serial mutated: got %d", s.Serial)
	}
}

// Flag=true must NOT alter A/AAAA/TXT RDATA — those types are excluded from
// the rewrite switch entirely.
func TestRewriteRR_A_FlagTrue_RDATAUntouched(t *testing.T) {
	orig := newA("www.root.com.", "1.2.3.4")
	got := RewriteRR(orig, root, backup, true)

	a := got.(*dns.A)
	if a.A.String() != "1.2.3.4" {
		t.Errorf("A IP rewritten: got %q", a.A.String())
	}
}

func TestRewriteRR_TXT_FlagTrue_RDATAUntouched(t *testing.T) {
	// A TXT string that contains a label-shaped substring matching root MUST
	// still be preserved verbatim because TXT RDATA is opaque text.
	orig := newTXT("root.com.", "v=spf1 include:root.com. ~all")
	got := RewriteRR(orig, root, backup, true)

	txt := got.(*dns.TXT)
	if txt.Txt[0] != "v=spf1 include:root.com. ~all" {
		t.Errorf("TXT mutated: got %q", txt.Txt[0])
	}
}

// ---- RewriteRR expanded RDATA types ----
//
// Each newly covered type is exercised under both rewrite_rdata_labels
// values, for in-bailiwick values (rewritten), out-of-bailiwick values
// (preserved byte-for-byte), and mid-label root sequences (rewritten only
// when the flag is true).

// expandedTypeCases enumerates the newly covered RDATA-name-bearing types.
// build constructs a record whose name-bearing RDATA fields are set from
// names (in declaration order); rdata extracts those fields for assertion.
var expandedTypeCases = []struct {
	typ    string
	fields int
	build  func(owner string, names []string) dns.RR
	rdata  func(rr dns.RR) []string
}{
	{
		typ: "HTTPS", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.HTTPS{SVCB: dns.SVCB{
				Hdr:      dns.RR_Header{Name: owner, Rrtype: dns.TypeHTTPS, Class: dns.ClassINET, Ttl: 300},
				Priority: 1,
				Target:   n[0],
			}}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.HTTPS).Target} },
	},
	{
		typ: "SVCB", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.SVCB{
				Hdr:      dns.RR_Header{Name: owner, Rrtype: dns.TypeSVCB, Class: dns.ClassINET, Ttl: 300},
				Priority: 1,
				Target:   n[0],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.SVCB).Target} },
	},
	{
		typ: "DNAME", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.DNAME{
				Hdr:    dns.RR_Header{Name: owner, Rrtype: dns.TypeDNAME, Class: dns.ClassINET, Ttl: 300},
				Target: n[0],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.DNAME).Target} },
	},
	{
		typ: "NAPTR", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.NAPTR{
				Hdr:         dns.RR_Header{Name: owner, Rrtype: dns.TypeNAPTR, Class: dns.ClassINET, Ttl: 300},
				Order:       100,
				Preference:  10,
				Flags:       "s",
				Service:     "SIP+D2T",
				Regexp:      "",
				Replacement: n[0],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.NAPTR).Replacement} },
	},
	{
		typ: "RP", fields: 2,
		build: func(owner string, n []string) dns.RR {
			return &dns.RP{
				Hdr:  dns.RR_Header{Name: owner, Rrtype: dns.TypeRP, Class: dns.ClassINET, Ttl: 300},
				Mbox: n[0],
				Txt:  n[1],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.RP).Mbox, rr.(*dns.RP).Txt} },
	},
	{
		typ: "KX", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.KX{
				Hdr:        dns.RR_Header{Name: owner, Rrtype: dns.TypeKX, Class: dns.ClassINET, Ttl: 300},
				Preference: 10,
				Exchanger:  n[0],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.KX).Exchanger} },
	},
	{
		typ: "AFSDB", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.AFSDB{
				Hdr:      dns.RR_Header{Name: owner, Rrtype: dns.TypeAFSDB, Class: dns.ClassINET, Ttl: 300},
				Subtype:  1,
				Hostname: n[0],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.AFSDB).Hostname} },
	},
	{
		typ: "PX", fields: 2,
		build: func(owner string, n []string) dns.RR {
			return &dns.PX{
				Hdr:        dns.RR_Header{Name: owner, Rrtype: dns.TypePX, Class: dns.ClassINET, Ttl: 300},
				Preference: 10,
				Map822:     n[0],
				Mapx400:    n[1],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.PX).Map822, rr.(*dns.PX).Mapx400} },
	},
	{
		typ: "RT", fields: 1,
		build: func(owner string, n []string) dns.RR {
			return &dns.RT{
				Hdr:        dns.RR_Header{Name: owner, Rrtype: dns.TypeRT, Class: dns.ClassINET, Ttl: 300},
				Preference: 10,
				Host:       n[0],
			}
		},
		rdata: func(rr dns.RR) []string { return []string{rr.(*dns.RT).Host} },
	},
}

func TestRewriteRR_ExpandedTypes(t *testing.T) {
	scenarios := []struct {
		name string
		flag bool
		in   func(i int) string
		want func(i int) string
	}{
		{
			name: "in-bailiwick rewritten (flag=false)",
			flag: false,
			in:   func(i int) string { return fmt.Sprintf("n%d.root.com.", i) },
			want: func(i int) string { return fmt.Sprintf("n%d.backup.com.", i) },
		},
		{
			name: "in-bailiwick rewritten (flag=true)",
			flag: true,
			in:   func(i int) string { return fmt.Sprintf("n%d.root.com.", i) },
			want: func(i int) string { return fmt.Sprintf("n%d.backup.com.", i) },
		},
		{
			name: "out-of-bailiwick preserved (flag=false)",
			flag: false,
			in:   func(i int) string { return fmt.Sprintf("n%d.external.example.net.", i) },
			want: func(i int) string { return fmt.Sprintf("n%d.external.example.net.", i) },
		},
		{
			name: "out-of-bailiwick preserved (flag=true)",
			flag: true,
			in:   func(i int) string { return fmt.Sprintf("n%d.external.example.net.", i) },
			want: func(i int) string { return fmt.Sprintf("n%d.external.example.net.", i) },
		},
		{
			name: "mid-label rewritten (flag=true)",
			flag: true,
			in:   func(i int) string { return fmt.Sprintf("n%d.root.com.cdn.example.net.", i) },
			want: func(i int) string { return fmt.Sprintf("n%d.backup.com.cdn.example.net.", i) },
		},
		{
			name: "mid-label preserved (flag=false)",
			flag: false,
			in:   func(i int) string { return fmt.Sprintf("n%d.root.com.cdn.example.net.", i) },
			want: func(i int) string { return fmt.Sprintf("n%d.root.com.cdn.example.net.", i) },
		},
	}

	for _, tc := range expandedTypeCases {
		for _, sc := range scenarios {
			t.Run(tc.typ+"/"+sc.name, func(t *testing.T) {
				in := make([]string, tc.fields)
				want := make([]string, tc.fields)
				for i := range tc.fields {
					in[i] = sc.in(i)
					want[i] = sc.want(i)
				}
				orig := tc.build("svc.root.com.", in)
				got := RewriteRR(orig, root, backup, sc.flag)

				if got.Header().Name != "svc.backup.com." {
					t.Errorf("%s owner: got %q, want svc.backup.com.", tc.typ, got.Header().Name)
				}
				for i, g := range tc.rdata(got) {
					if g != want[i] {
						t.Errorf("%s RDATA field %d: got %q, want %q", tc.typ, i, g, want[i])
					}
				}
				// Original must not be mutated.
				for i, o := range tc.rdata(orig) {
					if o != in[i] {
						t.Errorf("%s original RDATA field %d mutated: %q", tc.typ, i, o)
					}
				}
			})
		}
	}
}

// Auxiliary (non-name) RDATA fields of the newly covered types must be
// preserved byte-for-byte, per the spec's NAPTR scenario.
func TestRewriteRR_NAPTR_AuxFieldsPreserved(t *testing.T) {
	orig := &dns.NAPTR{
		Hdr:         dns.RR_Header{Name: "root.com.", Rrtype: dns.TypeNAPTR, Class: dns.ClassINET, Ttl: 300},
		Order:       100,
		Preference:  10,
		Flags:       "s",
		Service:     "SIP+D2T",
		Regexp:      "",
		Replacement: "svc.root.com.",
	}
	got := RewriteRR(orig, root, backup, false).(*dns.NAPTR)

	if got.Replacement != "svc.backup.com." {
		t.Errorf("NAPTR Replacement: got %q, want svc.backup.com.", got.Replacement)
	}
	if got.Order != 100 || got.Preference != 10 || got.Flags != "s" || got.Service != "SIP+D2T" || got.Regexp != "" {
		t.Errorf("NAPTR aux fields changed: order=%d pref=%d flags=%q service=%q regexp=%q",
			got.Order, got.Preference, got.Flags, got.Service, got.Regexp)
	}
}

func TestRewriteRR_HTTPS_SvcParamsPreserved(t *testing.T) {
	orig := &dns.HTTPS{SVCB: dns.SVCB{
		Hdr:      dns.RR_Header{Name: "www.root.com.", Rrtype: dns.TypeHTTPS, Class: dns.ClassINET, Ttl: 300},
		Priority: 1,
		Target:   "svc.root.com.",
		Value:    []dns.SVCBKeyValue{&dns.SVCBAlpn{Alpn: []string{"h2"}}},
	}}
	got := RewriteRR(orig, root, backup, false).(*dns.HTTPS)

	if got.Target != "svc.backup.com." {
		t.Errorf("HTTPS Target: got %q, want svc.backup.com.", got.Target)
	}
	if got.Priority != 1 {
		t.Errorf("HTTPS Priority changed: got %d", got.Priority)
	}
	if len(got.Value) != 1 {
		t.Fatalf("HTTPS SvcParams changed: got %d params", len(got.Value))
	}
	alpn, ok := got.Value[0].(*dns.SVCBAlpn)
	if !ok || len(alpn.Alpn) != 1 || alpn.Alpn[0] != "h2" {
		t.Errorf("HTTPS alpn param changed: %v", got.Value[0])
	}
}

// ---- Legacy-type regression guard ----
//
// The six originally covered types plus A/AAAA/TXT must produce output
// identical to the pre-expansion behavior under both flag values. Pinned
// via full RR String() comparison so any drift in owner, RDATA, or aux
// fields surfaces here.
func TestRewriteRR_LegacyTypesUnchanged(t *testing.T) {
	build := []struct {
		name string
		orig dns.RR
		want string // expected String() of the rewritten record (same under both flags)
	}{
		{"CNAME in-bailiwick", newCNAME("alias.root.com.", "canonical.root.com."),
			"alias.backup.com.\t300\tIN\tCNAME\tcanonical.backup.com."},
		{"NS in-bailiwick", newNS("root.com.", "ns1.root.com."),
			"backup.com.\t300\tIN\tNS\tns1.backup.com."},
		{"MX in-bailiwick", newMX("root.com.", 10, "mail.root.com."),
			"backup.com.\t300\tIN\tMX\t10 mail.backup.com."},
		{"PTR in-bailiwick", newPTR("4.2.0.192.in-addr.arpa.", "www.root.com."),
			"4.2.0.192.in-addr.arpa.\t300\tIN\tPTR\twww.backup.com."},
		{"SRV in-bailiwick", newSRV("_http._tcp.root.com.", 10, 20, 80, "app.root.com."),
			"_http._tcp.backup.com.\t300\tIN\tSRV\t10 20 80 app.backup.com."},
		{"SOA in-bailiwick", newSOA("root.com.", "ns1.root.com.", "admin.root.com.", 2024010101, 3600, 900, 604800, 300),
			"backup.com.\t300\tIN\tSOA\tns1.backup.com. admin.backup.com. 2024010101 3600 900 604800 300"},
		{"A untouched RDATA", newA("www.root.com.", "192.0.2.4"),
			"www.backup.com.\t300\tIN\tA\t192.0.2.4"},
		{"AAAA untouched RDATA", newAAAA("v6.root.com.", "2001:db8::1"),
			"v6.backup.com.\t300\tIN\tAAAA\t2001:db8::1"},
		{"TXT untouched RDATA", newTXT("root.com.", "v=spf1 include:root.com. ~all"),
			"backup.com.\t300\tIN\tTXT\t\"v=spf1 include:root.com. ~all\""},
	}

	for _, tc := range build {
		for _, flag := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/flag=%v", tc.name, flag), func(t *testing.T) {
				got := RewriteRR(tc.orig, root, backup, flag)
				if got.String() != tc.want {
					t.Errorf("got  %q\nwant %q", got.String(), tc.want)
				}
			})
		}
	}
}

func TestRewriteRR_OriginalNotMutated(t *testing.T) {
	orig := newCNAME("www.root.com.", "canonical.root.com.")
	_ = RewriteRR(orig, root, backup, false)

	if orig.Header().Name != "www.root.com." {
		t.Errorf("original CNAME owner mutated: %q", orig.Header().Name)
	}
	if orig.Target != "canonical.root.com." {
		t.Errorf("original CNAME target mutated: %q", orig.Target)
	}
}
