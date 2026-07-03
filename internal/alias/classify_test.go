package alias

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// coveredTypeSamples returns one record of each covered rrtype whose
// name-bearing RDATA fields all point in-bailiwick (under root.com.). The
// owner is deliberately out-of-bailiwick so root.com. appears only in RDATA.
// Used by the Decision 2 invariant tests; a covered rrtype with no sample
// here fails the round-trip test loudly instead of being skipped.
func coveredTypeSamples() map[uint16]dns.RR {
	const owner = "owner.test."
	hdr := func(rrtype uint16) dns.RR_Header {
		return dns.RR_Header{Name: owner, Rrtype: rrtype, Class: dns.ClassINET, Ttl: 300}
	}
	return map[uint16]dns.RR{
		dns.TypeCNAME: &dns.CNAME{Hdr: hdr(dns.TypeCNAME), Target: "t.root.com."},
		dns.TypeNS:    &dns.NS{Hdr: hdr(dns.TypeNS), Ns: "ns1.root.com."},
		dns.TypeMX:    &dns.MX{Hdr: hdr(dns.TypeMX), Preference: 10, Mx: "mail.root.com."},
		dns.TypePTR:   &dns.PTR{Hdr: hdr(dns.TypePTR), Ptr: "host.root.com."},
		dns.TypeSRV:   &dns.SRV{Hdr: hdr(dns.TypeSRV), Priority: 1, Weight: 2, Port: 80, Target: "app.root.com."},
		dns.TypeSOA:   &dns.SOA{Hdr: hdr(dns.TypeSOA), Ns: "ns1.root.com.", Mbox: "admin.root.com.", Serial: 1, Refresh: 2, Retry: 3, Expire: 4, Minttl: 5},
		dns.TypeHTTPS: &dns.HTTPS{SVCB: dns.SVCB{Hdr: hdr(dns.TypeHTTPS), Priority: 1, Target: "svc.root.com."}},
		dns.TypeSVCB:  &dns.SVCB{Hdr: hdr(dns.TypeSVCB), Priority: 1, Target: "svc.root.com."},
		dns.TypeDNAME: &dns.DNAME{Hdr: hdr(dns.TypeDNAME), Target: "sub.root.com."},
		dns.TypeNAPTR: &dns.NAPTR{Hdr: hdr(dns.TypeNAPTR), Order: 100, Preference: 10, Flags: "s", Service: "SIP+D2T", Replacement: "svc.root.com."},
		dns.TypeRP:    &dns.RP{Hdr: hdr(dns.TypeRP), Mbox: "admin.root.com.", Txt: "info.root.com."},
		dns.TypeKX:    &dns.KX{Hdr: hdr(dns.TypeKX), Preference: 10, Exchanger: "kx.root.com."},
		dns.TypeAFSDB: &dns.AFSDB{Hdr: hdr(dns.TypeAFSDB), Subtype: 1, Hostname: "afs.root.com."},
		dns.TypePX:    &dns.PX{Hdr: hdr(dns.TypePX), Preference: 10, Map822: "px.root.com.", Mapx400: "x400.root.com."},
		dns.TypeRT:    &dns.RT{Hdr: hdr(dns.TypeRT), Preference: 10, Host: "rt.root.com."},
	}
}

func TestClassifyRR(t *testing.T) {
	tests := []struct {
		name string
		rr   dns.RR
		want rrClass
	}{
		{"CNAME covered", &dns.CNAME{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeCNAME}, Target: "b.root.com."}, classCovered},
		{"HTTPS covered", &dns.HTTPS{SVCB: dns.SVCB{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeHTTPS}, Target: "b.root.com."}}, classCovered},
		{"A noName", newA("a.root.com.", "192.0.2.1"), classNoName},
		{"AAAA noName", newAAAA("a.root.com.", "2001:db8::1"), classNoName},
		{"TXT noName", newTXT("a.root.com.", "text"), classNoName},
		{"RFC3597 unknown type noName", &dns.RFC3597{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: 65280}, Rdata: "abcd"}, classNoName},
		{"RRSIG uncovered name-bearing", &dns.RRSIG{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeRRSIG}, SignerName: "root.com."}, classUncoveredName},
		{"NSEC uncovered name-bearing", &dns.NSEC{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeNSEC}, NextDomain: "b.root.com."}, classUncoveredName},
		{"MINFO uncovered name-bearing", &dns.MINFO{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeMINFO}, Rmail: "a.root.com.", Email: "b.root.com."}, classUncoveredName},
		{"MB uncovered name-bearing", &dns.MB{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeMB}, Mb: "host.root.com."}, classUncoveredName},
		// IPSECKEY/AMTRELAY carry a domain-name gateway under dedicated
		// wire-format tags (ipsechost/amtrelayhost), not domain-name — they
		// must still classify as name-bearing (fail closed).
		{"IPSECKEY uncovered name-bearing", &dns.IPSECKEY{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeIPSECKEY}, GatewayType: 3, GatewayHost: "gw.root.com."}, classUncoveredName},
		{"AMTRELAY uncovered name-bearing", &dns.AMTRELAY{Hdr: dns.RR_Header{Name: "a.root.com.", Rrtype: dns.TypeAMTRELAY}, GatewayType: 3, GatewayHost: "relay.root.com."}, classUncoveredName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRR(tc.rr); got != tc.want {
				t.Errorf("classifyRR(%T) = %v, want %v", tc.rr, got, tc.want)
			}
		})
	}
}

