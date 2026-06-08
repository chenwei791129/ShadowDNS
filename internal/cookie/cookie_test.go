package cookie

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"testing"
)

// mustDecode is a test helper converting hex strings from RFC 9018 Appendix A
// into raw bytes.
func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("invalid hex %q: %v", s, err)
	}
	return b
}

func TestGenerateRFC9018Vectors(t *testing.T) {
	// Test vectors from RFC 9018 Appendix A. The full 24-byte COOKIE option
	// content is asserted: 8-byte client cookie echo + 16-byte server cookie.
	tests := []struct {
		name         string
		secret       string
		clientCookie string
		clientIP     string
		timestamp    int64
		want         string
	}{
		{
			name:         "A.1 learning a new server cookie (IPv4)",
			secret:       "e5e973e5a6b2a43f48e7dc849e37bfcf",
			clientCookie: "2464c4abcf10c957",
			clientIP:     "198.51.100.100",
			timestamp:    1559731985,
			want:         "2464c4abcf10c957010000005cf79f111f8130c3eee29480",
		},
		{
			name:         "A.2 same client renewed server cookie (IPv4)",
			secret:       "e5e973e5a6b2a43f48e7dc849e37bfcf",
			clientCookie: "2464c4abcf10c957",
			clientIP:     "198.51.100.100",
			timestamp:    1559734385,
			want:         "2464c4abcf10c957010000005cf7a871d4a564a1442aca77",
		},
		{
			name:         "A.3 another client renewed server cookie (IPv4)",
			secret:       "e5e973e5a6b2a43f48e7dc849e37bfcf",
			clientCookie: "fc93fc62807ddb86",
			clientIP:     "203.0.113.203",
			timestamp:    1559734700,
			want:         "fc93fc62807ddb86010000005cf7a9acf73a7810aca2381e",
		},
		{
			name:         "A.4 IPv6 query with rolled over secret",
			secret:       "445536bcd2513298075a5d379663c962",
			clientCookie: "22681ab97d52c298",
			clientIP:     "2001:db8:220:1:59de:d0f4:8769:82b8",
			timestamp:    1559741961,
			want:         "22681ab97d52c298010000005cf7c609a6bb79d16625507a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var secret [SecretLen]byte
			copy(secret[:], mustDecode(t, tt.secret))
			var cc [ClientCookieLen]byte
			copy(cc[:], mustDecode(t, tt.clientCookie))

			g := New(secret)
			got := g.Generate(cc, netip.MustParseAddr(tt.clientIP), tt.timestamp)

			if want := mustDecode(t, tt.want); !bytes.Equal(got[:], want) {
				t.Errorf("Generate() = %x, want %x", got, want)
			}
		})
	}
}

func TestGenerateByteLayout(t *testing.T) {
	// Server cookie layout per RFC 9018: Version(1) + Reserved(3) +
	// Timestamp(4, big-endian) + Hash(8). The server cookie starts at
	// offset 8 of the full cookie, after the client cookie echo.
	var secret [SecretLen]byte
	copy(secret[:], mustDecode(t, "e5e973e5a6b2a43f48e7dc849e37bfcf"))
	var cc [ClientCookieLen]byte
	copy(cc[:], mustDecode(t, "2464c4abcf10c957"))

	const ts int64 = 1559731985
	got := New(secret).Generate(cc, netip.MustParseAddr("198.51.100.100"), ts)

	if !bytes.Equal(got[:8], cc[:]) {
		t.Errorf("client cookie echo = %x, want %x", got[:8], cc)
	}
	sc := got[8:]
	if sc[0] != 0x01 {
		t.Errorf("server cookie byte 0 (version) = %#02x, want 0x01", sc[0])
	}
	if sc[1] != 0 || sc[2] != 0 || sc[3] != 0 {
		t.Errorf("server cookie bytes 1-3 (reserved) = %x, want 000000", sc[1:4])
	}
	if gotTS := binary.BigEndian.Uint32(sc[4:8]); gotTS != uint32(ts) {
		t.Errorf("server cookie bytes 4-7 (timestamp) = %d, want %d", gotTS, uint32(ts))
	}
}

func BenchmarkGenerate(b *testing.B) {
	var secret [SecretLen]byte
	copy(secret[:], "0123456789abcdef")
	var cc [ClientCookieLen]byte
	copy(cc[:], "clcookie")
	g := New(secret)
	ip := netip.MustParseAddr("198.51.100.100")

	b.ReportAllocs()
	for b.Loop() {
		_ = g.Generate(cc, ip, 1559731985)
	}
}
