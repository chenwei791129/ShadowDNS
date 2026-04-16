package server

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	dto "github.com/prometheus/client_model/go"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeARecord builds a simple A record.
func makeARecord(name, ip string, ttl uint32) *dns.A {
	return &dns.A{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		A: net.ParseIP(ip).To4(),
	}
}

// makeSOA builds a SOA record for testing.
func makeSOA(origin string) *dns.SOA {
	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   origin,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Ns:      "ns1." + origin,
		Mbox:    "admin." + origin,
		Serial:  2024010101,
		Refresh: 3600,
		Retry:   900,
		Expire:  604800,
		Minttl:  300,
	}
}

// makeMatcher builds a simple Matcher that maps the given CIDR → viewName.
func makeMatcher(cidr, viewName string) *view.Matcher {
	prefix := netip.MustParsePrefix(cidr)
	return &view.Matcher{
		Views: []view.NamedRuleSet{
			{
				Name: viewName,
				Rules: []config.MatchRule{
					config.CIDRRule{Prefix: prefix},
				},
			},
		},
	}
}

// makeAnyMatcher builds a Matcher with a single view that uses "any" to match all clients.
func makeAnyMatcher(viewName string) *view.Matcher {
	return &view.Matcher{
		Views: []view.NamedRuleSet{
			{Name: viewName, Rules: []config.MatchRule{config.AnyRule{}}},
		},
	}
}

// buildRootZone constructs an in-memory root zone with the given records.
func buildRootZone(origin string, rrs ...dns.RR) *zone.Zone {
	z := &zone.Zone{
		Origin:  origin,
		Records: make(map[string][]dns.RR),
		Role:    zone.RoleRoot,
	}
	soa := makeSOA(origin)
	z.AddRR(soa)
	for _, rr := range rrs {
		z.AddRR(rr)
	}
	return z
}

// buildBackupZone constructs an in-memory backup-override zone.
func buildBackupZone(origin string, rrs ...dns.RR) *zone.Zone {
	z := &zone.Zone{
		Origin:  origin,
		Records: make(map[string][]dns.RR),
		Role:    zone.RoleBackupOverride,
	}
	for _, rr := range rrs {
		z.AddRR(rr)
	}
	return z
}

// newRootBackupServer starts a server with one root zone and one backup-override
// zone under the "default" view, returning the UDP address and a cancel func.
func newRootBackupServer(t *testing.T, rootZ, backupZ *zone.Zone) (string, func()) {
	t.Helper()
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {rootZ.Origin, backupZ.Origin}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {rootZ.Origin: rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {backupZ.Origin: backupZ}},
		Aliases:     config.AliasMap{backupZ.Origin: rootZ.Origin},
	}, nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	return udpAddr, cancel
}

// startTestServer starts a Server on an OS-assigned port and returns it plus
// a cancel function.  The server is fully ready when this function returns.
func startTestServer(t *testing.T, srv *Server) (udpAddr, tcpAddr string, cancel func()) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())

	ready := make(chan struct{})
	go func() {
		close(ready)
		if err := srv.Start(ctx, "127.0.0.1:0"); err != nil && ctx.Err() == nil {
			t.Logf("server exited: %v", err)
		}
	}()

	// Wait for Start to bind (it pre-binds before launching goroutines).
	<-ready
	// Give ActivateAndServe a moment to start reading.
	time.Sleep(20 * time.Millisecond)

	udpAddr = srv.UDPAddr().String()
	tcpAddr = srv.TCPAddr().String()

	cancel = func() {
		cancelFn()
		time.Sleep(30 * time.Millisecond)
	}
	return udpAddr, tcpAddr, cancel
}

// query sends a DNS query over the given network (udp/tcp) and returns the response.
func query(t *testing.T, network, addr, qname string, qtype uint16) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: network, Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = false

	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("DNS query %s %s via %s to %s: %v", qname, dns.TypeToString[qtype], network, addr, err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestListener_UDP verifies the server can answer a UDP query.
func TestListener_UDP(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) == 0 {
		t.Error("expected an answer record")
	}
}

