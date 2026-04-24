package server

import (
	"bytes"
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"
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
