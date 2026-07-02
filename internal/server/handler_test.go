package server

import (
	"bytes"
	"errors"
	"net"
	"net/netip"
	"strings"
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

// addrFromRemoteWriter is a minimal dns.ResponseWriter stub used by
// TestAddrFromRemote that returns a configurable RemoteAddr (or nil).
type addrFromRemoteWriter struct {
	remote net.Addr
}

func (w *addrFromRemoteWriter) LocalAddr() net.Addr       { return nil }
func (w *addrFromRemoteWriter) RemoteAddr() net.Addr      { return w.remote }
func (w *addrFromRemoteWriter) WriteMsg(*dns.Msg) error   { return nil }
func (w *addrFromRemoteWriter) Write([]byte) (int, error) { return 0, nil }
func (w *addrFromRemoteWriter) Close() error              { return nil }
func (w *addrFromRemoteWriter) TsigStatus() error         { return errors.New("not signed") }
func (w *addrFromRemoteWriter) TsigTimersOnly(bool)       {}
func (w *addrFromRemoteWriter) Hijack()                   {}

// stubNetAddr is a non-UDP/TCP net.Addr used to exercise the default arm
// fallback in addrFromRemote.
type stubNetAddr struct{ s string }

func (a stubNetAddr) Network() string { return "stub" }
func (a stubNetAddr) String() string  { return a.s }

func TestAddrFromRemote(t *testing.T) {
	tests := []struct {
		name     string
		remote   net.Addr
		wantAddr netip.Addr
		wantErr  string // substring; empty means no error
		wantIs4  bool   // only checked when wantErr == ""
	}{
		{
			name:     "UDP 4-byte v4",
			remote:   &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4).To4(), Port: 53},
			wantAddr: netip.MustParseAddr("1.2.3.4"),
			wantIs4:  true,
		},
		{
			name:     "UDP 16-byte v4-in-v6 canonicalized via Unmap",
			remote:   &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}, // net.IPv4 returns 16-byte form
			wantAddr: netip.MustParseAddr("1.2.3.4"),
			wantIs4:  true,
		},
		{
			name:     "TCP 4-byte v4",
			remote:   &net.TCPAddr{IP: net.IPv4(5, 6, 7, 8).To4(), Port: 53},
			wantAddr: netip.MustParseAddr("5.6.7.8"),
			wantIs4:  true,
		},
		{
			name:     "UDP pure IPv6 stays IPv6 after Unmap",
			remote:   &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 53},
			wantAddr: netip.MustParseAddr("2001:db8::1"),
			wantIs4:  false,
		},
		{
			name:    "nil remote addr",
			remote:  nil,
			wantErr: "nil remote addr",
		},
		{
			name:    "UDP with nil IP triggers AddrFromSlice ok=false",
			remote:  &net.UDPAddr{IP: nil, Port: 53},
			wantErr: "invalid UDP IP slice length 0",
		},
		{
			name:     "default arm fallback via stub net.Addr",
			remote:   stubNetAddr{s: "9.10.11.12:5000"},
			wantAddr: netip.MustParseAddr("9.10.11.12"),
			wantIs4:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := &addrFromRemoteWriter{remote: tc.remote}
			got, err := addrFromRemote(w)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (addr=%v)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantAddr {
				t.Fatalf("addr mismatch: got %v want %v", got, tc.wantAddr)
			}
			if got.Is4() != tc.wantIs4 {
				t.Fatalf("Is4 mismatch: got %v want %v (addr=%v)", got.Is4(), tc.wantIs4, got)
			}
		})
	}
}

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
	replyWithAnswer(w, req, parseQueryOpt(req), answer)

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
	replyWithAnswer(w, req, parseQueryOpt(req), answer)

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
	replyWithAnswer(w, req, parseQueryOpt(req), answer)

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
// truncateForUDP binary-search truncation tests (GitHub issue #15)
// ---------------------------------------------------------------------------

// buildTruncMsg assembles a compressed reply carrying answer, matching the
// state truncateForUDP sees when replyWithAnswer calls it (SetReply + AA=1 +
// Compress=true), so these unit tests exercise the real truncation path.
func buildTruncMsg(owner string, answer []dns.RR) *dns.Msg {
	req := buildTXTQuery(owner, 0)
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = true
	m.Compress = true
	m.Answer = answer
	return m
}

