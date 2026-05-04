package prunebackup

import (
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// TestOverridableTypesMirrorDnsutil pins the local overridableTypes map to
// dnsutil.OverridableTypes. If the runtime overridable set widens (e.g.
// CNAME becomes overridable) without updating the prune rules, this test
// fails loudly — catching the silent-wrongness risk the comment in diff.go
// warns about.
func TestOverridableTypesMirrorDnsutil(t *testing.T) {
	for typ := range dnsutil.OverridableTypes {
		if !overridableTypes[typ] {
			t.Errorf("dnsutil.OverridableTypes has %s, prunebackup.overridableTypes missing it",
				dns.TypeToString[typ])
		}
	}
	for typ := range overridableTypes {
		if !dnsutil.OverridableTypes[typ] {
			t.Errorf("prunebackup.overridableTypes has %s, dnsutil.OverridableTypes missing it",
				dns.TypeToString[typ])
		}
	}
}

// mustRR is a concise test-only shorthand for dns.NewRR with fail-fast.
func mustRR(t *testing.T, s string) dns.RR {
	t.Helper()
	rr, err := dns.NewRR(s)
	if err != nil {
		t.Fatalf("dns.NewRR(%q): %v", s, err)
	}
	return rr
}

func TestRRSetEqual(t *testing.T) {
	a := []dns.RR{
		mustRR(t, "mail.backup.example. 300 IN MX 10 a.example.net."),
		mustRR(t, "mail.backup.example. 300 IN MX 20 b.example.net."),
	}
	// Same records, different TTL, reversed order — semantically equal.
	b := []dns.RR{
		mustRR(t, "mail.example.com. 900 IN MX 20 b.example.net."),
		mustRR(t, "mail.example.com. 900 IN MX 10 a.example.net."),
	}
	// For rrsetEqual, owner is stripped from comparison (only rdata matters).
	if !rrsetEqual(a, b) {
		t.Errorf("rrsetEqual returned false for TTL/order-varying equal sets")
	}

	// Differ by one record → not equal.
	c := []dns.RR{
		mustRR(t, "mail.backup.example. 300 IN MX 10 a.example.net."),
	}
	if rrsetEqual(a, c) {
		t.Errorf("rrsetEqual returned true for subset")
	}
}

func TestClassify_SpecTable(t *testing.T) {
	origin := "backup.example."

	type tc struct {
		name   string
		owner  string
		rtype  uint16
		backup []dns.RR
		root   []dns.RR
		want   decision
	}
	cases := []tc{
		{
			name:  "identical MX sets -> delete",
			owner: "mail.backup.example.", rtype: dns.TypeMX,
			backup: []dns.RR{
				mustRR(t, "mail.backup.example. 300 IN MX 10 a.example.net."),
				mustRR(t, "mail.backup.example. 300 IN MX 20 b.example.net."),
				mustRR(t, "mail.backup.example. 300 IN MX 30 c.example.net."),
			},
			root: []dns.RR{
				mustRR(t, "mail.example.com. 600 IN MX 10 a.example.net."),
				mustRR(t, "mail.example.com. 600 IN MX 20 b.example.net."),
				mustRR(t, "mail.example.com. 600 IN MX 30 c.example.net."),
			},
			want: decisionDelete,
		},
		{
			name:  "backup subset of root -> retain",
			owner: "mail.backup.example.", rtype: dns.TypeMX,
			backup: []dns.RR{
				mustRR(t, "mail.backup.example. 300 IN MX 10 a.example.net."),
				mustRR(t, "mail.backup.example. 300 IN MX 20 b.example.net."),
			},
			root: []dns.RR{
				mustRR(t, "mail.example.com. 600 IN MX 10 a.example.net."),
				mustRR(t, "mail.example.com. 600 IN MX 20 b.example.net."),
				mustRR(t, "mail.example.com. 600 IN MX 30 c.example.net."),
			},
			want: decisionRetain,
		},
		{
			name:  "matching two-element MX sets -> delete",
			owner: "mail.backup.example.", rtype: dns.TypeMX,
			backup: []dns.RR{
				mustRR(t, "mail.backup.example. 300 IN MX 10 a.example.net."),
				mustRR(t, "mail.backup.example. 300 IN MX 20 b.example.net."),
			},
			root: []dns.RR{
				mustRR(t, "mail.example.com. 600 IN MX 10 a.example.net."),
				mustRR(t, "mail.example.com. 600 IN MX 20 b.example.net."),
			},
			want: decisionDelete,
		},
		{
			name:  "rdata differs -> retain",
			owner: "mail.backup.example.", rtype: dns.TypeMX,
			backup: []dns.RR{
				mustRR(t, "mail.backup.example. 300 IN MX 10 a.example.net."),
			},
			root: []dns.RR{
				mustRR(t, "mail.example.com. 600 IN MX 10 z.example.net."),
			},
			want: decisionRetain,
		},
		{
			name:  "TXT present in backup only -> retain",
			owner: "@.backup.example.", rtype: dns.TypeTXT,
			backup: []dns.RR{
				mustRR(t, "backup.example. 300 IN TXT \"v=spf1 a -all\""),
			},
			root: nil,
			want: decisionRetain,
		},
		{
			name:  "A record anywhere -> delete (not overridable)",
			owner: "www.backup.example.", rtype: dns.TypeA,
			backup: []dns.RR{
				mustRR(t, "www.backup.example. 300 IN A 192.0.2.10"),
			},
			root: []dns.RR{
				mustRR(t, "www.example.com. 600 IN A 192.0.2.10"),
			},
			want: decisionDelete,
		},
		{
			name:  "SOA always retained",
			owner: "backup.example.", rtype: dns.TypeSOA,
			backup: []dns.RR{
				mustRR(t, "backup.example. 300 IN SOA ns1. hostmaster. 1 300 120 604800 300"),
			},
			root: nil,
			want: decisionRetain,
		},
		{
			name:  "apex NS retained",
			owner: "backup.example.", rtype: dns.TypeNS,
			backup: []dns.RR{
				mustRR(t, "backup.example. 300 IN NS ns1.backup.example."),
			},
			root: nil,
			want: decisionRetain,
		},
		{
			name:  "sub-delegation NS deleted",
			owner: "child.backup.example.", rtype: dns.TypeNS,
			backup: []dns.RR{
				mustRR(t, "child.backup.example. 300 IN NS ns.other."),
			},
			root: nil,
			want: decisionDelete,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classify(c.backup, c.root, c.owner, c.rtype, origin)
			if got != c.want {
				t.Errorf("classify got %v, want %v", got, c.want)
			}
		})
	}
}

func TestBuildRRSetIndex_CanonicalizesOwner(t *testing.T) {
	rrs := []dns.RR{
		mustRR(t, "WWW.Backup.Example. 300 IN TXT \"token\""),
		mustRR(t, "www.backup.example. 300 IN TXT \"token\""),
	}
	idx := buildRRSetIndex(rrs)
	key := rrsetKey{Owner: "www.backup.example.", Rtype: dns.TypeTXT}
	set, ok := idx[key]
	if !ok {
		t.Fatalf("expected index entry for %v, got %#v", key, idx)
	}
	if len(set) != 2 {
		t.Errorf("expected 2 RRs under canonical owner, got %d", len(set))
	}
	// No other owner keys.
	for k := range idx {
		if !strings.EqualFold(k.Owner, "www.backup.example.") {
			t.Errorf("unexpected index key %v", k)
		}
	}
}
