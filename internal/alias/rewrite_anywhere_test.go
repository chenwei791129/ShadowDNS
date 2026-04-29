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
			name: "uppercase input on the no-match path preserves original case",
			in:   "ABC.AMAZONAWS.COM.",
			want: "ABC.AMAZONAWS.COM.",
		},
		{
			name: "mixed-case input on the match path preserves prefix and suffix case",
			in:   "Host.ROOT.com.cdn.Example.net.",
			want: "Host.backup.com.cdn.Example.net.",
		},
		{
			name: "mixed-case mid-label match preserves surrounding case while emitting lowercase root match",
			in:   "HoSt.RoOt.CoM.CDN.Example.NET.",
			want: "HoSt.backup.com.CDN.Example.NET.",
		},
		{
			name: "all-uppercase query under root rewritten to lowercase backup with prefix preserved",
			in:   "ROOT.COM.",
			want: "backup.com.",
		},
	}

	// Boundary tests with mixed-case backup verify the operator-authored case
	// is emitted byte-for-byte in the output.
	t.Run("mixed-case backup emitted verbatim at apex (empty prefix)", func(t *testing.T) {
		got := RewriteNameAnywhere("root.com.", root, "BackUp.Com.")
		if got != "BackUp.Com." {
			t.Errorf("got %q, want BackUp.Com.", got)
		}
	})
	t.Run("all-uppercase backup emitted verbatim with mixed-case n preserved", func(t *testing.T) {
		got := RewriteNameAnywhere("HoSt.RoOt.CoM.cdn.example.net.", root, "BACKUP.COM.")
		if got != "HoSt.BACKUP.COM.cdn.example.net." {
			t.Errorf("got %q, want HoSt.BACKUP.COM.cdn.example.net.", got)
		}
	})

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
