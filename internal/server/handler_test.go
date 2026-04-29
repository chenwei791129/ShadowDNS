package server

import (
	"bytes"
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// recordingWriter is a UDP dns.ResponseWriter stub that captures the bytes
// written via WriteMsg or Write, so tests can inspect the wire format.
type recordingWriter struct {
	Packed []byte
}

func (r *recordingWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}
}

func (r *recordingWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 40000}
}

func (r *recordingWriter) WriteMsg(m *dns.Msg) error {
	b, err := m.Pack()
	if err != nil {
		return err
	}
	r.Packed = b
	return nil
}

func (r *recordingWriter) Write(b []byte) (int, error) {
	r.Packed = append([]byte(nil), b...)
	return len(b), nil
}

func (r *recordingWriter) Close() error        { return nil }
func (r *recordingWriter) TsigStatus() error   { return errors.New("not signed") }
func (r *recordingWriter) TsigTimersOnly(bool) {}
func (r *recordingWriter) Hijack()             {}

// buildTXTQuery builds a TXT query for qname with an optional EDNS0 OPT record.
// bufferSize=0 omits the OPT record (legacy 512-byte client).
func buildTXTQuery(qname string, bufferSize uint16) *dns.Msg {
	req := new(dns.Msg)
	req.SetQuestion(qname, dns.TypeTXT)
	if bufferSize > 0 {
		req.SetEdns0(bufferSize, false)
	}
	return req
}

// makeTXTsAtOwner builds n TXT RRs sharing owner, each with a unique value
// of length valueLen so the RDATA itself cannot be compressed away.
func makeTXTsAtOwner(owner string, n, valueLen int) []dns.RR {
	out := make([]dns.RR, 0, n)
	for i := 0; i < n; i++ {
		val := make([]byte, valueLen)
		for j := range val {
			val[j] = byte('a' + ((i*valueLen + j) % 26))
		}
		out = append(out, makeTXTRecord(owner, string(val), 30))
	}
	return out
}

// ownerNameWire returns the uncompressed wire-format encoding of an FQDN.
func ownerNameWire(t *testing.T, name string) []byte {
	t.Helper()
	buf := make([]byte, len(name)+1)
	off, err := dns.PackDomainName(name, buf, 0, nil, false)
	if err != nil {
		t.Fatalf("PackDomainName(%q): %v", name, err)
	}
	return buf[:off]
}

// TestReplyWithAnswer_UsesNameCompression asserts the spec requirement
// "Successful answer responses SHALL use DNS name compression": the second
// occurrence of a shared owner name in the wire MUST be a 2-byte pointer.
func TestReplyWithAnswer_UsesNameCompression(t *testing.T) {
	const owner = "_acme-challenge.example.com."
	answer := makeTXTsAtOwner(owner, 2, 32)
	req := buildTXTQuery(owner, 4096)

	w := &recordingWriter{}
	replyWithAnswer(w, req, answer)

	if len(w.Packed) == 0 {
		t.Fatal("recordingWriter captured no bytes")
	}

	nameWire := ownerNameWire(t, owner)
	// The first occurrence (in the Question section) must appear uncompressed.
	if bytes.Count(w.Packed, nameWire) != 1 {
		t.Fatalf("expected exactly one uncompressed occurrence of %q in wire (question section); got %d\npacked=% x",
			owner, bytes.Count(w.Packed, nameWire), w.Packed)
	}

	// Scan the Answer section for at least one 2-byte compression pointer
	// pointing back to an earlier offset. A pointer byte has its top two bits
	// set (0xC0 mask) per RFC 1035 §4.1.4.
	questionEnd := bytes.Index(w.Packed, nameWire) + len(nameWire) + 4 // +qtype+qclass
	foundPointer := false
	for i := questionEnd; i < len(w.Packed)-1; i++ {
		if w.Packed[i]&0xC0 == 0xC0 {
			foundPointer = true
			break
		}
	}
	if !foundPointer {
		t.Errorf("no compression pointer (0xC0..) found after question section; answer RRs are not compressed\npacked=% x", w.Packed)
	}
}