// dropOneLoopSurvivors reproduces the pre-fix one-RR-at-a-time drop-and-repack
// loop and returns how many Answer RRs it retains for the same input, so the
// binary-search result can be asserted identical to the reference algorithm.
// It operates on a copy so the caller's message is left untouched.
func dropOneLoopSurvivors(t *testing.T, m *dns.Msg, budget int) int {
	t.Helper()
	ref := m.Copy()
	for {
		packed, err := ref.Pack()
		if err != nil {
			t.Fatalf("reference drop-one pack: %v", err)
		}
		if len(packed) <= budget {
			return len(ref.Answer)
		}
		if len(ref.Answer) == 0 {
			return 0
		}
		ref.Answer = ref.Answer[:len(ref.Answer)-1]
	}
}

// ceilLog2 returns ceil(log2(n)) for n >= 1 (0 for n <= 1).
func ceilLog2(n int) int {
	b := 0
	for (1 << b) < n {
		b++
	}
	return b
}

// TestTruncateForUDP_LargeRRsetMatchesDropOneLoop covers tasks 2.1 and 2.2: a
// large single-owner RRset trimmed to the 512-byte budget fits, sets TC=1,
// keeps >0 RRs, and retains exactly the prefix a drop-one reference loop would.
func TestTruncateForUDP_LargeRRsetMatchesDropOneLoop(t *testing.T) {
	const (
		owner  = "big.example.com."
		budget = 512
		n      = 1000
	)
	answer := makeTXTsAtOwner(owner, n, 5)

	// Reference survivor count from the pre-fix drop-one loop, computed before
	// truncation so the shared answer slice is still intact.
	want := dropOneLoopSurvivors(t, buildTruncMsg(owner, answer), budget)

	m := buildTruncMsg(owner, answer)
	truncateForUDP(m, budget)

	packed, err := m.Pack()
	if err != nil {
		t.Fatalf("pack truncated msg: %v", err)
	}
	if len(packed) > budget {
		t.Errorf("packed wire size %d bytes exceeds budget %d", len(packed), budget)
	}
	if !m.Truncated {
		t.Error("expected TC=1 after dropping RRs to fit budget")
	}
	if len(m.Answer) == 0 {
		t.Fatal("expected >0 surviving Answer RRs")
	}
	if len(m.Answer) != want {
		t.Errorf("binary search kept %d RRs; drop-one reference kept %d", len(m.Answer), want)
	}
	// Sanity: the input genuinely required trimming (guards against a trivially
	// passing test where nothing was dropped).
	if len(m.Answer) >= n {
		t.Fatalf("test setup invalid: no RRs dropped (kept %d of %d)", len(m.Answer), n)
	}
}

// TestTruncateForUDP_PackCallsAreLogarithmic covers task 2.4: for N RRs the
// truncation routine performs O(log N) Pack calls, not O(N). A linear
// implementation (~N calls) blows past the bound and fails.
func TestTruncateForUDP_PackCallsAreLogarithmic(t *testing.T) {
	const (
		owner  = "big.example.com."
		budget = 512
		n      = 1000
	)
	answer := makeTXTsAtOwner(owner, n, 5)
	m := buildTruncMsg(owner, answer)

	orig := packMsg
	var calls int
	packMsg = func(msg *dns.Msg) ([]byte, error) {
		calls++
		return orig(msg)
	}
	defer func() { packMsg = orig }()

	truncateForUDP(m, budget)

	// bound = 2*ceil(log2(N)) + 4: one full-answer probe + the O(log N) search
	// steps + a small constant slack.
	bound := 2*ceilLog2(n) + 4
	if calls > bound {
		t.Errorf("truncateForUDP made %d Pack calls for N=%d; want <= %d (logarithmic)", calls, n, bound)
	}
	// Concrete guard from the spec example: N=1000 must stay within 24.
	if calls > 24 {
		t.Errorf("N=1000: %d Pack calls exceeds the spec bound of 24", calls)
	}
}