// Decision 2 invariant (a): covered ⊆ name-bearing. Every covered rrtype must
// be classified as having a name-bearing RDATA field — explicitly including
// HTTPS, whose domain-name tag lives on the embedded SVCB struct and would be
// missed by a non-recursive reflection walk.
func TestCoveredTypesAreNameBearing(t *testing.T) {
	samples := coveredTypeSamples()
	for rrtype := range coveredRRTypes {
		rr, ok := samples[rrtype]
		if !ok {
			t.Errorf("coveredRRTypes contains %s but coveredTypeSamples has no sample; add one",
				dns.TypeToString[rrtype])
			continue
		}
		if !hasNameBearingRDATA(rr) {
			t.Errorf("covered type %s not classified name-bearing; the reflection walk is under-protecting it",
				dns.TypeToString[rrtype])
		}
	}
}

// Decision 2 invariant (b): covered == switch round-trip. Every rrtype in
// coveredRRTypes must actually be rewritten by rewriteRDATANames; a type in
// the set that the switch does not handle would silently withhold a
// rewritable record (data loss), and a type in the switch but not the set is
// caught by the sample-completeness check plus this rewrite assertion.
func TestCoveredSetMatchesRewriteSwitch(t *testing.T) {
	samples := coveredTypeSamples()
	if len(samples) != len(coveredRRTypes) {
		t.Errorf("coveredTypeSamples has %d entries, coveredRRTypes has %d; keep them in sync",
			len(samples), len(coveredRRTypes))
	}
	for rrtype, rr := range samples {
		if !coveredRRTypes[rrtype] {
			t.Errorf("sample type %s missing from coveredRRTypes", dns.TypeToString[rrtype])
			continue
		}
		cp := dns.Copy(rr)
		rewriteRDATANames(cp, "root.com.", "backup.com.", false)
		if strings.Contains(cp.String(), "root.com.") {
			t.Errorf("%s: rewriteRDATANames left root.com. in RDATA (switch does not handle a covered type): %s",
				dns.TypeToString[rrtype], cp.String())
		}
	}
}

func TestFilterBackupRRs(t *testing.T) {
	a := newA("www.root.com.", "192.0.2.1")
	cname := newCNAME("www.root.com.", "svc.root.com.")
	rrsig := &dns.RRSIG{Hdr: dns.RR_Header{Name: "www.root.com.", Rrtype: dns.TypeRRSIG, Class: dns.ClassINET, Ttl: 300}, SignerName: "root.com."}
	nsec := &dns.NSEC{Hdr: dns.RR_Header{Name: "www.root.com.", Rrtype: dns.TypeNSEC, Class: dns.ClassINET, Ttl: 300}, NextDomain: "zzz.root.com."}

	t.Run("nothing withheld passes through", func(t *testing.T) {
		in := []dns.RR{a, cname}
		kept, withheld := FilterBackupRRs(in)
		if len(kept) != 2 || len(withheld) != 0 {
			t.Fatalf("kept=%d withheld=%d, want 2/0", len(kept), len(withheld))
		}
	})

	t.Run("uncovered withheld, covered and noName kept", func(t *testing.T) {
		kept, withheld := FilterBackupRRs([]dns.RR{a, rrsig, cname})
		if len(kept) != 2 {
			t.Fatalf("kept=%d, want 2", len(kept))
		}
		if kept[0] != dns.RR(a) || kept[1] != dns.RR(cname) {
			t.Errorf("kept order/content wrong: %v", kept)
		}
		if len(withheld) != 1 {
			t.Fatalf("withheld=%d, want 1", len(withheld))
		}
		if withheld[0].Rrtype != dns.TypeRRSIG || withheld[0].Owner != "www.root.com." {
			t.Errorf("withheld = %+v, want RRSIG www.root.com.", withheld[0])
		}
	})

	t.Run("all withheld yields empty kept", func(t *testing.T) {
		kept, withheld := FilterBackupRRs([]dns.RR{rrsig, nsec})
		if len(kept) != 0 {
			t.Fatalf("kept=%d, want 0", len(kept))
		}
		if len(withheld) != 2 {
			t.Fatalf("withheld=%d, want 2", len(withheld))
		}
		if withheld[1].Rrtype != dns.TypeNSEC {
			t.Errorf("withheld[1].Rrtype = %s, want NSEC", dns.TypeToString[withheld[1].Rrtype])
		}
	})

	t.Run("empty input", func(t *testing.T) {
		kept, withheld := FilterBackupRRs(nil)
		if len(kept) != 0 || len(withheld) != 0 {
			t.Fatalf("kept=%d withheld=%d, want 0/0", len(kept), len(withheld))
		}
	})
}