// TestReplyWithAnswer_UDPRespectsEDNS0Budget asserts the spec requirement
// "UDP response size SHALL NOT exceed the advertised EDNS0 buffer" using a
// 48-TXT shared-owner response with EDNS0 UDPSize=4096.
func TestReplyWithAnswer_UDPRespectsEDNS0Budget(t *testing.T) {
	const owner = "_acme-challenge.example.com."
	answer := makeTXTsAtOwner(owner, 48, 80) // 48 × 80-byte values → uncompressed > 4096
	req := buildTXTQuery(owner, 4096)

	w := &recordingWriter{}
	replyWithAnswer(w, req, answer)

	if got := len(w.Packed); got > 4096 {
		t.Errorf("packed wire size %d bytes exceeds EDNS0 budget 4096", got)
	}

	// Sanity: the uncompressed, untruncated answer is deliberately oversized.
	// Packing the answer on its own without compression should exceed 4096
	// (guards the test from accidentally becoming trivial).
	rawMsg := new(dns.Msg)
	rawMsg.SetReply(req)
	rawMsg.Answer = answer
	rawBytes, err := rawMsg.Pack()
	if err != nil {
		t.Fatalf("pack raw msg: %v", err)
	}
	if len(rawBytes) < 4096 {
		t.Fatalf("test setup invalid: uncompressed answer is only %d bytes, need > 4096 to exercise truncation", len(rawBytes))
	}

	// Truncation MUST raise TC=1 so the client retries over TCP; a silent
	// drop without TC would regress to the pre-fix ambiguous behavior.
	resp := new(dns.Msg)
	if err := resp.Unpack(w.Packed); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if !resp.Truncated {
		t.Error("expected TC=1 when RRs are dropped to fit EDNS0 budget")
	}
}