// TestTruncateForUDP_Boundaries covers task 2.3: full answer within budget is
// untouched with TC unset; a lone RR over budget clears the Answer and sets TC;
// an empty Answer does not panic.
func TestTruncateForUDP_Boundaries(t *testing.T) {
	const owner = "big.example.com."

	t.Run("full answer fits: no drop, no TC", func(t *testing.T) {
		answer := makeTXTsAtOwner(owner, 2, 5)
		m := buildTruncMsg(owner, answer)
		truncateForUDP(m, 4096)
		if len(m.Answer) != 2 {
			t.Errorf("kept %d RRs; want 2 (nothing should be dropped)", len(m.Answer))
		}
		if m.Truncated {
			t.Error("TC must not be set when the full answer fits")
		}
	})

	t.Run("single RR over budget: Answer cleared, TC set", func(t *testing.T) {
		// budget fits header+question but not the sole RR, so k converges to 0.
		answer := makeTXTsAtOwner(owner, 1, 100)
		m := buildTruncMsg(owner, answer)
		const budget = 60
		truncateForUDP(m, budget)
		if len(m.Answer) != 0 {
			t.Errorf("Answer not cleared: kept %d RRs", len(m.Answer))
		}
		if !m.Truncated {
			t.Error("expected TC=1 when the only RR is dropped")
		}
		packed, err := m.Pack()
		if err != nil {
			t.Fatalf("pack header-only msg: %v", err)
		}
		if len(packed) > budget {
			t.Errorf("header-only wire size %d exceeds budget %d", len(packed), budget)
		}
	})

	t.Run("empty answer does not panic", func(t *testing.T) {
		m := buildTruncMsg(owner, nil)
		truncateForUDP(m, 512) // must not panic
		if len(m.Answer) != 0 {
			t.Errorf("empty answer became %d RRs", len(m.Answer))
		}
		if m.Truncated {
			t.Error("TC must not be set for an empty answer that fits")
		}
	})
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

// ---------------------------------------------------------------------------
// CNAME chain collapsing — root query path (design D4/D5/D6)
// ---------------------------------------------------------------------------

func makeAAAARecord(name, ip string, ttl uint32) *dns.AAAA {
	return &dns.AAAA{
		Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
		AAAA: net.ParseIP(ip),
	}
}

// newCollapseRootServer starts a server whose single root zone example.com.
// holds rrs and has collapse_cname_chain enabled.
func newCollapseRootServer(t *testing.T, rrs ...dns.RR) (string, func()) {
	t.Helper()
	rootZ := buildRootZone("example.com.", rrs...)
	srv := NewServer(ServerState{
		Matcher:       makeAnyMatcher("default"),
		ZoneOrigins:   map[string][]string{"default": {"example.com."}},
		RootZones:     map[string]map[string]*zone.Zone{"default": {"example.com.": rootZ}},
		BackupZones:   map[string]map[string]*zone.Zone{},
		Aliases:       config.AliasMap{},
		CollapseFlags: config.CollapseFlags{"example.com.": true},
	}, nil)
	udpAddr, _, cancel := startTestServer(t, srv)
	return udpAddr, cancel
}

// collapseChainRecords returns the spec's canonical multi-hop chain:
// www 300 CNAME lb, lb 60 CNAME pool-a, pool-a 600 A 192.0.2.10.
func collapseChainRecords() []dns.RR {
	return []dns.RR{
		makeCNAMERecord("www.example.com.", "lb.example.com.", 300),
		makeCNAMERecord("lb.example.com.", "pool-a.example.com.", 60),
		makeARecord("pool-a.example.com.", "192.0.2.10", 600),
	}
}

// (a) A multi-hop in-zone chain collapses to exactly the terminal record:
// owner echoes the on-wire qname, TTL is the chain minimum, no CNAME appears.
func TestRootCollapse_MultiHopChainToSingleRecord(t *testing.T) {
	const queryOnWire = "WwW.ExAmPle.Com."
	udpAddr, cancel := newCollapseRootServer(t, collapseChainRecords()...)
	defer cancel()

	resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d: %v", len(resp.Answer), resp.Answer)
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.A", resp.Answer[0])
	}
	if got := a.Hdr.Name; got != queryOnWire {
		t.Errorf("owner = %q, want on-wire qname %q", got, queryOnWire)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (chain minimum of 300,60,600)", a.Hdr.Ttl)
	}
	if a.A.String() != "192.0.2.10" {
		t.Errorf("A = %s, want 192.0.2.10", a.A)
	}
}

