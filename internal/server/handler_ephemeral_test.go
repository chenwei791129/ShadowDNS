package server

import (
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// newRootOnlyServerWithEphemeral starts a server with one root zone under the
// "default" view and attaches the given ephemeral store.
func newRootOnlyServerWithEphemeral(t *testing.T, rootZ *zone.Zone, store *ephemeral.Store) (string, func()) {
	t.Helper()
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {rootZ.Origin}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {rootZ.Origin: rootZ}},
	}, nil)
	srv.EphemeralStore = store
	udpAddr, _, cancel := startTestServer(t, srv)
	return udpAddr, cancel
}

// newRootBackupServerWithEphemeral attaches an ephemeral store to a server
// that has both a root and a backup-override zone.
func newRootBackupServerWithEphemeral(t *testing.T, rootZ, backupZ *zone.Zone, store *ephemeral.Store) (string, func()) {
	t.Helper()
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {rootZ.Origin, backupZ.Origin}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {rootZ.Origin: rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {backupZ.Origin: backupZ}},
		Aliases:     config.AliasMap{backupZ.Origin: rootZ.Origin},
	}, nil)
	srv.EphemeralStore = store
	udpAddr, _, cancel := startTestServer(t, srv)
	return udpAddr, cancel
}

// TestEphemeral_TXTReturnedWhenZoneHasNoMatch verifies that an ephemeral TXT
// record is served for a name that is in-zone but absent from the zone file.
func TestEphemeral_TXTReturnedWhenZoneHasNoMatch(t *testing.T) {
	rootZ := buildRootZone("example.com.")
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.example.com.", "token-abc", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if !resp.Authoritative {
		t.Error("expected AA=1 on ephemeral TXT response")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1; Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "token-abc" {
		t.Errorf("TXT value = %v, want [token-abc]", txt.Txt)
	}
	// TTL should be remaining seconds (≤ original TTL, > 0).
	if txt.Hdr.Ttl == 0 || txt.Hdr.Ttl > 120 {
		t.Errorf("TTL = %d, want remaining seconds in (0, 120]", txt.Hdr.Ttl)
	}
}

// TestEphemeral_ZoneFileTakesPrecedence verifies that when a zone file has a
// TXT record at qname, the ephemeral store is NOT consulted (zone wins).
func TestEphemeral_ZoneFileTakesPrecedence(t *testing.T) {
	zoneTXT := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   "_acme-challenge.example.com.",
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Txt: []string{"zone-value"},
	}
	rootZ := buildRootZone("example.com.", zoneTXT)
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.example.com.", "ephemeral-value", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.example.com.", dns.TypeTXT)

	if len(resp.Answer) == 0 {
		t.Fatal("expected at least one answer")
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT", resp.Answer[0])
	}
	if txt.Txt[0] != "zone-value" {
		t.Errorf("TXT value = %q, want zone-value (zone file must take precedence)", txt.Txt[0])
	}
}

// TestEphemeral_ExpiredRecordNotReturned verifies that an expired ephemeral
// entry yields a negative reply rather than being served.
func TestEphemeral_ExpiredRecordNotReturned(t *testing.T) {
	rootZ := buildRootZone("example.com.")
	store := ephemeral.NewStore()
	// TTL 1 second; after Put we can't easily rewind the clock via the public
	// API (Store clock injection is internal). Simulate expiration by
	// inserting with TTL 0 — lookup should treat it as already-expired since
	// expireAt = now, and Lookup returns empty for remaining <= 0.
	store.Put("_acme-challenge.example.com.", "stale", 0)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Rcode = %d, want NXDOMAIN (expired ephemeral → negative reply)", resp.Rcode)
	}
	if len(resp.Answer) != 0 {
		t.Errorf("Answer = %v, want empty", resp.Answer)
	}
}

// TestEphemeral_NonTXTTypeNotMatched verifies that queries for non-TXT types
// never consult the ephemeral store, even when a same-name TXT entry exists.
func TestEphemeral_NonTXTTypeNotMatched(t *testing.T) {
	rootZ := buildRootZone("example.com.")
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.example.com.", "token", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Rcode = %d, want NXDOMAIN (A query must not hit ephemeral store)", resp.Rcode)
	}
	if len(resp.Answer) != 0 {
		t.Errorf("Answer = %v, want empty", resp.Answer)
	}
}