// TestReplyWithAnswer_UDPNoEDNSFallsBackTo512 asserts the spec requirement
// "UDP response without EDNS0 falls back to 512-byte budget": no OPT record
// means the response MUST be ≤ 512 bytes with TC=1 when answers are dropped.
func TestReplyWithAnswer_UDPNoEDNSFallsBackTo512(t *testing.T) {
	const owner = "big.example.com."
	answer := makeTXTsAtOwner(owner, 20, 50) // well over 512 bytes uncompressed
	req := buildTXTQuery(owner, 0)           // no EDNS → 512 budget

	w := &recordingWriter{}
	replyWithAnswer(w, req, answer)

	if got := len(w.Packed); got > 512 {
		t.Errorf("packed wire size %d bytes exceeds 512-byte fallback budget", got)
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(w.Packed); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if !resp.Truncated {
		t.Error("expected TC=1 when answers are dropped to fit 512-byte budget")
	}
}

// ---------------------------------------------------------------------------
// Case preservation tests (RFC 4343 + DNS-0x20 echo)
// ---------------------------------------------------------------------------

// TestRootZone_MixedCaseQuery_PreservesCase covers two contracts in one
// round trip:
//   - Question section MUST echo the on-wire case bit-for-bit (RFC 4343).
//   - Answer owner case MUST come from the zone-file storage, not from the
//     lookup-fold qname; the zone-file case is the source of truth.
func TestRootZone_MixedCaseQuery_PreservesCase(t *testing.T) {
	const (
		zoneFileOwner = "WwW.Root.Com." // operator-authored zone-file case
		queryOnWire   = "wWw.RoOt.cOm." // arbitrary 0x20-randomized query case
	)
	rootZ := buildRootZone("root.com.",
		makeARecord(zoneFileOwner, "1.2.3.4", 300),
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

	resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if got := resp.Question[0].Name; got != queryOnWire {
		t.Errorf("Question section case: got %q, want echo %q", got, queryOnWire)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d", len(resp.Answer))
	}
	if got := resp.Answer[0].Header().Name; got != zoneFileOwner {
		t.Errorf("Answer owner case: got %q, want zone-file %q", got, zoneFileOwner)
	}
}

// TestRootZone_WildcardMixedCaseQuery_OwnerEchoesQuery covers RFC 4592
// wildcard synthesis: the synthesized owner MUST adopt the on-wire case of
// the qname, because there is no zone-file owner to draw from for the
// wildcard label.
func TestRootZone_WildcardMixedCaseQuery_OwnerEchoesQuery(t *testing.T) {
	const queryOnWire = "WWW.Root.Com."
	rootZ := buildRootZone("root.com.",
		makeARecord("*.root.com.", "1.2.3.4", 300),
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

	resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if got := resp.Question[0].Name; got != queryOnWire {
		t.Errorf("Question section case: got %q, want echo %q", got, queryOnWire)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d", len(resp.Answer))
	}
	if got := resp.Answer[0].Header().Name; got != queryOnWire {
		t.Errorf("synthesized wildcard owner: got %q, want qname-echo %q", got, queryOnWire)
	}
}

// TestBackupAlias_MixedCaseQuery_PreservesAliasConfigCase exercises the
// alias rewrite path with three different operator-authored backup yaml
// cases, asserting:
//   - lookup is case-insensitive (every variant resolves the same record);
//   - Question section echoes the on-wire case of the query;
//   - Answer owner suffix matches the operator-authored backup yaml case
//     byte-for-byte (this is the exact-match path, so the owner prefix is
//     the zone-file storage case, not the qname case).
func TestBackupAlias_MixedCaseQuery_PreservesAliasConfigCase(t *testing.T) {
	const queryOnWire = "wWw.ExAmPlE.cOm."

	cases := []struct {
		name            string
		backupYAMLCase  string // operator-authored case in YAML (with trailing dot)
		wantAnswerOwner string // expected Answer[0].Header().Name
	}{
		{
			name:            "lowercase backup yaml",
			backupYAMLCase:  "example.com.",
			wantAnswerOwner: "www.example.com.", // prefix from zone-file, suffix from yaml
		},
		{
			name:            "mixed-case backup yaml",
			backupYAMLCase:  "Example.Com.",
			wantAnswerOwner: "www.Example.Com.",
		},
		{
			name:            "all-uppercase backup yaml",
			backupYAMLCase:  "EXAMPLE.COM.",
			wantAnswerOwner: "www.EXAMPLE.COM.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rootZ := buildRootZone("root.com.",
				makeARecord("www.root.com.", "1.2.3.4", 300),
			)
			backupZ := buildBackupZone("example.com.")

			srv := NewServer(ServerState{
				Matcher:     makeAnyMatcher("default"),
				ZoneOrigins: map[string][]string{"default": {"root.com.", "example.com."}},
				RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
				BackupZones: map[string]map[string]*zone.Zone{"default": {"example.com.": backupZ}},
				Aliases:     config.AliasMap{"example.com.": "root.com."},
				BackupOriginalCase: map[string]string{
					"example.com.": tc.backupYAMLCase,
				},
			}, nil)

			udpAddr, _, cancel := startTestServer(t, srv)
			defer cancel()

			resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
			if resp.Rcode != dns.RcodeSuccess {
				t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
			}
			if got := resp.Question[0].Name; got != queryOnWire {
				t.Errorf("Question section case: got %q, want echo %q", got, queryOnWire)
			}
			if len(resp.Answer) != 1 {
				t.Fatalf("expected exactly 1 answer, got %d", len(resp.Answer))
			}
			if got := resp.Answer[0].Header().Name; got != tc.wantAnswerOwner {
				t.Errorf("Answer owner: got %q, want %q", got, tc.wantAnswerOwner)
			}
		})
	}
}

// TestBackupAlias_WildcardMixedCaseQuery_OwnerPreservesCase covers wildcard
// synthesis through the alias-rewrite path: the synthesized owner prefix
// must come from qnameOrig and the suffix from the operator-authored backup
// yaml case.
func TestBackupAlias_WildcardMixedCaseQuery_OwnerPreservesCase(t *testing.T) {
	const (
		queryOnWire    = "HoSt.Example.Com."
		backupYAMLCase = "Example.Com."
		wantOwner      = "HoSt.Example.Com." // prefix from qname, suffix from yaml
	)
	rootZ := buildRootZone("root.com.",
		makeARecord("*.root.com.", "1.2.3.4", 300),
	)
	backupZ := buildBackupZone("example.com.")

	srv := NewServer(ServerState{
		Matcher:     makeAnyMatcher("default"),
		ZoneOrigins: map[string][]string{"default": {"root.com.", "example.com."}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {"root.com.": rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{"default": {"example.com.": backupZ}},
		Aliases:     config.AliasMap{"example.com.": "root.com."},
		BackupOriginalCase: map[string]string{
			"example.com.": backupYAMLCase,
		},
	}, nil)

	udpAddr, _, cancel := startTestServer(t, srv)
	defer cancel()

	resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if got := resp.Question[0].Name; got != queryOnWire {
		t.Errorf("Question section case: got %q, want echo %q", got, queryOnWire)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d", len(resp.Answer))
	}
	if got := resp.Answer[0].Header().Name; got != wantOwner {
		t.Errorf("synthesized wildcard owner: got %q, want %q", got, wantOwner)
	}
}

// BenchmarkReplyWithAnswer_N48_Compressed records the Pack cost and wire size
// for 48 shared-owner TXT RRs with DNS name compression enabled.
func BenchmarkReplyWithAnswer_N48_Compressed(b *testing.B) {
	const owner = "_acme-challenge.example.com."
	answer := makeTXTsAtOwner(owner, 48, 43)

	// Prime once to capture the wire size, then loop Pack in the hot path.
	m := new(dns.Msg)
	req := buildTXTQuery(owner, 4096)
	m.SetReply(req)
	m.Authoritative = true
	m.Compress = true
	m.Answer = answer

	packed, err := m.Pack()
	if err != nil {
		b.Fatalf("prime Pack: %v", err)
	}
	b.Logf("n=48 compressed wire size: %d bytes", len(packed))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Pack(); err != nil {
			b.Fatalf("Pack: %v", err)
		}
	}
}