// (b) An out-of-zone tail collapses to one synthesized CNAME; the consumed
// intermediate names appear nowhere in the response.
func TestRootCollapse_OutOfZoneTailSynthesizesCNAME(t *testing.T) {
	udpAddr, cancel := newCollapseRootServer(t,
		makeCNAMERecord("www.example.com.", "lb.example.com.", 300),
		makeCNAMERecord("lb.example.com.", "pool-a.example.com.", 60),
		makeCNAMERecord("pool-a.example.com.", "cdn.external-vendor.example.org.", 600),
	)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.example.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d: %v", len(resp.Answer), resp.Answer)
	}
	cn, ok := resp.Answer[0].(*dns.CNAME)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.CNAME", resp.Answer[0])
	}
	if cn.Hdr.Name != "www.example.com." {
		t.Errorf("owner = %q, want www.example.com.", cn.Hdr.Name)
	}
	if cn.Target != "cdn.external-vendor.example.org." {
		t.Errorf("target = %q, want cdn.external-vendor.example.org.", cn.Target)
	}
	if cn.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (min of 300,60,600)", cn.Hdr.Ttl)
	}
	wire := resp.String()
	for _, hidden := range []string{"lb.example.com.", "pool-a.example.com."} {
		if strings.Contains(wire, hidden) {
			t.Errorf("intermediate name %q leaked into the response:\n%s", hidden, wire)
		}
	}
}

// (c) AAAA over an A-only chain tail is NODATA (NOERROR + SOA in authority),
// and a wildcard covering the original qname is NOT consulted — the collapse
// NODATA short-circuits instead of falling through to wildcard synthesis
// (spec scenario "Collapse NODATA does not trigger wildcard synthesis").
func TestRootCollapse_NoDataShortCircuitsWildcardSynthesis(t *testing.T) {
	udpAddr, cancel := newCollapseRootServer(t,
		makeCNAMERecord("www.app.example.com.", "pool-a.example.com.", 300),
		makeARecord("pool-a.example.com.", "192.0.2.10", 600),
		makeAAAARecord("*.app.example.com.", "2001:db8::1", 300),
	)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.app.example.com.", dns.TypeAAAA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR (NODATA), got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Fatalf("expected zero answers (wildcard must not be consulted), got %d: %v", len(resp.Answer), resp.Answer)
	}
	if len(resp.Ns) != 1 {
		t.Fatalf("expected SOA in authority section, got %d records", len(resp.Ns))
	}
	if _, ok := resp.Ns[0].(*dns.SOA); !ok {
		t.Errorf("authority record: got %T, want *dns.SOA", resp.Ns[0])
	}
}

// Plain AAAA NODATA over the canonical chain (implementation contract row 3).
func TestRootCollapse_AAAAOverAOnlyTailIsNoData(t *testing.T) {
	udpAddr, cancel := newCollapseRootServer(t, collapseChainRecords()...)
	defer cancel()

	resp := query(t, "udp", udpAddr, "www.example.com.", dns.TypeAAAA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR (NODATA), got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Fatalf("expected zero answers, got %d: %v", len(resp.Answer), resp.Answer)
	}
	if len(resp.Ns) != 1 {
		t.Fatalf("expected SOA in authority section, got %d records", len(resp.Ns))
	}
}

