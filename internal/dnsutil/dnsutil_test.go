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
		{name: "uppercase", in: "EXAMPLE.COM", want: "example.com."},
		{name: "mixed case with dot", in: "Example.Com.", want: "example.com."},
		{name: "single label", in: "localhost", want: "localhost."},
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

func TestIsInZone(t *testing.T) {
	tests := []struct {
		name string
		n    string
		zone string
		want bool
	}{
		{name: "exact match", n: "example.com.", zone: "example.com.", want: true},
		{name: "subdomain", n: "www.example.com.", zone: "example.com.", want: true},
		{name: "deep subdomain", n: "a.b.example.com.", zone: "example.com.", want: true},
		{name: "different zone", n: "example.net.", zone: "example.com.", want: false},
		{name: "partial suffix no dot", n: "badexample.com.", zone: "example.com.", want: false},
		{name: "empty name", n: "", zone: "example.com.", want: false},
		{name: "name is zone apex", n: "sub.example.com.", zone: "sub.example.com.", want: true},
		{name: "parent zone not in child", n: "com.", zone: "example.com.", want: false},
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