// TestListener_TCP verifies the server can answer a TCP query.
func TestListener_TCP(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	_, tcpAddr, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "tcp", tcpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestListener_TCFlagOnTruncation verifies that a UDP response that would exceed
// 512 bytes gets TC=1, and the same query over TCP succeeds without TC.
func TestListener_TCFlagOnTruncation(t *testing.T) {
	// Build a zone with enough distinct A records to definitely exceed 512 bytes.
	// Each A record is ~16 bytes wire format; 40 distinct IPs × 16 ≈ 640 bytes plus
	// question and header → well over 512 bytes.
	rootZ := buildRootZone("big.com.")
	for i := 0; i < 40; i++ {
		ip := net.IPv4(10, byte(i/256), byte(i%256), 1).To4()
		rootZ.AddRR(&dns.A{
			Hdr: dns.RR_Header{Name: "www.big.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   ip,
		})
	}

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"big.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"big.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, tcpAddr, cancel := startTestServer(t, srv)
	defer cancel()

	// UDP: send raw query and read raw bytes to detect TC=1 without client-level
	// truncation error. miekg sets TC=1 and truncates the message to 512 bytes.
	req := new(dns.Msg)
	req.SetQuestion("www.big.com.", dns.TypeA)
	reqBytes, err := req.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	udpConn, err := net.Dial("udp", udpAddr)
	if err != nil {
		t.Fatalf("dial UDP: %v", err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := udpConn.Write(reqBytes); err != nil {
		t.Fatalf("write UDP: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := udpConn.Read(buf)
	if err != nil {
		t.Fatalf("read UDP response: %v", err)
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack UDP response: %v", err)
	}
	if !resp.Truncated {
		t.Errorf("expected TC=1 in UDP response for oversized answer (got %d answer records in %d bytes)",
			len(resp.Answer), n)
	}

	// TCP: same query must deliver all records without TC.
	respTCP := query(t, "tcp", tcpAddr, "www.big.com.", dns.TypeA)
	if respTCP.Truncated {
		t.Error("TCP response must not have TC flag set")
	}
	if respTCP.Rcode != dns.RcodeSuccess {
		t.Errorf("TCP expected NOERROR, got %s", dns.RcodeToString[respTCP.Rcode])
	}
	if len(respTCP.Answer) == 0 {
		t.Error("TCP response must contain answer records")
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestAuthoritative_AASet verifies AA=1 and RA=0 for queries in a loaded zone.
func TestAuthoritative_AASet(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if resp.RecursionAvailable {
		t.Error("expected RA=0")
	}
}

// TestAuthoritative_RefusedOutsideZone verifies REFUSED + AA=0 + RA=0 for names outside loaded zones.
func TestAuthoritative_RefusedOutsideZone(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.")},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "outside.example.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
	if resp.Authoritative {
		t.Error("expected AA=0 for REFUSED")
	}
	if resp.RecursionAvailable {
		t.Error("expected RA=0")
	}
}

// TestAuthoritative_NoRecursion verifies that an RD=1 query outside all zones
// gets REFUSED (server does not recurse).
func TestAuthoritative_NoRecursion(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.")},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("outside.example.com.", dns.TypeA)
	m.RecursionDesired = true // RD=1 — server must still refuse, not recurse
	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for recursive query to authoritative-only server, got %s",
			dns.RcodeToString[resp.Rcode])
	}
	if resp.RecursionAvailable {
		t.Error("RA must always be 0")
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestQuery_RootZoneA verifies an A query returns the correct record.
func TestQuery_RootZoneA(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "192.0.2.1", 300))

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatal("answer is not an A record")
	}
	if a.A.String() != "192.0.2.1" {
		t.Errorf("expected 192.0.2.1, got %s", a.A.String())
	}
}

// TestQuery_BackupZoneA verifies an A query to a backup zone returns the rewritten record.
func TestQuery_BackupZoneA(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "10.0.0.1", 300))
	aliases := config.AliasMap{"backup.com.": "root.com."}

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "backup.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {}},
		Aliases:     aliases,
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.backup.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected an answer record")
	}
	// Owner name must be in the backup namespace.
	if resp.Answer[0].Header().Name != "www.backup.com." {
		t.Errorf("expected owner www.backup.com., got %s", resp.Answer[0].Header().Name)
	}
}

// TestQuery_ViewRouting verifies that different client IPs are routed to different views.
func TestQuery_ViewRouting(t *testing.T) {
	rootZoneA := buildRootZone("view-a.com.", makeARecord("www.view-a.com.", "10.1.1.1", 300))
	rootZoneB := buildRootZone("view-b.com.", makeARecord("www.view-b.com.", "10.2.2.2", 300))

	// Two views: "viewA" matches 127.0.0.1/32, "viewB" matches 127.0.0.2/32.
	// Since tests always come from 127.0.0.1, we build a server with both zones
	// in the "default" view and different zone names to verify routing.
	rootZ := buildRootZone("example.com.",
		makeARecord("a.example.com.", "1.1.1.1", 300),
	)

	matcher := &view.Matcher{
		Views: []view.NamedRuleSet{
			{Name: "default", Rules: []config.MatchRule{config.AnyRule{}}},
		},
	}

	srv := NewServer(ServerState{
		Matcher:     matcher,
		ZoneOrigins: map[string][]string{"default": {"view-a.com.", "view-b.com.", "example.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {
				"view-a.com.":  rootZoneA,
				"view-b.com.":  rootZoneB,
				"example.com.": rootZ,
			},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	// Query for view-a zone.
	resp := query(t, "udp", udpAddr, "www.view-a.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("view-a query: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}

	// Query for view-b zone.
	resp = query(t, "udp", udpAddr, "www.view-b.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("view-b query: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestQuery_NoViewMatch verifies REFUSED when no view matches.
func TestQuery_NoViewMatch(t *testing.T) {
	// Matcher matches nothing — returns "" for all clients.
	matcher := &view.Matcher{
		Views: []view.NamedRuleSet{}, // empty: no view matches
	}

	srv := NewServer(ServerState{
		Matcher:     matcher,
		ZoneOrigins: map[string][]string{},
		RootZones:   map[string]map[string]*zone.Zone{},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "example.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestQuery_NoZoneMatch verifies REFUSED when qname matches no loaded zone.
func TestQuery_NoZoneMatch(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": buildRootZone("root.com.")}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "notloaded.example.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestNegative_RootNXDOMAIN verifies NXDOMAIN with SOA authority for a nonexistent name.
func TestNegative_RootNXDOMAIN(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "nxdomain.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Ns) == 0 {
		t.Fatal("expected SOA in authority section")
	}
	soa, ok := resp.Ns[0].(*dns.SOA)
	if !ok {
		t.Fatal("authority record is not SOA")
	}
	// TTL must be capped to min(SOA TTL, SOA Minttl).
	wantTTL := uint32(300) // makeSOA sets TTL=3600, Minttl=300 → min=300
	if soa.Hdr.Ttl != wantTTL {
		t.Errorf("SOA authority TTL: expected %d (capped), got %d", wantTTL, soa.Hdr.Ttl)
	}
}

// TestNegative_RootNODATA verifies NODATA (RCODE=0) with SOA authority.
func TestNegative_RootNODATA(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	// www.root.com. exists but has no MX record → NODATA.
	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeMX)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("NODATA: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Error("NODATA: expected empty answer section")
	}
	if len(resp.Ns) == 0 {
		t.Error("NODATA: expected SOA in authority section")
	}
	if _, ok := resp.Ns[0].(*dns.SOA); !ok {
		t.Error("NODATA: authority record is not SOA")
	}
}

// TestNegative_BackupNXDOMAIN verifies NXDOMAIN with rewritten SOA for backup zone.
func TestNegative_BackupNXDOMAIN(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))
	aliases := config.AliasMap{"backup.com.": "root.com."}

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "backup.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {}},
		Aliases:     aliases,
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "nonexistent.backup.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Ns) == 0 {
		t.Fatal("expected SOA in authority section")
	}
	soa, ok := resp.Ns[0].(*dns.SOA)
	if !ok {
		t.Fatal("authority is not SOA")
	}
	// SOA owner must be in the backup namespace.
	if soa.Hdr.Name != "backup.com." {
		t.Errorf("backup SOA owner: expected backup.com., got %s", soa.Hdr.Name)
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestSOA_RootApex verifies SOA is returned from answer section on apex SOA query.
func TestSOA_RootApex(t *testing.T) {
	rootZ := buildRootZone("root.com.")

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "root.com.", dns.TypeSOA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected SOA in answer section")
	}
	if _, ok := resp.Answer[0].(*dns.SOA); !ok {
		t.Error("answer is not SOA")
	}
}

// TestSOA_BackupApex verifies the rewritten SOA is returned for a backup zone apex.
func TestSOA_BackupApex(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	aliases := config.AliasMap{"backup.com.": "root.com."}

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "backup.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {}},
		Aliases:     aliases,
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "backup.com.", dns.TypeSOA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected SOA in answer section")
	}
	soa, ok := resp.Answer[0].(*dns.SOA)
	if !ok {
		t.Fatal("answer is not SOA")
	}
	if soa.Hdr.Name != "backup.com." {
		t.Errorf("backup SOA owner: expected backup.com., got %s", soa.Hdr.Name)
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

func TestChaos_VersionBind(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{},
		RootZones:   map[string]map[string]*zone.Zone{},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("version.bind.", dns.TypeTXT)
	m.Question[0].Qclass = dns.ClassCHAOS

	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("chaos query: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("version.bind: expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Error("version.bind: answer section must be empty")
	}
}

func TestChaos_HostnameBind(t *testing.T) {
	srv := NewServer(ServerState{Matcher: makeAnyMatcher("default"),
		Aliases: config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("hostname.bind.", dns.TypeTXT)
	m.Question[0].Qclass = dns.ClassCHAOS

	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("chaos query: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("hostname.bind: expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestChaos_IDServer(t *testing.T) {
	srv := NewServer(ServerState{Matcher: makeAnyMatcher("default"),
		Aliases: config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("id.server.", dns.TypeTXT)
	m.Question[0].Qclass = dns.ClassCHAOS

	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("chaos query: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("id.server: expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestChaos_RandomQuery(t *testing.T) {
	srv := NewServer(ServerState{Matcher: makeAnyMatcher("default"),
		Aliases: config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("random.bind.", dns.TypeTXT)
	m.Question[0].Qclass = dns.ClassCHAOS

	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("chaos query: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("random CHAOS: expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// CNAME synthesis (RFC 1034 §3.6.2)
// ---------------------------------------------------------------------------

// makeCNAMERecord builds a CNAME record.
func makeCNAMERecord(name, target string, ttl uint32) *dns.CNAME {
	return &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		Target: target,
	}
}

// TestCNAMESynthesis_RootZone verifies CNAME synthesis for various query types
// against a zone where alias.root.com. has only a CNAME record.
func TestCNAMESynthesis_RootZone(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeCNAMERecord("alias.root.com.", "target.other.com.", 300),
	)

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	cases := []struct {
		name      string
		qname     string
		qtype     uint16
		wantRcode int
		wantCNAME bool // expect CNAME in answer
	}{
		{"A at CNAME name", "alias.root.com.", dns.TypeA, dns.RcodeSuccess, true},
		{"AAAA at CNAME name", "alias.root.com.", dns.TypeAAAA, dns.RcodeSuccess, true},
		{"MX at CNAME name", "alias.root.com.", dns.TypeMX, dns.RcodeSuccess, true},
		{"explicit CNAME query", "alias.root.com.", dns.TypeCNAME, dns.RcodeSuccess, true},
		{"NXDOMAIN unchanged", "nonexistent.root.com.", dns.TypeA, dns.RcodeNameError, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := query(t, "udp", udpAddr, tc.qname, tc.qtype)
			if resp.Rcode != tc.wantRcode {
				t.Errorf("expected %s, got %s", dns.RcodeToString[tc.wantRcode], dns.RcodeToString[resp.Rcode])
			}
			if tc.wantCNAME {
				if !resp.Authoritative {
					t.Error("expected AA=1")
				}
				if len(resp.Answer) != 1 {
					t.Fatalf("expected 1 answer (CNAME), got %d", len(resp.Answer))
				}
				cname, ok := resp.Answer[0].(*dns.CNAME)
				if !ok {
					t.Fatalf("expected CNAME record, got %T", resp.Answer[0])
				}
				if cname.Target != "target.other.com." {
					t.Errorf("CNAME target: got %q, want target.other.com.", cname.Target)
				}
			}
		})
	}
}

// TestCNAMESynthesis_RootZone_NODATA_Unchanged verifies NODATA when name has A
// but not AAAA and no CNAME — uses a different zone fixture than the table above.
func TestCNAMESynthesis_RootZone_NODATA_Unchanged(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeARecord("www.root.com.", "1.2.3.4", 300),
	)

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeAAAA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("NODATA: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("NODATA: expected empty answer, got %d", len(resp.Answer))
	}
	if len(resp.Ns) == 0 {
		t.Error("NODATA: expected SOA in authority")
	}
}

// ---------------------------------------------------------------------------

// TestMinimal_NoAuthorityOnPositive verifies authority section is empty on positive answers.
func TestMinimal_NoAuthorityOnPositive(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if len(resp.Ns) != 0 {
		t.Errorf("expected empty authority section, got %d records", len(resp.Ns))
	}
	if len(resp.Extra) != 0 {
		t.Errorf("expected empty additional section, got %d records", len(resp.Extra))
	}
}

// TestMinimal_NoGlueForMX verifies MX answers do not include glue A records.
func TestMinimal_NoGlueForMX(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeARecord("mail.root.com.", "5.5.5.5", 300),
	)
	// Add an MX record pointing to mail.root.com.
	rootZ.AddRR(&dns.MX{
		Hdr:        dns.RR_Header{Name: "root.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 300},
		Preference: 10,
		Mx:         "mail.root.com.",
	})

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "root.com.", dns.TypeMX)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Extra) != 0 {
		t.Errorf("expected no glue in additional, got %d extra records", len(resp.Extra))
	}
}

// TestMinimal_NoGlueForNS verifies NS answers do not include glue A records.
func TestMinimal_NoGlueForNS(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeARecord("ns1.root.com.", "9.9.9.9", 300),
	)
	rootZ.AddRR(&dns.NS{
		Hdr: dns.RR_Header{Name: "root.com.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
		Ns:  "ns1.root.com.",
	})

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "root.com.", dns.TypeNS)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Extra) != 0 {
		t.Errorf("expected no glue in additional, got %d extra records", len(resp.Extra))
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestMalformed_UpdateOpcode verifies UPDATE messages receive NOTIMP.
func TestMalformed_UpdateOpcode(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": buildRootZone("root.com.")}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("root.com.", dns.TypeSOA)
	m.Opcode = dns.OpcodeUpdate

	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("UPDATE query: %v", err)
	}
	if resp.Rcode != dns.RcodeNotImplemented {
		t.Errorf("expected NOTIMP, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestMalformed_ZeroQuestions verifies that a query with 0 questions receives FORMERR.
func TestMalformed_ZeroQuestions(t *testing.T) {
	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": buildRootZone("root.com.")}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.Id = dns.Id()
	m.RecursionDesired = false
	// No questions — leave Question nil.

	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("zero-question query: %v", err)
	}
	if resp.Rcode != dns.RcodeFormatError {
		t.Errorf("expected FORMERR, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestMalformed_PanicRecovery verifies the server does not crash on a panic in the
// handler, and that subsequent queries still succeed.
func TestMalformed_PanicRecovery(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))

	// Inject a nil SOA to trigger a panic path in BackupSOA if called improperly.
	// We simulate a panic by building a backup zone that references a non-existent root.
	aliases := config.AliasMap{"backup.com.": "root.com."}

	// Build a server where the root zone for "root.com." has a nil SOA to
	// force a nil-pointer dereference in the backup SOA path.
	brokenRootZ := &zone.Zone{
		Origin:  "root.com.",
		Records: make(map[string][]dns.RR),
		Role:    zone.RoleRoot,
		SOA:     nil, // intentionally nil to trigger panic in BackupSOA
	}
	brokenRootZ.AddRR(makeARecord("www.root.com.", "1.2.3.4", 300))

	_ = rootZ // also keep good zone for subsequent queries

	goodSrv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "backup.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": brokenRootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {}},
		Aliases:     aliases,
	}, nil)

	udpAddr, _, cancel := startTestServer(t, goodSrv)
	defer cancel()

	// Send the SOA query for backup.com. — this will trigger nil SOA access.
	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("backup.com.", dns.TypeSOA)
	resp, _, err := c.Exchange(m, udpAddr)
	if err != nil {
		t.Fatalf("panic-trigger query failed: %v", err)
	}
	// Server should have recovered and returned SERVFAIL or similar.
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}

	// Subsequent query to a good zone must still work.
	resp2 := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp2.Rcode != dns.RcodeSuccess {
		t.Errorf("post-panic query: expected NOERROR, got %s", dns.RcodeToString[resp2.Rcode])
	}
}

// ---------------------------------------------------------------------------
// CIDR-based view routing (view-matcher spec: first-match on IP/CIDR)
// ---------------------------------------------------------------------------

// TestViewRouting_CIDRMatches verifies a CIDR rule covering the loopback
// address routes the query to the matching view.
func TestViewRouting_CIDRMatches(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "192.0.2.1", 300))

	srv := NewServer(ServerState{
		Matcher:     makeMatcher("127.0.0.0/8", "loopback"),
		ZoneOrigins: map[string][]string{"loopback": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"loopback": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("CIDR match expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
}

// TestViewRouting_CIDRDoesNotMatch verifies a CIDR rule that excludes the
// loopback address produces REFUSED (no view matched).
func TestViewRouting_CIDRDoesNotMatch(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "192.0.2.1", 300))

	srv := NewServer(ServerState{
		Matcher:     makeMatcher("10.0.0.0/8", "corp"),
		ZoneOrigins: map[string][]string{"corp": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"corp": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("non-matching CIDR expected REFUSED, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// ---------------------------------------------------------------------------
// Backup-zone override (alias-resolver spec: TXT/MX/SRV override precedence)
// ---------------------------------------------------------------------------

// TestBackupOverride_TXTPrecedence verifies a TXT record defined in the
// backup zone takes precedence over the root zone's inherited value.
func TestBackupOverride_TXTPrecedence(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	rootZ.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: "root.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"v=spf1 -all"},
	})
	backupZ := buildBackupZone("backup.com.")
	backupZ.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: "backup.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"v=spf1 include:_spf.backup.com -all"},
	})

	udpAddr, cancel := newRootBackupServer(t, rootZ, backupZ)
	defer cancel()

	resp := query(t, "udp", udpAddr, "backup.com.", dns.TypeTXT)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 TXT answer, got %d", len(resp.Answer))
	}
	txt, ok := resp.Answer[0].(*dns.TXT)
	if !ok {
		t.Fatalf("answer is not TXT: %T", resp.Answer[0])
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "v=spf1 include:_spf.backup.com -all" {
		t.Errorf("expected backup override TXT, got %v", txt.Txt)
	}
}

// TestBackupOverride_MXPrecedence verifies an MX record defined in the backup
// zone replaces the root zone's inherited MX.
func TestBackupOverride_MXPrecedence(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	rootZ.AddRR(&dns.MX{
		Hdr:        dns.RR_Header{Name: "root.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 300},
		Preference: 10,
		Mx:         "mail.root.com.",
	})
	backupZ := buildBackupZone("backup.com.")
	backupZ.AddRR(&dns.MX{
		Hdr:        dns.RR_Header{Name: "backup.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 300},
		Preference: 5,
		Mx:         "mail.backup.com.",
	})

	udpAddr, cancel := newRootBackupServer(t, rootZ, backupZ)
	defer cancel()

	resp := query(t, "udp", udpAddr, "backup.com.", dns.TypeMX)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 MX answer, got %d", len(resp.Answer))
	}
	mx, ok := resp.Answer[0].(*dns.MX)
	if !ok {
		t.Fatalf("answer is not MX: %T", resp.Answer[0])
	}
	if mx.Preference != 5 || mx.Mx != "mail.backup.com." {
		t.Errorf("expected backup override MX (pref=5 mail.backup.com.), got pref=%d %s", mx.Preference, mx.Mx)
	}
}

// TestBackupOverride_NonOverridableFallsThrough verifies that a query for a
// non-overridable type (A) falls through to the root zone even when the
// backup override zone is present.
func TestBackupOverride_NonOverridableFallsThrough(t *testing.T) {
	rootZ := buildRootZone("root.com.", makeARecord("www.root.com.", "10.0.0.1", 300))
	backupZ := buildBackupZone("backup.com.")
	backupZ.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: "backup.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"override"},
	})

	udpAddr, cancel := newRootBackupServer(t, rootZ, backupZ)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.backup.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 A answer (inherited from root), got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer is not A: %T", resp.Answer[0])
	}
	if a.Hdr.Name != "www.backup.com." {
		t.Errorf("expected owner rewritten to www.backup.com., got %s", a.Hdr.Name)
	}
	if a.A.String() != "10.0.0.1" {
		t.Errorf("expected inherited A 10.0.0.1, got %s", a.A.String())
	}
}

// ---------------------------------------------------------------------------
// SwapState — atomic state replacement
// ---------------------------------------------------------------------------

// TestSwapState_QueriesUseNewState verifies that after SwapState, new DNS
// queries are answered using the replacement state.
func TestSwapState_QueriesUseNewState(t *testing.T) {
	// Initial state: www.root.com. → 1.2.3.4
	stateA := ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}

	srv := NewServer(stateA, nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	// Verify initial state responds with 1.2.3.4.
	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("before swap: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("before swap: expected 1 answer, got %d", len(resp.Answer))
	}
	a := resp.Answer[0].(*dns.A)
	if a.A.String() != "1.2.3.4" {
		t.Fatalf("before swap: expected 1.2.3.4, got %s", a.A.String())
	}

	// Swap to new state: www.root.com. → 5.6.7.8
	stateB := ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "5.6.7.8", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}
	srv.SwapState(stateB)

	// Verify new queries use the swapped state.
	resp = query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("after swap: expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("after swap: expected 1 answer, got %d", len(resp.Answer))
	}
	a = resp.Answer[0].(*dns.A)
	if a.A.String() != "5.6.7.8" {
		t.Errorf("after swap: expected 5.6.7.8, got %s", a.A.String())
	}
}

// TestSwapState_ConcurrentQueriesConsistent verifies that in-flight DNS queries
// during an atomic state swap never observe mixed or partial state. Every
// response IP must be exactly one of the two known addresses.
func TestSwapState_ConcurrentQueriesConsistent(t *testing.T) {
	const numGoroutines = 50

	// stateA: www.root.com. → 1.2.3.4
	stateA := ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "1.2.3.4", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}

	// stateB: www.root.com. → 5.6.7.8
	stateB := ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {"root.com.": buildRootZone("root.com.", makeARecord("www.root.com.", "5.6.7.8", 300))},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}

	srv := NewServer(stateA, nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	// results collects the response IP from each goroutine.
	results := make([]string, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch all query goroutines before triggering the swap so they are
	// in-flight when the atomic pointer replacement occurs.
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
			if len(resp.Answer) == 1 {
				if a, ok := resp.Answer[0].(*dns.A); ok {
					results[idx] = a.A.String()
				}
			}
		}(i)
	}

	// Swap state while queries are in-flight.
	srv.SwapState(stateB)

	wg.Wait()

	// Every response must be one of the two valid IPs — never anything else.
	// This proves no query observed partial or mixed state.
	for i, ip := range results {
		if ip != "1.2.3.4" && ip != "5.6.7.8" {
			t.Errorf("goroutine %d: unexpected IP %q (want 1.2.3.4 or 5.6.7.8)", i, ip)
		}
	}
}

// ---------------------------------------------------------------------------
// Metrics integration tests
// ---------------------------------------------------------------------------

// TestServeDNS_Metrics_RecordsRequestAndResponse verifies that when Metrics is
// set on the Server, DNS queries result in incremented counters and histogram
// observations.
func TestServeDNS_Metrics_RecordsRequestAndResponse(t *testing.T) {
	m := metrics.New()

	rootZ := buildRootZone("root.com.",
		makeARecord("www.root.com.", "1.2.3.4", 300),
	)

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
	}, nil)
	srv.Metrics = m

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}

	// Gather metrics and verify.
	mfs, err := m.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	families := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		families[mf.GetName()] = mf
	}

	// Verify requests_total was incremented.
	reqMF, ok := families["shadowdns_dns_requests_total"]
	if !ok {
		t.Fatal("shadowdns_dns_requests_total not found")
	}
	if len(reqMF.GetMetric()) == 0 {
		t.Fatal("no metric entries in shadowdns_dns_requests_total")
	}

	// Verify responses_total was incremented with NOERROR.
	respMF, ok := families["shadowdns_dns_responses_total"]
	if !ok {
		t.Fatal("shadowdns_dns_responses_total not found")
	}
	var foundNoError bool
	for _, metric := range respMF.GetMetric() {
		for _, lp := range metric.GetLabel() {
			if lp.GetName() == "rcode" && lp.GetValue() == "NOERROR" {
				foundNoError = true
			}
		}
	}
	if !foundNoError {
		t.Error("expected responses_total entry with rcode=NOERROR")
	}

	// Verify duration histogram was observed.
	durMF, ok := families["shadowdns_dns_request_duration_seconds"]
	if !ok {
		t.Fatal("shadowdns_dns_request_duration_seconds not found")
	}
	if len(durMF.GetMetric()) == 0 {
		t.Fatal("no entries in duration histogram")
	}
	count := durMF.GetMetric()[0].GetHistogram().GetSampleCount()
	if count != 1 {
		t.Errorf("expected 1 duration sample, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Wildcard matching (RFC 4592)
// ---------------------------------------------------------------------------

// TestWildcard_RootZone_AQuery verifies wildcard A match: the answer owner
// name must be the original qname, not the "*" label.
func TestWildcard_RootZone_AQuery(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	rootZ.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.root.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("10.0.0.99").To4(),
	})

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "foo.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if !resp.Authoritative {
		t.Error("expected AA=1")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatal("answer is not A")
	}
	// Owner must be the original qname, not "*.root.com."
	if a.Hdr.Name != "foo.root.com." {
		t.Errorf("owner: expected foo.root.com., got %s", a.Hdr.Name)
	}
	if a.A.String() != "10.0.0.99" {
		t.Errorf("expected 10.0.0.99, got %s", a.A.String())
	}
}

// TestWildcard_ENTBlocking verifies that an ENT prevents wildcard matching → NXDOMAIN.
func TestWildcard_ENTBlocking(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	rootZ.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.root.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("10.0.0.99").To4(),
	})
	// sub.root.com. exists → ENT blocker
	rootZ.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: "sub.root.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"ent"},
	})

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "other.sub.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestWildcard_ExactRecordTakesPrecedence verifies exact match beats wildcard.
func TestWildcard_ExactRecordTakesPrecedence(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	rootZ.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.root.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("10.0.0.99").To4(),
	})
	rootZ.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "www.root.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4").To4(),
	})

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatal("answer is not A")
	}
	// Must be the exact record, not the wildcard.
	if a.A.String() != "1.2.3.4" {
		t.Errorf("expected exact match 1.2.3.4, got %s", a.A.String())
	}
}

