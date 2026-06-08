package server

import (
	"testing"

	"github.com/miekg/dns"
)

// newBenchServer builds a minimal in-memory server (no metrics, no query log)
// for ServeDNS micro-benchmarks, sharing the optTestState fixture.
func newBenchServer() *Server {
	return NewServer(optTestState(), nil)
}

// benchServeDNS drives ServeDNS directly with a recordingWriter (UDP), so the
// measurement covers parse-to-Pack of the full handler path without network
// I/O. Note: attachOPT reuses the request's OPT record, overwriting its
// Hdr.Class with ednsUDPSize after the first iteration — keep the SetEdns0
// size in these benchmarks equal to ednsUDPSize (1232) so later iterations
// measure the same truncation budget as the first.
func benchServeDNS(b *testing.B, req *dns.Msg) {
	srv := newBenchServer()
	w := &recordingWriter{}

	b.ReportAllocs()
	for b.Loop() {
		srv.ServeDNS(w, req)
	}
	if len(w.Packed) == 0 {
		b.Fatal("no response written")
	}
}

// BenchmarkServeDNS_NoEDNS measures the handler path for a plain query
// without an OPT record.
func BenchmarkServeDNS_NoEDNS(b *testing.B) {
	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)
	benchServeDNS(b, req)
}

// BenchmarkServeDNS_EDNSNoCookie measures the handler path for an EDNS0
// query without a COOKIE option.
func BenchmarkServeDNS_EDNSNoCookie(b *testing.B) {
	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)
	req.SetEdns0(1232, false)
	benchServeDNS(b, req)
}

// BenchmarkServeDNS_EDNSWithCookie measures the handler path for an EDNS0
// query carrying an 8-byte client cookie. The handler reuses the request's
// OPT record for the response (replacing its options with the response
// cookie), so the client COOKIE option is restored before each iteration —
// a real server unpacks a fresh request per query.
func BenchmarkServeDNS_EDNSWithCookie(b *testing.B) {
	req := new(dns.Msg)
	req.SetQuestion("www.root.com.", dns.TypeA)
	req.SetEdns0(1232, false)
	opt := req.IsEdns0()
	cookieOpt := &dns.EDNS0_COOKIE{
		Code:   dns.EDNS0COOKIE,
		Cookie: "2464c4abcf10c957", // 8 raw bytes as hex
	}

	srv := newBenchServer()
	w := &recordingWriter{}

	b.ReportAllocs()
	for b.Loop() {
		opt.Option = append(opt.Option[:0], cookieOpt)
		srv.ServeDNS(w, req)
	}
	if len(w.Packed) == 0 {
		b.Fatal("no response written")
	}
}
