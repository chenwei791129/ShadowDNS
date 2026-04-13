package alias

import (
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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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
	got := RewriteRR(orig, root, backup)

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

func TestRewriteRR_OriginalNotMutated(t *testing.T) {
	orig := newCNAME("www.root.com.", "canonical.root.com.")
	_ = RewriteRR(orig, root, backup)

	if orig.Header().Name != "www.root.com." {
		t.Errorf("original CNAME owner mutated: %q", orig.Header().Name)
	}
	if orig.Target != "canonical.root.com." {
		t.Errorf("original CNAME target mutated: %q", orig.Target)
	}
}