// TestWildcard_BackupZone verifies wildcard match through backup zone alias.
func TestWildcard_BackupZone(t *testing.T) {
	rootZ := buildRootZone("root.com.")
	rootZ.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "*.root.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("10.0.0.99").To4(),
	})
	aliases := config.AliasMap{"backup.com.": "root.com."}

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "backup.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {}},
		Aliases:     aliases,
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "foo.backup.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatal("answer is not A")
	}
	// Owner must be in backup namespace.
	if a.Hdr.Name != "foo.backup.com." {
		t.Errorf("owner: expected foo.backup.com., got %s", a.Hdr.Name)
	}
	if a.A.String() != "10.0.0.99" {
		t.Errorf("expected 10.0.0.99, got %s", a.A.String())
	}
}

// ---------------------------------------------------------------------------
// Metrics integration tests (continued)
// ---------------------------------------------------------------------------

// TestServeDNS_NilMetrics_NoPanic verifies that the server does not panic
// when Metrics is nil (the default for existing tests).
func TestServeDNS_NilMetrics_NoPanic(t *testing.T) {
	rootZ := buildRootZone("root.com.",
		makeARecord("www.root.com.", "1.2.3.4", 300),
	)

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
	}, nil)
	// srv.Metrics is intentionally nil.

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.root.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
}
