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
