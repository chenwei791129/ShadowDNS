package dnsutil

import "testing"

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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsInZone(tc.n, tc.zone)
			if got != tc.want {
				t.Errorf("IsInZone(%q, %q) = %v; want %v", tc.n, tc.zone, got, tc.want)
			}
		})
	}
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