// TestEphemeral_MultipleTXTRRsReturned verifies that when the ephemeral
// store holds multiple values for the same FQDN (the ACME wildcard + apex
// parallel-validation case), every value is returned as its own TXT RR in
// the answer section.
func TestEphemeral_MultipleTXTRRsReturned(t *testing.T) {
	rootZ := buildRootZone("example.com.")
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.example.com.", "token-A", 120)
	store.Put("_acme-challenge.example.com.", "token-B", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if len(resp.Answer) != 2 {
		t.Fatalf("Answer len = %d, want 2", len(resp.Answer))
	}

	values := map[string]bool{}
	for _, rr := range resp.Answer {
		txt, ok := rr.(*dns.TXT)
		if !ok {
			t.Fatalf("Answer entry type = %T, want *dns.TXT", rr)
		}
		if len(txt.Txt) != 1 {
			t.Errorf("each ephemeral entry should yield its own RR with one string; got %d strings", len(txt.Txt))
		}
		values[txt.Txt[0]] = true
		if txt.Hdr.Name != "_acme-challenge.example.com." {
			t.Errorf("owner name = %q, want _acme-challenge.example.com.", txt.Hdr.Name)
		}
	}
	if !values["token-A"] || !values["token-B"] {
		t.Errorf("expected both token-A and token-B in answer; got %v", values)
	}
}

// TestEphemeral_DeleteDoesNotAffectZoneRecord covers the ephemeral-api spec
// scenario "Delete does not affect zone file records": deleting ephemeral
// entries for an FQDN must leave the zone file's TXT record still answerable.
func TestEphemeral_DeleteDoesNotAffectZoneRecord(t *testing.T) {
	zoneTXT := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   "_acme-challenge.example.com.",
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Txt: []string{"zone-static"},
	}
	rootZ := buildRootZone("example.com.", zoneTXT)
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.example.com.", "ephemeral-extra", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	store.Delete("_acme-challenge.example.com.")

	resp := query(t, "udp", addr, "_acme-challenge.example.com.", dns.TypeTXT)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR (zone record must still answer)", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
	if txt := resp.Answer[0].(*dns.TXT); txt.Txt[0] != "zone-static" {
		t.Errorf("TXT value = %q, want zone-static (zone must survive ephemeral DELETE)", txt.Txt[0])
	}
}

// TestEphemeral_BackupNamespaceTXTReturned verifies that ephemeral TXT
// records also work for names within a backup zone.
func TestEphemeral_BackupNamespaceTXTReturned(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	backupZ := buildBackupZone("backup.com.")
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.backup.com.", "backup-token", 60)

	addr, cancel := newRootBackupServerWithEphemeral(t, rootZ, backupZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.backup.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
	if txt := resp.Answer[0].(*dns.TXT); txt.Txt[0] != "backup-token" {
		t.Errorf("TXT value = %q, want backup-token", txt.Txt[0])
	}
}

// TestEphemeral_ExactBeatsWildcardTXT verifies that when the zone carries a
// wildcard TXT and the ephemeral store has an exact-qname TXT entry, the
// response contains only the ephemeral value (ephemeral exact match > zone
// wildcard).
func TestEphemeral_ExactBeatsWildcardTXT(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeTXTRecord("*.example.com.", "wild-value", 300),
	)
	store := ephemeral.NewStore()
	store.Put("foo.example.com.", "ephemeral-value", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "foo.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (ephemeral only, no wildcard); Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "ephemeral-value" {
		t.Errorf("TXT value = %v, want [ephemeral-value] (wildcard must not leak into answer)", txt.Txt)
	}
}

// TestEphemeral_ExactBeatsWildcardCNAME verifies that when the zone carries a
// wildcard CNAME and the ephemeral store has an exact-qname TXT entry, a TXT
// query returns the ephemeral value instead of the synthesized CNAME.
func TestEphemeral_ExactBeatsWildcardCNAME(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeCNAMERecord("*.example.com.", "target.other.com.", 300),
	)
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.example.com.", "token", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.foo.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (only ephemeral TXT); Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT (ephemeral must suppress wildcard CNAME synthesis)", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "token" {
		t.Errorf("TXT value = %v, want [token]", txt.Txt)
	}
}

// TestEphemeral_WildcardStillAppliesWithoutExactMatch verifies that when the
// ephemeral store has no exact entry, the zone's wildcard still answers
// correctly (regression guard for the reordering in 2.1).
func TestEphemeral_WildcardStillAppliesWithoutExactMatch(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeARecord("*.example.com.", "1.2.3.4", 300),
	)
	store := ephemeral.NewStore()

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "foo.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1; Answer=%v", len(resp.Answer), resp.Answer)
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.A", resp.Answer[0])
	}
	if got := a.A.String(); got != "1.2.3.4" {
		t.Errorf("A value = %s, want 1.2.3.4", got)
	}
	if a.Hdr.Name != "foo.example.com." {
		t.Errorf("owner name = %q, want foo.example.com. (synthesized)", a.Hdr.Name)
	}
}

