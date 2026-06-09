package ratelimit

import (
	"net"
	"net/netip"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// ResponseWriter wraps a dns.ResponseWriter and applies response rate limiting
// at the single WriteMsg convergence point. It is installed at the ServeDNS
// entry point inside the metrics wrapper and outside the real writer, so every
// response path (including early error replies) flows through it.
//
// The wrapper holds only the *Limiter: the client address and imputed name are
// derived at WriteMsg time from RemoteAddr() and the response message, never
// injected at construction. This keeps it decoupled from the handler — early
// error replies (sent before the handler parses the client IP) and the zone
// origin (known only after alias detection) are both recoverable from the
// message and remote address at write time.
type ResponseWriter struct {
	dns.ResponseWriter
	limiter *Limiter
}

// NewResponseWriter wraps inner with rate limiting driven by l. A nil l makes
// the wrapper a pass-through (every response delivered unchanged).
func NewResponseWriter(inner dns.ResponseWriter, l *Limiter) *ResponseWriter {
	return &ResponseWriter{ResponseWriter: inner, limiter: l}
}

// WriteMsg applies the limiter to UDP responses and delegates to the underlying
// writer. TCP responses bypass the limiter and are delivered unchanged (TCP
// source addresses cannot be spoofed). On a Decide verdict of Drop the response
// is not written; on Slip the message is truncated (TC=1) before writing.
func (w *ResponseWriter) WriteMsg(m *dns.Msg) error {
	// Only UDP responses are rate limited; everything else delegates unchanged.
	if !dnsutil.IsUDP(w) {
		return w.ResponseWriter.WriteMsg(m)
	}
	ip, ok := clientAddr(w.RemoteAddr())
	if !ok {
		// Fail-open: without a parseable client address there is no account to
		// charge, so deliver the response unchanged rather than drop it.
		return w.ResponseWriter.WriteMsg(m)
	}

	category := ClassifyResponse(m)
	name := ImputedName(m, category)
	switch w.limiter.Decide(ip, category, name) {
	case Drop:
		return nil
	case Slip:
		truncateResponse(m)
		return w.ResponseWriter.WriteMsg(m)
	default: // Allow
		return w.ResponseWriter.WriteMsg(m)
	}
}

// clientAddr extracts the unmapped client address from a UDP remote net.Addr
// without allocating. WriteMsg only reaches this for UDP responses (TCP bypass
// returns earlier), so only the *net.UDPAddr case is handled; ok is false for
// anything else and the caller fails open.
func clientAddr(addr net.Addr) (netip.Addr, bool) {
	if a, ok := addr.(*net.UDPAddr); ok {
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			return ip.Unmap(), true
		}
	}
	return netip.Addr{}, false
}
