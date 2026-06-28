// Package doh implements a DNS-over-HTTPS (RFC 8484) endpoint that reuses the
// authoritative query path. An HTTP handler decodes the request into a
// *dns.Msg, wraps a synthetic dns.ResponseWriter around the HTTP connection,
// and calls the existing server.Server.ServeDNS so DoH responses are identical
// to UDP/TCP responses. TLS certificates for the configured IP address are
// obtained and renewed via ACME (see acme.go).
package doh

import (
	"net"

	"github.com/miekg/dns"
)

// protoLabel is the per-query metrics transport label for DoH-served queries,
// distinct from "udp" and "tcp".
const protoLabel = "doh"

// responseWriter is a synthetic dns.ResponseWriter that bridges an HTTP request
// to server.Server.ServeDNS. It presents the HTTP connection's peer TCP
// address as RemoteAddr — so view selection and DNS cookies behave exactly as
// for a TCP query from the same source — and captures the DNS response so the
// HTTP handler can write it as the response body.
//
// LocalAddr returns a *net.TCPAddr (never *net.UDPAddr); dnsutil.IsUDP
// therefore reports false, giving DoH the TCP-style no-truncation behavior
// required by HTTP framing. Protocol returns "doh" so the query path can label
// the request distinctly in metrics.
//
// The X-Forwarded-For / Forwarded headers are deliberately never consulted:
// the handler builds remote solely from the TCP connection's peer address, so
// a client cannot select a view by forging a header.
type responseWriter struct {
	remote *net.TCPAddr
	local  *net.TCPAddr
	// skipPack, when true, makes WriteMsg capture only the structured message
	// (msg) and skip wire serialization. The DoH JSON path serializes from msg
	// and never sends packed, so packing the response would be wasted work on
	// every JSON query.
	skipPack bool
	// msg holds the response message passed to WriteMsg (nil if only Write was
	// used); the handler reads its Answer TTLs to bound the HTTP cache header
	// and, on the JSON path, serializes it into the response body.
	msg *dns.Msg
	// packed is the wire-format response captured from WriteMsg/Write. It is
	// left empty when skipPack is set.
	packed []byte
}

// newResponseWriter builds a synthetic writer for one DoH request. remote MUST
// be non-nil (the peer address of the HTTP connection). local MUST be non-nil;
// pass a placeholder *net.TCPAddr when the server's local address is unknown.
func newResponseWriter(remote, local *net.TCPAddr) *responseWriter {
	return &responseWriter{remote: remote, local: local}
}

// newJSONResponseWriter builds a writer for a DoH JSON request: it captures the
// structured message but skips wire serialization, since the JSON path reads
// only the message.
func newJSONResponseWriter(remote, local *net.TCPAddr) *responseWriter {
	return &responseWriter{remote: remote, local: local, skipPack: true}
}

func (w *responseWriter) LocalAddr() net.Addr  { return w.local }
func (w *responseWriter) RemoteAddr() net.Addr { return w.remote }

// WriteMsg captures the response message and, unless skipPack is set, its wire
// form. ServeDNS assembles a complete message (compression on, no UDP
// truncation for the synthetic non-UDP writer) before calling this exactly once
// per query. The JSON path sets skipPack so the message is captured without the
// wasted wire serialization it never sends.
func (w *responseWriter) WriteMsg(m *dns.Msg) error {
	w.msg = m
	if w.skipPack {
		return nil
	}
	packed, err := m.Pack()
	if err != nil {
		return err
	}
	w.packed = packed
	return nil
}

// Write captures raw wire bytes. Part of the dns.ResponseWriter contract; the
// DoH path uses WriteMsg, but a defensive implementation is provided so a
// future ServeDNS code path that calls Write directly still works.
func (w *responseWriter) Write(b []byte) (int, error) {
	w.packed = append([]byte(nil), b...)
	return len(b), nil
}

func (w *responseWriter) Close() error        { return nil }
func (w *responseWriter) TsigStatus() error   { return nil }
func (w *responseWriter) TsigTimersOnly(bool) {}
func (w *responseWriter) Hijack()             {}

// Protocol identifies the transport as DoH so the query path assigns a metrics
// proto label distinct from udp/tcp. server.protoFromWriter type-asserts this.
func (w *responseWriter) Protocol() string { return protoLabel }
