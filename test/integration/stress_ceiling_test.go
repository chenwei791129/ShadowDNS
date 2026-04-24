package integration_test

import (
	"fmt"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
)

// TestStressCeiling_LEBufferBreakpoints probes how many LE-shaped TXT records
// (43-byte base64url values sharing one owner name) a single UDP response can
// carry, with compression enabled (HEAD behaviour), across the EDNS0 buffers
// realistic clients advertise. Reports wire size, TC flag, and answer count
// at each buffer — definitive answer to the "max records per LE query" question.
func TestStressCeiling_LEBufferBreakpoints(t *testing.T) {
	store := ephemeral.NewStore()
	srv, teardown := newTestServerWithEphemeral(t, store)
	defer teardown()

	const fqdn = "_acme-challenge.example.com."

	// Seed 100 distinct values so the server has headroom to serve whatever
	// the buffer will bear.
	for i := 0; i < 100; i++ {
		store.Put(fqdn, challengeValue(t), stressValueTTL)
	}

	buffers := []uint16{0 /* no EDNS → 512 */, 1232, 1500, 2800, 4096, 8192, 65535}
	t.Logf("%-10s %-6s %-4s %-8s", "buffer", "wire", "TC", "answers")
	t.Logf("%-10s %-6s %-4s %-8s", "------", "----", "--", "-------")
	for _, buf := range buffers {
		var resp *dns.Msg
		if buf == 0 {
			resp = queryEDNS(t, udpAddr(srv), fqdn, dns.TypeTXT, 0)
		} else {
			resp = queryEDNS(t, udpAddr(srv), fqdn, dns.TypeTXT, buf)
		}
		packed, err := resp.Pack()
		if err != nil {
			t.Fatalf("Pack: %v", err)
		}
		label := fmt.Sprintf("%d", buf)
		if buf == 0 {
			label = "(no EDNS)"
		}
		t.Logf("%-10s %-6d %-4v %-8d", label, len(packed), resp.Truncated, len(resp.Answer))
	}
}