// TestEphemeral_DoesNotSuppressWildcardForNonTXT verifies that an ephemeral
// TXT entry does not block wildcard synthesis for non-TXT query types at the
// same qname.
func TestEphemeral_DoesNotSuppressWildcardForNonTXT(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeARecord("*.example.com.", "1.2.3.4", 300),
	)
	store := ephemeral.NewStore()
	store.Put("foo.example.com.", "token", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "foo.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR (wildcard A must still answer)", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1; Answer=%v", len(resp.Answer), resp.Answer)
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.A", resp.Answer[0])
	}
	if got := a.A.String(); got != "1.2.3.4" {
		t.Errorf("A value = %s, want 1.2.3.4", got)
	}
}

// TestEphemeral_ExactBeatsWildcardTXTInBackupZone verifies that an ephemeral
// TXT entry at the exact backup-namespace qname takes precedence over a TXT
// wildcard synthesized from the root zone into the backup namespace.
func TestEphemeral_ExactBeatsWildcardTXTInBackupZone(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeTXTRecord("*.root.com.", "wild-value", 300),
	)
	backupZ := buildBackupZone("backup.com.")
	store := ephemeral.NewStore()
	store.Put("foo.backup.com.", "ephemeral-value", 60)

	addr, cancel := newRootBackupServerWithEphemeral(t, rootZ, backupZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "foo.backup.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (ephemeral only, no wildcard); Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "ephemeral-value" {
		t.Errorf("TXT value = %v, want [ephemeral-value] (wildcard must not leak into answer)", txt.Txt)
	}
}

// TestEphemeral_OverridesExactCNAME_RootZone verifies that an ephemeral TXT
// entry at a qname where the zone carries an exact (non-wildcard) CNAME
// overrides the CNAME for TXT queries. Covers the ACME DNS-01 delegation
// scenario where `_acme-challenge.<name>` is CNAME'd to an external acme-dns
// provider but a local ephemeral TXT has been written for the same name.
func TestEphemeral_OverridesExactCNAME_RootZone(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeCNAMERecord("_acme-challenge.foo.example.com.", "acme-dns.external.net.", 300),
	)
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.example.com.", "token-xyz", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.foo.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (ephemeral TXT only, no CNAME); Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT (ephemeral must override exact CNAME)", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "token-xyz" {
		t.Errorf("TXT value = %v, want [token-xyz]", txt.Txt)
	}
	for _, rr := range resp.Answer {
		if _, isCNAME := rr.(*dns.CNAME); isCNAME {
			t.Errorf("answer contains CNAME RR %v; CNAME must be suppressed when ephemeral TXT exists", rr)
		}
	}
}

// TestEphemeral_CNAMEQueryUnaffectedByEphemeralTXT verifies that an explicit
// CNAME query at a qname with both a zone CNAME and an ephemeral TXT returns
// only the zone CNAME. The ephemeral overlay is scoped to TXT qtype.
func TestEphemeral_CNAMEQueryUnaffectedByEphemeralTXT(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeCNAMERecord("_acme-challenge.foo.example.com.", "acme-dns.external.net.", 300),
	)
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.example.com.", "token-xyz", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.foo.example.com.", dns.TypeCNAME)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (zone CNAME only); Answer=%v", len(resp.Answer), resp.Answer)
	}
	cname, ok := resp.Answer[0].(*dns.CNAME)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.CNAME", resp.Answer[0])
	}
	if cname.Target != "acme-dns.external.net." {
		t.Errorf("CNAME target = %q, want acme-dns.external.net.", cname.Target)
	}
}

// TestEphemeral_TXTQueryFallsBackToCNAMEWhenStoreEmpty verifies that when the
// ephemeral store has no entry for a qname with a zone CNAME, standard RFC
// 1034 §3.6.2 CNAME synthesis is preserved (CNAME + in-zone target TXT).
func TestEphemeral_TXTQueryFallsBackToCNAMEWhenStoreEmpty(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeCNAMERecord("_acme-challenge.foo.example.com.", "target.example.com.", 300),
		makeTXTRecord("target.example.com.", "zone-txt", 300),
	)
	store := ephemeral.NewStore()

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.foo.example.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 2 {
		t.Fatalf("Answer len = %d, want 2 (CNAME + target TXT); Answer=%v", len(resp.Answer), resp.Answer)
	}
	cname, ok := resp.Answer[0].(*dns.CNAME)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.CNAME", resp.Answer[0])
	}
	if cname.Target != "target.example.com." {
		t.Errorf("CNAME target = %q, want target.example.com.", cname.Target)
	}
	txt, ok := resp.Answer[1].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[1] type = %T, want *dns.TXT", resp.Answer[1])
	}
	if txt.Hdr.Name != "target.example.com." {
		t.Errorf("TXT owner = %q, want target.example.com.", txt.Hdr.Name)
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "zone-txt" {
		t.Errorf("TXT value = %v, want [zone-txt]", txt.Txt)
	}
}

