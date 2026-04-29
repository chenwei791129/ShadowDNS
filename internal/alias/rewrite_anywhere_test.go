package alias

import "testing"

// RewriteNameAnywhere drives the RDATA opt-in path for templated-CNAME
// alias groups. Every case below corresponds to a row in the
// alias-resolver spec's "Apply in-bailiwick rewrite to record values"
// example table or to a label-boundary edge case.
func TestRewriteNameAnywhere(t *testing.T) {
	const (
		root   = "root.com."
		backup = "backup.com."
	)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "in-bailiwick suffix still rewrites (subdomain of root)",
			in:   "service.root.com.",
			want: "service.backup.com.",
		},
		{
			name: "apex root rewritten to apex backup",
			in:   "root.com.",
			want: "backup.com.",
		},
		{
			name: "mid-label root sequence rewritten",
			in:   "host.root.com.cdn.example.net.",
			want: "host.backup.com.cdn.example.net.",
		},
		{
			name: "root sequence at start (preceded by name boundary) rewritten",
			in:   "root.com.cdn.example.net.",
			want: "backup.com.cdn.example.net.",
		},
		{
			name: "boundary protection: myroot.com is not a match (preceded by 'y')",
			in:   "myroot.com.foo.com.",
			want: "myroot.com.foo.com.",
		},
		{
			name: "boundary protection: prefixroot.com is not a match (preceded by 'x')",
			in:   "prefixroot.com.foo.com.",
			want: "prefixroot.com.foo.com.",
		},
		{
			name: "third-party name with no root sequence preserved",
			in:   "abc.us-east-1.elb.amazonaws.com.",
			want: "abc.us-east-1.elb.amazonaws.com.",
		},
		{
			name: "first match wins when root sequence appears multiple times",
			in:   "root.com.foo.root.com.bar.com.",
			want: "backup.com.foo.root.com.bar.com.",
		},
		{
			name: "empty input returned unchanged",
			in:   "",
			want: "",
		},
		{
			name: "uppercase input is lowercased on the no-match path",
			in:   "ABC.AMAZONAWS.COM.",
			want: "abc.amazonaws.com.",
		},
		{
			name: "uppercase input is lowercased on the match path",
			in:   "Host.ROOT.com.cdn.Example.net.",
			want: "host.backup.com.cdn.example.net.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteNameAnywhere(tc.in, root, backup)
			if got != tc.want {
				t.Errorf("RewriteNameAnywhere(%q, %q, %q) = %q, want %q",
					tc.in, root, backup, got, tc.want)
			}
		})
	}
}