// (d) Direct CNAME queries follow the unified rule: an in-zone chain ends
// NODATA; an out-of-zone tail yields the synthesized CNAME.
func TestRootCollapse_DirectCNAMEQuery(t *testing.T) {
	t.Run("in-zone chain is NODATA", func(t *testing.T) {
		udpAddr, cancel := newCollapseRootServer(t, collapseChainRecords()...)
		defer cancel()

		resp := query(t, "udp", udpAddr, "www.example.com.", dns.TypeCNAME)
		if resp.Rcode != dns.RcodeSuccess {
			t.Fatalf("expected NOERROR (NODATA), got %s", dns.RcodeToString[resp.Rcode])
		}
		if len(resp.Answer) != 0 {
			t.Fatalf("expected zero answers (stored CNAME must not leak), got %d: %v", len(resp.Answer), resp.Answer)
		}
		if len(resp.Ns) != 1 {
			t.Fatalf("expected SOA in authority section, got %d records", len(resp.Ns))
		}
	})

	t.Run("out-of-zone tail synthesizes CNAME", func(t *testing.T) {
		udpAddr, cancel := newCollapseRootServer(t,
			makeCNAMERecord("www.example.com.", "lb.example.com.", 300),
			makeCNAMERecord("lb.example.com.", "pool-a.example.com.", 60),
			makeCNAMERecord("pool-a.example.com.", "cdn.external-vendor.example.org.", 600),
		)
		defer cancel()

		resp := query(t, "udp", udpAddr, "www.example.com.", dns.TypeCNAME)
		if resp.Rcode != dns.RcodeSuccess {
			t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
		}
		if len(resp.Answer) != 1 {
			t.Fatalf("expected exactly 1 answer, got %d: %v", len(resp.Answer), resp.Answer)
		}
		cn, ok := resp.Answer[0].(*dns.CNAME)
		if !ok {
			t.Fatalf("Answer[0]: got %T, want *dns.CNAME", resp.Answer[0])
		}
		if cn.Target != "cdn.external-vendor.example.org." {
			t.Errorf("target = %q, want cdn.external-vendor.example.org.", cn.Target)
		}
		if cn.Hdr.Ttl != 60 {
			t.Errorf("TTL = %d, want 60", cn.Hdr.Ttl)
		}
	})
}

// (e) Intermediate chain names stay directly queryable and their responses
// collapse under the same rule, with owner echoing the on-wire qname case.
func TestRootCollapse_IntermediateNameAlsoCollapses(t *testing.T) {
	const queryOnWire = "LB.example.COM."
	udpAddr, cancel := newCollapseRootServer(t, collapseChainRecords()...)
	defer cancel()

	resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d: %v", len(resp.Answer), resp.Answer)
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.A", resp.Answer[0])
	}
	if got := a.Hdr.Name; got != queryOnWire {
		t.Errorf("owner = %q, want on-wire qname %q", got, queryOnWire)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("TTL = %d, want 60 (min of 60,600)", a.Hdr.Ttl)
	}
}

// (f) A chain starting from a wildcard-synthesized CNAME collapses the same
// way: single terminal record, owner = on-wire qname, chain-minimum TTL.
func TestRootCollapse_WildcardCNAMEStart(t *testing.T) {
	const queryOnWire = "AnYthing.example.com."
	udpAddr, cancel := newCollapseRootServer(t,
		makeCNAMERecord("*.example.com.", "pool-a.example.com.", 300),
		makeARecord("pool-a.example.com.", "192.0.2.10", 600),
	)
	defer cancel()

	resp := query(t, "udp", udpAddr, queryOnWire, dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected exactly 1 answer, got %d: %v", len(resp.Answer), resp.Answer)
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0]: got %T, want *dns.A", resp.Answer[0])
	}
	if got := a.Hdr.Name; got != queryOnWire {
		t.Errorf("owner = %q, want on-wire qname %q", got, queryOnWire)
	}
	if a.Hdr.Ttl != 300 {
		t.Errorf("TTL = %d, want 300 (min of 300,600)", a.Hdr.Ttl)
	}
}

// (f') A direct CNAME query whose chain starts from a wildcard CNAME follows
// the unified rule too (hook point 3: wildcard exact with qtype=CNAME): an
// in-zone walk-to-end is NODATA, never the stored wildcard CNAME.
func TestRootCollapse_WildcardDirectCNAMEQuery(t *testing.T) {
	// The wildcard sits one label down so it does not also cover the chain
	// tail (a wildcard covering its own tail loops and yields Tail per D3).
	udpAddr, cancel := newCollapseRootServer(t,
		makeCNAMERecord("*.w.example.com.", "pool-a.example.com.", 300),
		makeARecord("pool-a.example.com.", "192.0.2.10", 600),
	)
	defer cancel()

	resp := query(t, "udp", udpAddr, "foo.w.example.com.", dns.TypeCNAME)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR (NODATA), got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Fatalf("expected zero answers (wildcard CNAME must not leak), got %d: %v", len(resp.Answer), resp.Answer)
	}
	if len(resp.Ns) != 1 {
		t.Fatalf("expected SOA in authority section, got %d records", len(resp.Ns))
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
