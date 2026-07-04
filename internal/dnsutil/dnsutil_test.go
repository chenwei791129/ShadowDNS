package dnsutil

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "simple no dot", in: "example.com", want: "example.com."},
		{name: "already FQDN", in: "example.com.", want: "example.com."},
		{name: "uppercase preserved", in: "EXAMPLE.COM", want: "EXAMPLE.COM."},
		{name: "mixed case with dot preserved", in: "Example.Com.", want: "Example.Com."},
		{name: "mixed case no dot preserved", in: "Example.Com", want: "Example.Com."},
		{name: "single label", in: "localhost", want: "localhost."},
		{name: "single label uppercase", in: "Localhost", want: "Localhost."},
		{name: "root dot", in: ".", want: "."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Canonicalize(tc.in)
			if got != tc.want {
				t.Errorf("Canonicalize(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLookupKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "simple no dot", in: "example.com", want: "example.com."},
		{name: "already FQDN", in: "example.com.", want: "example.com."},
		{name: "uppercase folded", in: "EXAMPLE.COM", want: "example.com."},
		{name: "mixed case with dot folded", in: "Example.Com.", want: "example.com."},
		{name: "mixed case no dot folded", in: "Example.Com", want: "example.com."},
		{name: "single label uppercase folded", in: "Localhost", want: "localhost."},
		{name: "root dot", in: ".", want: "."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := LookupKey(tc.in)
			if got != tc.want {
				t.Errorf("LookupKey(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsInZone(t *testing.T) {
	tests := []struct {
		name string
		n    string
		zone string
		want bool
	}{
		{name: "exact match", n: "example.com.", zone: "example.com.", want: true},
		{name: "exact match deep label", n: "sub.example.com.", zone: "sub.example.com.", want: true},
		{name: "subdomain", n: "www.example.com.", zone: "example.com.", want: true},
		{name: "deep subdomain", n: "a.b.example.com.", zone: "example.com.", want: true},
		{name: "partial suffix no dot", n: "badexample.com.", zone: "example.com.", want: false},
		{name: "boundary mismatch oo vs foo", n: "oo.com.", zone: "foo.com.", want: false},
		{name: "boundary mismatch barfoo vs foo", n: "barfoo.com.", zone: "foo.com.", want: false},
		{name: "parent zone not in child", n: "com.", zone: "example.com.", want: false},
		{name: "name shorter than zone", n: "o.com.", zone: "foo.com.", want: false},
		{name: "empty name", n: "", zone: "example.com.", want: false},
		{name: "empty zone with trailing dot name", n: "foo.com.", zone: "", want: true},
		{name: "different zone", n: "example.net.", zone: "example.com.", want: false},
		// Escaped dot: the "\." in the leftmost label is a within-label byte,
		// not a label boundary, so "x\.a.example.com." is a child of
		// example.com. but NOT of a.example.com.
		{name: "escaped dot not a boundary for child zone", n: "x\\.a.example.com.", zone: "a.example.com.", want: false},
		{name: "escaped dot label attributed to enclosing zone", n: "x\\.a.example.com.", zone: "example.com.", want: true},
		// Root zone follows the established byte contract (membership false
		// unless name == "."), and escaped names behave identically to
		// non-escaped ones so an escaped dot never flips root membership.
		{name: "root zone: non-escaped name not matched (byte contract)", n: "www.example.com.", zone: ".", want: false},
		{name: "root zone: escaped name matches non-escaped behavior", n: "x\\.a.example.com.", zone: ".", want: false},
		{name: "root zone: name equal to root matches", n: ".", zone: ".", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsInZone(tc.n, tc.zone)
			if got != tc.want {
				t.Errorf("IsInZone(%q, %q) = %v; want %v", tc.n, tc.zone, got, tc.want)
			}
			// For backslash-free names, the inlinable byte-suffix fast path
			// (the form alias.Detect calls directly) must agree with IsInZone.
			if !HasEscape(tc.n) {
				if bs := IsInZoneByteSuffix(tc.n, tc.zone); bs != got {
					t.Errorf("IsInZoneByteSuffix(%q, %q) = %v; want %v (must match IsInZone for backslash-free names)", tc.n, tc.zone, bs, got)
				}
			}
		})
	}
}

// TestIsInZone_FastPathMatchesLabelAware guards the backslash-gated
// optimization: for backslash-free names IsInZone takes the byte-suffix fast
// path, and this asserts that path agrees with the escaping-aware label walk
// (dns.IsSubDomain) IsInZone uses for escaped names. Confirms the two internal
// paths never diverge on the common case.
func TestIsInZone_FastPathMatchesLabelAware(t *testing.T) {
	cases := []struct {
		n    string
		zone string
	}{
		{"example.com.", "example.com."},
		{"www.example.com.", "example.com."},
		{"a.b.example.com.", "example.com."},
		{"badexample.com.", "example.com."},
		{"oo.com.", "foo.com."},
		{"com.", "example.com."},
		{"example.net.", "example.com."},
		{"a.example.com.", "a.example.com."},
	}
	for _, tc := range cases {
		t.Run(tc.n+"|"+tc.zone, func(t *testing.T) {
			fast := IsInZone(tc.n, tc.zone)
			labelAware := dns.IsSubDomain(tc.zone, tc.n)
			if fast != labelAware {
				t.Errorf("IsInZone(%q, %q)=%v disagrees with label-aware dns.IsSubDomain=%v", tc.n, tc.zone, fast, labelAware)
			}
		})
	}
}

func TestLookupKey_FastPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "root dot only", in: ".", want: "."},
		{name: "lookup form", in: "example.com.", want: "example.com."},
		{name: "no trailing dot", in: "example.com", want: "example.com."},
		{name: "mixed case with dot", in: "Example.COM.", want: "example.com."},
		{name: "non-ASCII", in: "εxample.com.", want: "εxample.com."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := LookupKey(tc.in)
			if got != tc.want {
				t.Errorf("LookupKey(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

var benchLookupKeySink string

func BenchmarkLookupKey_FastPath(b *testing.B) {
	in := "www.example.com."
	b.ReportAllocs()
	var sink string
	for range b.N {
		sink = LookupKey(in)
	}
	benchLookupKeySink = sink
}

func BenchmarkLookupKey_SlowPath(b *testing.B) {
	in := "WWW." + strings.Repeat("Example.", 1) + "com."
	b.ReportAllocs()
	var sink string
	for range b.N {
		sink = LookupKey(in)
	}
	benchLookupKeySink = sink
}

// benchIsInZoneSink defeats dead-code elimination of pure IsInZone calls.
var benchIsInZoneSink bool

// BenchmarkIsInZone covers the four hot-path branches used by alias.Detect:
// equal, subdomain match, byte-suffix match with bad boundary, and unrelated.
func BenchmarkIsInZone(b *testing.B) {
	cases := []struct {
		name string
		n    string
		zone string
	}{
		{name: "Equal", n: "example.com.", zone: "example.com."},
		{name: "Subdomain", n: "www.example.com.", zone: "example.com."},
		{name: "BoundaryMismatch", n: "badexample.com.", zone: "example.com."},
		{name: "Unrelated", n: "other.test.", zone: "example.com."},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var sink bool
			for range b.N {
				sink = IsInZone(tc.n, tc.zone)
			}
			benchIsInZoneSink = sink
		})
	}
}
