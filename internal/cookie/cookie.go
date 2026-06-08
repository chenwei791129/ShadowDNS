// Package cookie implements DNS Cookie (RFC 7873) server-cookie generation
// in the RFC 9018 interoperable format. It is the single seam for cookie
// construction: the server cookie is Version(1) + Reserved(3) +
// Timestamp(4, big-endian Unix seconds) + SipHash-2-4 hash(8), keyed with a
// 128-bit secret injected at construction time.
package cookie

import (
	"encoding/binary"
	"net/netip"

	"github.com/dchest/siphash"
)

const (
	// SecretLen is the length in bytes of the 128-bit server secret.
	SecretLen = 16
	// ClientCookieLen is the fixed length of a client cookie (RFC 7873 §4).
	ClientCookieLen = 8
	// ServerCookieLen is the length of an RFC 9018 server cookie.
	ServerCookieLen = 16
	// FullCookieLen is the length of a complete COOKIE option payload:
	// client cookie echo + server cookie.
	FullCookieLen = ClientCookieLen + ServerCookieLen

	// MinQueryCookieLen and MaxQueryCookieLen bound the raw length of a
	// full COOKIE option payload accepted from clients: an 8-byte client
	// cookie plus an 8-to-32-byte server cookie (RFC 7873 §4). A payload
	// of exactly ClientCookieLen (client-only) is also valid; anything
	// else is malformed per RFC 7873 §5.2.2.
	MinQueryCookieLen = 16
	MaxQueryCookieLen = 40

	// version is the RFC 9018 server cookie Version field value.
	version = 1
)

// ValidQueryLen reports whether n is a valid raw byte length for a COOKIE
// option payload received in a query (RFC 7873 §5.2.2).
func ValidQueryLen(n int) bool {
	return n == ClientCookieLen || (n >= MinQueryCookieLen && n <= MaxQueryCookieLen)
}

// Generator computes RFC 9018 server cookies with a fixed secret.
type Generator struct {
	// k0, k1 are the SipHash-2-4 key words, split little-endian from the
	// 128-bit secret per the SipHash reference implementation.
	k0, k1 uint64
}

// New returns a Generator keyed with the given 128-bit secret.
func New(secret [SecretLen]byte) *Generator {
	return &Generator{
		k0: binary.LittleEndian.Uint64(secret[0:8]),
		k1: binary.LittleEndian.Uint64(secret[8:16]),
	}
}

// Generate computes the complete 24-byte COOKIE option payload for a
// response: the client cookie echoed unmodified, followed by a freshly
// computed RFC 9018 server cookie. clientIP contributes 4 bytes for IPv4
// (including v4-mapped addresses) and 16 bytes for IPv6 to the hash input.
// unixTime is the current time in Unix seconds; it is truncated to the
// 32-bit Timestamp field per RFC 9018 §4.3 (serial number arithmetic).
func (g *Generator) Generate(clientCookie [ClientCookieLen]byte, clientIP netip.Addr, unixTime int64) [FullCookieLen]byte {
	var full [FullCookieLen]byte
	copy(full[0:8], clientCookie[:])
	full[8] = version
	// full[9:12] (Reserved) stay zero.
	binary.BigEndian.PutUint32(full[12:16], uint32(unixTime))

	// Hash input: Client Cookie | Version | Reserved | Timestamp | Client-IP
	// — exactly the first 16 bytes of the option payload followed by the
	// address bytes (RFC 9018 §4).
	var msg [16 + 16]byte
	copy(msg[0:16], full[0:16])
	msgLen := 16
	if clientIP.Is4() || clientIP.Is4In6() {
		ip4 := clientIP.As4()
		msgLen += copy(msg[16:], ip4[:])
	} else {
		ip16 := clientIP.As16()
		msgLen += copy(msg[16:], ip16[:])
	}

	binary.LittleEndian.PutUint64(full[16:24], siphash.Hash(g.k0, g.k1, msg[:msgLen]))
	return full
}