// TestEphemeral_NonTXTQueryAtCNAMEUnaffected verifies that an A query at a
// qname with both a zone CNAME and an ephemeral TXT entry is unaffected by
// the ephemeral store: standard CNAME synthesis runs and the CNAME plus
// target A are returned.
func TestEphemeral_NonTXTQueryAtCNAMEUnaffected(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeCNAMERecord("foo.example.com.", "target.example.com.", 300),
		makeARecord("target.example.com.", "1.2.3.4", 300),
	)
	store := ephemeral.NewStore()
	store.Put("foo.example.com.", "token", 120)

	addr, cancel := newRootOnlyServerWithEphemeral(t, rootZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "foo.example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 2 {
		t.Fatalf("Answer len = %d, want 2 (CNAME + A); Answer=%v", len(resp.Answer), resp.Answer)
	}
	if _, ok := resp.Answer[0].(*dns.CNAME); !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.CNAME", resp.Answer[0])
	}
	a, ok := resp.Answer[1].(*dns.A)
	if !ok {
		t.Fatalf("Answer[1] type = %T, want *dns.A", resp.Answer[1])
	}
	if got := a.A.String(); got != "1.2.3.4" {
		t.Errorf("A value = %s, want 1.2.3.4", got)
	}
	for _, rr := range resp.Answer {
		if _, isTXT := rr.(*dns.TXT); isTXT {
			t.Errorf("answer contains TXT RR %v; ephemeral TXT must not leak into A query", rr)
		}
	}
}

// TestEphemeral_OverridesExactCNAME_BackupZone verifies that an ephemeral TXT
// entry keyed by the backup-namespace qname overrides a root-zone exact CNAME
// at the rewritten qname for TXT queries. Backup zone path equivalent of
// TestEphemeral_OverridesExactCNAME_RootZone.
func TestEphemeral_OverridesExactCNAME_BackupZone(t *testing.T) {
	rootZ := buildRootZone("example.com.",
		makeCNAMERecord("_acme-challenge.foo.example.com.", "acme-dns.external.net.", 300),
	)
	backupZ := buildBackupZone("backup.com.")
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.backup.com.", "backup-token", 60)

	addr, cancel := newRootBackupServerWithEphemeral(t, rootZ, backupZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.foo.backup.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (ephemeral TXT only, no CNAME); Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "backup-token" {
		t.Errorf("TXT value = %v, want [backup-token]", txt.Txt)
	}
	if txt.Hdr.Name != "_acme-challenge.foo.backup.com." {
		t.Errorf("owner name = %q, want _acme-challenge.foo.backup.com.", txt.Hdr.Name)
	}
	for _, rr := range resp.Answer {
		if _, isCNAME := rr.(*dns.CNAME); isCNAME {
			t.Errorf("answer contains CNAME RR %v; CNAME must be suppressed when ephemeral TXT exists on backup zone", rr)
		}
	}
}

// TestEphemeral_ExactBeatsWildcardCNAMEInBackupZone verifies that an ephemeral
// TXT entry registered under the backup-namespace qname takes precedence over
// the backup-derived wildcard CNAME synthesized from the root zone.
func TestEphemeral_ExactBeatsWildcardCNAMEInBackupZone(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeCNAMERecord("*.root.com.", "target.other.com.", 300),
	)
	backupZ := buildBackupZone("backup.com.")
	store := ephemeral.NewStore()
	store.Put("_acme-challenge.foo.backup.com.", "backup-token", 60)

	addr, cancel := newRootBackupServerWithEphemeral(t, rootZ, backupZ, store)
	defer cancel()

	resp := query(t, "udp", addr, "_acme-challenge.foo.backup.com.", dns.TypeTXT)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %d, want NOERROR", resp.Rcode)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1 (ephemeral only, no synthesized CNAME); Answer=%v", len(resp.Answer), resp.Answer)
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("Answer[0] type = %T, want *dns.TXT (ephemeral must suppress wildcard CNAME synthesis)", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "backup-token" {
		t.Errorf("TXT value = %v, want [backup-token]", txt.Txt)
	}
	if txt.Hdr.Name != "_acme-challenge.foo.backup.com." {
		t.Errorf("owner name = %q, want _acme-challenge.foo.backup.com.", txt.Hdr.Name)
	}
}
