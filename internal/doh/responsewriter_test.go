package doh

import (
	"bytes"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// TestResponseWriter_Contract asserts the synthetic writer presents the HTTP
// peer as RemoteAddr, is not a UDP writer (so the TCP no-truncation path
// applies), and reports the "doh" protocol label.
func TestResponseWriter_Contract(t *testing.T) {
	remote := tcpAddr("203.0.113.7:50000")
	local := tcpAddr("203.0.113.10:443")
	w := newResponseWriter(remote, local)

	if w.RemoteAddr().String() != "203.0.113.7:50000" {
		t.Errorf("RemoteAddr = %q, want 203.0.113.7:50000", w.RemoteAddr().String())
	}
	if dnsutil.IsUDP(w) {
		t.Error("IsUDP(w) = true, want false (DoH must take the TCP no-truncation path)")
	}
	if w.Protocol() != "doh" {
		t.Errorf("Protocol() = %q, want doh", w.Protocol())
	}
}

// TestResponseWriter_DrivesViewSelection proves the RemoteAddr IP — not any
// header — determines which view answers, capturing the response via the
// synthetic writer. (Spec: "View selection uses the DoH client IP".)
func TestResponseWriter_DrivesViewSelection(t *testing.T) {
	srv := newTwoViewServer(t, "www.example.com.", "203.0.113.0/24", "203.0.113.20", "198.51.100.0/24", "198.51.100.20")

	cases := []struct {
		clientIP string
		wantIP   string
	}{
		{"203.0.113.5:40000", "203.0.113.20"},
		{"198.51.100.5:40000", "198.51.100.20"},
	}
	for _, tc := range cases {
		w := newResponseWriter(tcpAddr(tc.clientIP), tcpAddr("203.0.113.10:443"))
		req := new(dns.Msg)
		req.SetQuestion("www.example.com.", dns.TypeA)
		srv.ServeDNS(w, req)

		if w.msg == nil {
			t.Fatalf("client %s: no response captured", tc.clientIP)
		}
		if len(w.msg.Answer) != 1 {
			t.Fatalf("client %s: got %d answers, want 1", tc.clientIP, len(w.msg.Answer))
		}
		a, ok := w.msg.Answer[0].(*dns.A)
		if !ok {
			t.Fatalf("client %s: answer not A: %T", tc.clientIP, w.msg.Answer[0])
		}
		if a.A.String() != tc.wantIP {
			t.Errorf("client %s: answer IP = %s, want %s", tc.clientIP, a.A.String(), tc.wantIP)
		}
	}
}

// TestResponseWriter_CapturedBytesMatchPack asserts the captured wire bytes
// equal the response message's own Pack output (the handler writes exactly
// these bytes to the HTTP body).
func TestResponseWriter_CapturedBytesMatchPack(t *testing.T) {
	srv := newAnyViewServer(t, buildRootZone("example.com.", makeA("www.example.com.", "203.0.113.30", 300)))
	w := newResponseWriter(tcpAddr("203.0.113.5:40000"), tcpAddr("203.0.113.10:443"))
	req := new(dns.Msg)
	req.SetQuestion("www.example.com.", dns.TypeA)
	srv.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("no response captured")
	}
	want, err := w.msg.Pack()
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if !bytes.Equal(w.packed, want) {
		t.Errorf("captured bytes (%d) != message Pack (%d)", len(w.packed), len(want))
	}
}
