package transfer

import (
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// buildTestZone builds a simple Zone with SOA + optional records.
func buildTestZone(origin string, extraRRs []dns.RR) *zone.Zone {
	z := &zone.Zone{Origin: origin}

	soa := &dns.SOA{
		Hdr:     dns.RR_Header{Name: origin, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:      "ns1." + origin,
		Mbox:    "admin." + origin,
		Serial:  2024010101,
		Refresh: 3600,
		Retry:   900,
		Expire:  604800,
		Minttl:  300,
	}
	z.AddRR(soa)

	for _, rr := range extraRRs {
		z.AddRR(rr)
	}
	return z
}

// startAXFRServer starts a DNS server (UDP+TCP) on a random localhost port
// with the given handler mux. Returns the bound address and a cleanup func.
func startAXFRServer(t *testing.T, mux *dns.ServeMux) (addr string, cleanup func()) {
	t.Helper()

	// Bind TCP listener.
	tcpLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("TCP listen: %v", err)
	}
	tcpAddr := tcpLn.Addr().String()

	// Bind UDP listener on the same port.
	_, portStr, _ := net.SplitHostPort(tcpAddr)
	udpAddr := "127.0.0.1:" + portStr
	udpConn, err := net.ListenPacket("udp4", udpAddr)
	if err != nil {
		_ = tcpLn.Close()
		t.Fatalf("UDP listen on %s: %v", udpAddr, err)
	}

	srvTCP := &dns.Server{
		Listener: tcpLn,
		Net:      "tcp",
		Handler:  mux,
	}
	srvUDP := &dns.Server{
		PacketConn: udpConn,
		Net:        "udp",
		Handler:    mux,
	}

	var wg sync.WaitGroup
	wg.Add(2)

	tcpReady := make(chan struct{})
	udpReady := make(chan struct{})
	srvTCP.NotifyStartedFunc = func() { close(tcpReady) }
	srvUDP.NotifyStartedFunc = func() { close(udpReady) }

	go func() { defer wg.Done(); _ = srvTCP.ActivateAndServe() }()
	go func() { defer wg.Done(); _ = srvUDP.ActivateAndServe() }()

	<-tcpReady
	<-udpReady

	return tcpAddr, func() {
		_ = srvTCP.Shutdown()
		_ = srvUDP.Shutdown()
		wg.Wait()
	}
}

// collectAXFR performs a TCP AXFR using dns.Transfer.In and returns all RRs.
func collectAXFR(t *testing.T, qname, hostport string) []dns.RR {
	t.Helper()

	req := new(dns.Msg)
	req.SetAxfr(dns.Fqdn(qname))

	tr := new(dns.Transfer)
	ch, err := tr.In(req, hostport)
	if err != nil {
		t.Fatalf("Transfer.In: %v", err)
	}

	var rrs []dns.RR
	for env := range ch {
		if env.Error != nil {
			t.Fatalf("AXFR envelope error: %v", env.Error)
		}
		rrs = append(rrs, env.RR...)
	}
	return rrs
}

// --------------------------------------------------------------------------
// --------------------------------------------------------------------------

func TestHandleAXFR_TCP_StreamsSOARecordsSOA(t *testing.T) {
	origin := "example.com."
	aRec := &dns.A{
		Hdr: dns.RR_Header{Name: "www." + origin, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("192.0.2.1"),
	}
	z := buildTestZone(origin, []dns.RR{aRec})

	mux := dns.NewServeMux()
	mux.HandleFunc(origin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAXFR(w, req, z, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	rrs := collectAXFR(t, origin, addr)

	if len(rrs) < 3 {
		t.Fatalf("expected at least 3 RRs (SOA, A, SOA), got %d: %v", len(rrs), rrs)
	}

	// First RR must be SOA.
	if _, ok := rrs[0].(*dns.SOA); !ok {
		t.Errorf("first RR must be SOA, got %T", rrs[0])
	}
	// Last RR must be SOA.
	if _, ok := rrs[len(rrs)-1].(*dns.SOA); !ok {
		t.Errorf("last RR must be SOA, got %T", rrs[len(rrs)-1])
	}

	// There should be an A record in the middle.
	foundA := false
	for _, rr := range rrs[1 : len(rrs)-1] {
		if a, ok := rr.(*dns.A); ok && a.Hdr.Name == "www."+origin {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("expected A record for www.%s in stream", origin)
	}
}

func TestHandleAXFR_UDP_Refused(t *testing.T) {
	origin := "example.com."
	z := buildTestZone(origin, nil)

	mux := dns.NewServeMux()
	mux.HandleFunc(origin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAXFR(w, req, z, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	// Send AXFR over UDP using dns.Exchange.
	req := new(dns.Msg)
	req.SetAxfr(dns.Fqdn(origin))

	resp, err := dns.Exchange(req, addr)
	if err != nil {
		t.Fatalf("UDP exchange error: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED over UDP, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// --------------------------------------------------------------------------
// --------------------------------------------------------------------------

func TestHandleAXFR_NilZone_Refused(t *testing.T) {
	origin := "missing.example."

	mux := dns.NewServeMux()
	mux.HandleFunc(origin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAXFR(w, req, nil, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	// Send over TCP — still expect REFUSED because zone is nil.
	conn, err := net.Dial("tcp4", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	dnsConn := &dns.Conn{Conn: conn}
	defer func() { _ = dnsConn.Close() }()

	req := new(dns.Msg)
	req.SetAxfr(dns.Fqdn(origin))
	if err := dnsConn.WriteMsg(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := dnsConn.ReadMsg()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED for nil zone, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// --------------------------------------------------------------------------
// --------------------------------------------------------------------------

func TestHandleTransfer_IXFR_FallsBackToAXFR(t *testing.T) {
	origin := "example.com."
	aRec := &dns.A{
		Hdr: dns.RR_Header{Name: "www." + origin, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("203.0.113.1"),
	}
	z := buildTestZone(origin, []dns.RR{aRec})

	mux := dns.NewServeMux()
	mux.HandleFunc(origin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAXFR(w, req, z, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	// Build an IXFR request (need a SOA in Ns section for the serial).
	req := new(dns.Msg)
	req.SetIxfr(dns.Fqdn(origin), 1, "ns1."+origin, "admin."+origin)

	tr := new(dns.Transfer)
	ch, err := tr.In(req, addr)
	if err != nil {
		t.Fatalf("Transfer.In IXFR: %v", err)
	}

	var rrs []dns.RR
	for env := range ch {
		if env.Error != nil {
			// Some clients return an error for an unexpected serial; check if we got data.
			t.Logf("IXFR envelope error (may be expected): %v", env.Error)
		}
		rrs = append(rrs, env.RR...)
	}

	// Verify we got a SOA-start / SOA-end stream (full AXFR format).
	if len(rrs) < 2 {
		t.Fatalf("expected at least 2 RRs in fallback AXFR, got %d", len(rrs))
	}
	if _, ok := rrs[0].(*dns.SOA); !ok {
		t.Errorf("first RR must be SOA, got %T", rrs[0])
	}
	if _, ok := rrs[len(rrs)-1].(*dns.SOA); !ok {
		t.Errorf("last RR must be SOA, got %T", rrs[len(rrs)-1])
	}
}

// --------------------------------------------------------------------------
// --------------------------------------------------------------------------

func buildRootZone() *zone.Zone {
	origin := "root.com."
	z := buildTestZone(origin, nil)

	// A record (in-bailiwick owner).
	z.AddRR(&dns.A{
		Hdr: dns.RR_Header{Name: "www." + origin, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4"),
	})
	// CNAME in-bailiwick.
	z.AddRR(&dns.CNAME{
		Hdr:    dns.RR_Header{Name: "alias." + origin, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: "www." + origin,
	})
	// CNAME external.
	z.AddRR(&dns.CNAME{
		Hdr:    dns.RR_Header{Name: "ext." + origin, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: "s3.amazonaws.com.",
	})
	// NS (in-bailiwick).
	z.AddRR(&dns.NS{
		Hdr: dns.RR_Header{Name: origin, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 3600},
		Ns:  "ns1." + origin,
	})
	// MX.
	z.AddRR(&dns.MX{
		Hdr:        dns.RR_Header{Name: origin, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 3600},
		Preference: 10,
		Mx:         "mail." + origin,
	})
	// TXT.
	z.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: origin, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"root-txt"},
	})
	return z
}

func buildBackupZoneWithTXTOverride() *zone.Zone {
	backupOrigin := "backup.com."
	z := &zone.Zone{Origin: backupOrigin}
	// TXT override at the backup apex.
	z.AddRR(&dns.TXT{
		Hdr: dns.RR_Header{Name: backupOrigin, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"override-txt"},
	})
	return z
}

func TestHandleAliasAXFR_RewritesRecords(t *testing.T) {
	rootZone := buildRootZone()
	backupZone := buildBackupZoneWithTXTOverride()
	backupOrigin := "backup.com."

	mux := dns.NewServeMux()
	mux.HandleFunc(backupOrigin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAliasAXFR(w, req, backupOrigin, backupOrigin, rootZone, backupZone, false, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	rrs := collectAXFR(t, backupOrigin, addr)
	if len(rrs) < 2 {
		t.Fatalf("expected at least 2 RRs, got %d", len(rrs))
	}

	// First and last must be SOA with backup origin.
	firstSOA, ok := rrs[0].(*dns.SOA)
	if !ok {
		t.Fatalf("first RR must be SOA, got %T", rrs[0])
	}
	if firstSOA.Hdr.Name != backupOrigin {
		t.Errorf("first SOA owner: want %s, got %s", backupOrigin, firstSOA.Hdr.Name)
	}

	lastSOA, ok := rrs[len(rrs)-1].(*dns.SOA)
	if !ok {
		t.Fatalf("last RR must be SOA, got %T", rrs[len(rrs)-1])
	}
	if lastSOA.Hdr.Name != backupOrigin {
		t.Errorf("last SOA owner: want %s, got %s", backupOrigin, lastSOA.Hdr.Name)
	}

	middle := rrs[1 : len(rrs)-1]

	// A record: owner rewritten, IP unchanged.
	foundA := false
	for _, rr := range middle {
		if a, ok := rr.(*dns.A); ok {
			if a.Hdr.Name == "www."+backupOrigin {
				foundA = true
				if a.A.String() != "1.2.3.4" {
					t.Errorf("A record IP wrong: %s", a.A)
				}
			}
		}
	}
	if !foundA {
		t.Errorf("expected A record for www.%s", backupOrigin)
	}

	// CNAME in-bailiwick: owner + target rewritten.
	foundCNAMEInBailiwick := false
	for _, rr := range middle {
		if c, ok := rr.(*dns.CNAME); ok {
			if c.Hdr.Name == "alias."+backupOrigin {
				foundCNAMEInBailiwick = true
				if c.Target != "www."+backupOrigin {
					t.Errorf("in-bailiwick CNAME target wrong: %s", c.Target)
				}
			}
		}
	}
	if !foundCNAMEInBailiwick {
		t.Errorf("expected in-bailiwick CNAME for alias.%s", backupOrigin)
	}

	// CNAME external: owner rewritten, target preserved.
	foundCNAMEExt := false
	for _, rr := range middle {
		if c, ok := rr.(*dns.CNAME); ok {
			if c.Hdr.Name == "ext."+backupOrigin {
				foundCNAMEExt = true
				if c.Target != "s3.amazonaws.com." {
					t.Errorf("external CNAME target should be unchanged, got %s", c.Target)
				}
			}
		}
	}
	if !foundCNAMEExt {
		t.Errorf("expected external CNAME for ext.%s", backupOrigin)
	}

	// TXT at backup apex: must be the override, NOT the root TXT rewritten.
	for _, rr := range middle {
		if txt, ok := rr.(*dns.TXT); ok && txt.Hdr.Name == backupOrigin {
			// Must contain override text.
			joined := strings.Join(txt.Txt, "")
			if joined == "root-txt" {
				t.Error("TXT at backup apex must be override, not root TXT rewritten")
			}
			if joined != "override-txt" {
				t.Errorf("TXT override: expected 'override-txt', got %q", joined)
			}
		}
	}
}

func TestHandleAliasAXFR_NilBackupZone_AllRootRecords(t *testing.T) {
	rootZone := buildRootZone()
	backupOrigin := "backup.com."

	mux := dns.NewServeMux()
	mux.HandleFunc(backupOrigin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAliasAXFR(w, req, backupOrigin, backupOrigin, rootZone, nil, false, zap.NewNop()) // nil backup zone
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	rrs := collectAXFR(t, backupOrigin, addr)

	// With no backup zone, TXT at apex should be the root TXT rewritten.
	foundTXT := false
	for _, rr := range rrs {
		if txt, ok := rr.(*dns.TXT); ok && txt.Hdr.Name == backupOrigin {
			foundTXT = true
			if strings.Join(txt.Txt, "") != "root-txt" {
				t.Errorf("expected root TXT rewritten, got %v", txt.Txt)
			}
		}
	}
	if !foundTXT {
		t.Errorf("expected TXT record at %s apex", backupOrigin)
	}
}

func TestHandleAliasAXFR_UDP_Refused(t *testing.T) {
	rootZone := buildRootZone()
	backupOrigin := "backup.com."

	mux := dns.NewServeMux()
	mux.HandleFunc(backupOrigin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAliasAXFR(w, req, backupOrigin, backupOrigin, rootZone, nil, false, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	req := new(dns.Msg)
	req.SetAxfr(dns.Fqdn(backupOrigin))

	resp, err := dns.Exchange(req, addr)
	if err != nil {
		t.Fatalf("UDP exchange: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("expected REFUSED over UDP for alias AXFR, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// TestHandleAliasAXFR_PreservesBackupCase verifies that AXFR output uses the
// operator-authored backup case (e.g. "Backup.Com.") for owner / RDATA names,
// not the lookup-fold form passed for override-key matching.
func TestHandleAliasAXFR_PreservesBackupCase(t *testing.T) {
	rootZone := buildRootZone()
	backupOrigin := "backup.com."       // lookup-fold (used to key backupZone.Records)
	backupOriginalCase := "Backup.Com." // operator-authored case (used on the wire)

	mux := dns.NewServeMux()
	mux.HandleFunc(backupOrigin, func(w dns.ResponseWriter, req *dns.Msg) {
		HandleAliasAXFR(w, req, backupOrigin, backupOriginalCase, rootZone, nil, false, zap.NewNop())
	})

	addr, cleanup := startAXFRServer(t, mux)
	defer cleanup()

	rrs := collectAXFR(t, backupOrigin, addr)
	if len(rrs) < 2 {
		t.Fatalf("expected at least 2 RRs, got %d", len(rrs))
	}

	// First/last SOA must use operator case.
	firstSOA, ok := rrs[0].(*dns.SOA)
	if !ok {
		t.Fatalf("first RR must be SOA, got %T", rrs[0])
	}
	if firstSOA.Hdr.Name != backupOriginalCase {
		t.Errorf("first SOA owner: want %q, got %q", backupOriginalCase, firstSOA.Hdr.Name)
	}
	if firstSOA.Ns != "ns1."+backupOriginalCase {
		t.Errorf("first SOA Ns: want %q, got %q", "ns1."+backupOriginalCase, firstSOA.Ns)
	}

	// At least one rewritten owner must carry the operator case suffix.
	foundCased := false
	for _, rr := range rrs[1 : len(rrs)-1] {
		if a, ok := rr.(*dns.A); ok && a.Hdr.Name == "www."+backupOriginalCase {
			foundCased = true
		}
	}
	if !foundCased {
		t.Errorf("expected at least one A record with owner %q in AXFR output", "www."+backupOriginalCase)
	}
}
