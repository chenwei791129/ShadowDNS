package integration_test

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
)

// TestCompression_UDPWireSizeAt48_EDNS4096 asserts the production shared-bucket
// case (48 TXT at one owner, 43-byte ACME key-auth values, EDNS0=4096) against
// the real UDP listener. Reads the raw datagram — not the client's
// Unpack→Pack re-encoding, which drops the server's compression state — and
// asserts the wire is compressed (materially smaller than ~4029 uncompressed),
// fits the budget, and preserves all 48 RRs with TC=0.
func TestCompression_UDPWireSizeAt48_EDNS4096(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	for i := 0; i < 48; i++ {
		store.Put(stressFQDN, challengeValue(t), stressValueTTL)
	}

	req := new(dns.Msg)
	req.SetQuestion(stressFQDN, dns.TypeTXT)
	req.SetEdns0(4096, false)
	reqBytes, err := req.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	conn, err := net.Dial("udp", udpAddr(srv))
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(reqBytes); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if n > 4096 {
		t.Errorf("UDP datagram size %d bytes exceeds advertised EDNS0 budget 4096", n)
	}

	// Compression lock: the uncompressed 48×TXT shared-owner response is
	// ~4029 bytes. A compressed packet on the wire should come in below 3500
	// bytes; we use 3500 as a conservative ceiling that still catches
	// "compression disabled" regressions without flaking on RDATA variance.
	// Assumes 43-byte ACME-shaped values — update this ceiling if
	// challengeValue() grows.
	const compressedCeiling = 3500
	if n >= compressedCeiling {
		t.Errorf("UDP datagram size %d bytes ≥ %d — compression likely disabled on the wire",
			n, compressedCeiling)
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if resp.Truncated {
		t.Errorf("TC=1 on 48-RR / 4096-buffer case: server should have fit everything under compression")
	}
	if len(resp.Answer) != 48 {
		t.Errorf("answer count = %d, want 48", len(resp.Answer))
	}
	t.Logf("48 TXT @ EDNS 4096: datagram=%d bytes, answers=%d, TC=%v",
		n, len(resp.Answer), resp.Truncated)
}
