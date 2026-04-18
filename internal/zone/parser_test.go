package zone

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/logging"
)

// writeZoneFile writes content to a temp file and returns its path.
func writeZoneFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.zone")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeZoneFile: %v", err)
	}
	return path
}

func TestParseFile_SOAMultiLine(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. (
	4230120512 ; serial
	300        ; refresh
	120        ; retry
	86400      ; expire
	3600       ; minimum
)
@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	soaRRs := z.Lookup("root.com.", dns.TypeSOA)
	if len(soaRRs) != 1 {
		t.Fatalf("expected 1 SOA record, got %d", len(soaRRs))
	}
	soa, ok := soaRRs[0].(*dns.SOA)
	if !ok {
		t.Fatal("record is not *dns.SOA")
	}
	if soa.Serial != 4230120512 {
		t.Errorf("Serial: got %d, want 4230120512", soa.Serial)
	}
	if soa.Refresh != 300 {
		t.Errorf("Refresh: got %d, want 300", soa.Refresh)
	}
	if soa.Retry != 120 {
		t.Errorf("Retry: got %d, want 120", soa.Retry)
	}
	if soa.Expire != 86400 {
		t.Errorf("Expire: got %d, want 86400", soa.Expire)
	}
	if soa.Minttl != 3600 {
		t.Errorf("Minttl: got %d, want 3600", soa.Minttl)
	}
	if soa.Ns != "ns1.root.com." {
		t.Errorf("MNAME: got %q, want %q", soa.Ns, "ns1.root.com.")
	}
	if soa.Mbox != "root.ns1.root.com." {
		t.Errorf("RNAME: got %q, want %q", soa.Mbox, "root.ns1.root.com.")
	}
}

func TestParseFile_AtShorthand_NS(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	nsRRs := z.Lookup("root.com.", dns.TypeNS)
	if len(nsRRs) != 1 {
		t.Fatalf("expected 1 NS record, got %d", len(nsRRs))
	}
	ns, ok := nsRRs[0].(*dns.NS)
	if !ok {
		t.Fatal("record is not *dns.NS")
	}
	if ns.Ns != "ns1.root.com." {
		t.Errorf("NS target: got %q, want %q", ns.Ns, "ns1.root.com.")
	}
}

func TestParseFile_BlankAndCommentLines_Skipped(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )

; this is a comment line

@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	// Only SOA and NS should be present — no phantom records from blank/comment lines.
	total := 0
	for _, rrs := range z.Records {
		total += len(rrs)
	}
	if total != 2 {
		t.Errorf("expected 2 total records (SOA + NS), got %d", total)
	}
}

func TestParseFile_CommentedOutRecord_NotEmitted(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
;@ IN A 1.2.3.4
@ IN NS ns1.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	aRRs := z.Lookup("root.com.", dns.TypeA)
	if len(aRRs) != 0 {
		t.Errorf("expected no A records, got %d", len(aRRs))
	}
}

func TestParseFile_ParseError_IncludesFilePath(t *testing.T) {
	// Intentionally malformed zone content.
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NOTAREALTYPE foobar
`
	path := writeZoneFile(t, content)
	_, err := ParseFile(path, "root.com.", nil)
	if err == nil {
		t.Fatal("expected error for malformed zone, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention file path %q", err.Error(), path)
	}
}

// TestParseFile_OutOfZoneOwner_Skipped verifies that out-of-zone records are
// silently skipped (matching BIND 9 behaviour) rather than causing a fatal
// error. The zone should load successfully without the offending record.
func TestParseFile_OutOfZoneOwner_Skipped(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.root.com.
example.org. IN A 1.2.3.4
www          IN A 192.0.2.1
`
	var buf bytes.Buffer
	cfg := logging.BaseEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder
	logger := zap.New(zapcore.NewCore(zapcore.NewConsoleEncoder(cfg), zapcore.AddSync(&buf), zapcore.DebugLevel))

	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", logger)
	if err != nil {
		t.Fatalf("ParseFile should succeed with out-of-zone record, got: %v", err)
	}

	// The out-of-zone record must NOT be in the zone.
	if rrs := z.Lookup("example.org.", dns.TypeA); len(rrs) != 0 {
		t.Errorf("out-of-zone record should be skipped, got %d records", len(rrs))
	}

	// Good records must still be loaded.
	if rrs := z.Lookup("www.root.com.", dns.TypeA); len(rrs) != 1 {
		t.Errorf("expected 1 A record for www.root.com., got %d", len(rrs))
	}

	// A warning must have been logged.
	if !strings.Contains(buf.String(), "out-of-zone") {
		t.Errorf("expected warning log with 'out-of-zone', got: %s", buf.String())
	}
}

// writeZoneWithFragment writes a CNAME fragment file plus a main zone file
// that wires it in via buildIncludeLine. Returns the path of the main file.
// The main file always starts at line 1 with $TTL and ends with the include
// line, so callers can append further malformed lines via buildIncludeLine
// when testing line-number preservation.
func writeZoneWithFragment(t *testing.T, buildIncludeLine func(fragmentPath string) string) string {
	t.Helper()
	dir := t.TempDir()

	fragmentPath := filepath.Join(dir, "fragment.zone")
	fragment := "alias IN CNAME target.example.com.\n"
	if err := os.WriteFile(fragmentPath, []byte(fragment), 0o600); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	main := `$TTL 3600
@ IN SOA ns1.example.com. root.ns1.example.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.example.com.
` + buildIncludeLine(fragmentPath) + "\n"
	mainPath := filepath.Join(dir, "main.zone")
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatalf("write main: %v", err)
	}
	return mainPath
}

// TestParseFile_QuotedInclude_DirectiveVariants exercises the four
// equivalent ways an operator may spell the $INCLUDE directive: lowercase
// quoted, uppercase quoted, bare (the pre-existing form), and quoted with
// a trailing comment. All four must load the same fragment and produce
// the same CNAME record.
func TestParseFile_QuotedInclude_DirectiveVariants(t *testing.T) {
	cases := []struct {
		name      string
		directive func(p string) string
	}{
		{"lowercase quoted", func(p string) string { return `$include "` + p + `"` }},
		{"uppercase quoted", func(p string) string { return `$INCLUDE "` + p + `"` }},
		{"bare path regression", func(p string) string { return `$include ` + p }},
		{"trailing comment", func(p string) string { return `$include "` + p + `" ; generated by zone-tool` }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mainPath := writeZoneWithFragment(t, tc.directive)

			z, err := ParseFile(mainPath, "example.com.", nil)
			if err != nil {
				t.Fatalf("ParseFile error: %v", err)
			}

			cnames := z.Lookup("alias.example.com.", dns.TypeCNAME)
			if len(cnames) != 1 {
				t.Fatalf("expected 1 CNAME for alias.example.com., got %d", len(cnames))
			}
			cname, ok := cnames[0].(*dns.CNAME)
			if !ok {
				t.Fatal("record is not *dns.CNAME")
			}
			if cname.Target != "target.example.com." {
				t.Errorf("CNAME target: got %q, want %q", cname.Target, "target.example.com.")
			}
		})
	}
}

// TestParseFile_TXTQuotedString_Unaffected verifies that quoted strings
// in TXT record rdata (which are not on a $INCLUDE line) are not touched
// by the BIND-compat rewrite layer.
func TestParseFile_TXTQuotedString_Unaffected(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.example.com. root.ns1.example.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.example.com.
@ IN TXT "v=spf1 -all"
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "example.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	txts := z.Lookup("example.com.", dns.TypeTXT)
	if len(txts) != 1 {
		t.Fatalf("expected 1 TXT record, got %d", len(txts))
	}
	txt, ok := txts[0].(*dns.TXT)
	if !ok {
		t.Fatal("record is not *dns.TXT")
	}
	if len(txt.Txt) != 1 || txt.Txt[0] != "v=spf1 -all" {
		t.Errorf("TXT value: got %v, want [\"v=spf1 -all\"]", txt.Txt)
	}
}

// TestParseFile_UnmatchedQuote_PassedThrough verifies that an opening `"`
// without a matching closing `"` on the same $INCLUDE line is left
// unchanged so that miekg surfaces the syntax error rather than the
// rewrite layer silently swallowing it.
func TestParseFile_UnmatchedQuote_PassedThrough(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.example.com. root.ns1.example.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.example.com.
$include "/no/closing/quote
`
	path := writeZoneFile(t, content)
	_, err := ParseFile(path, "example.com.", nil)
	if err == nil {
		t.Fatal("expected parse error for unmatched quote, got nil")
	}
	// miekg's scanner reports an error referencing the quote token.
	// We don't bind to its exact wording, but the line must still come
	// through to the parser unmodified.
	if !strings.Contains(err.Error(), `"`) && !strings.Contains(err.Error(), "INCLUDE") {
		t.Errorf("expected error to surface the malformed $INCLUDE line; got: %v", err)
	}
}

// TestParseFile_LineNumberPreserved verifies Decision 3: replacing the
// path-wrapping quotes with spaces (rather than deleting them) keeps the
// original line numbering intact, so error messages from miekg point at
// the right line in the operator's source file.
func TestParseFile_LineNumberPreserved(t *testing.T) {
	mainPath := writeZoneWithFragment(t, func(p string) string {
		// Two quoted includes followed by a malformed record on line 6.
		return `$include "` + p + `"
$include "` + p + `"
@ IN NOTAREALTYPE foobar`
	})

	_, err := ParseFile(mainPath, "example.com.", nil)
	if err == nil {
		t.Fatal("expected parse error on malformed line, got nil")
	}
	if !strings.Contains(err.Error(), "line: 6") {
		t.Errorf("expected error to cite line 6 of the original file; got: %v", err)
	}
}

func TestParseFile_UnknownRRType_Error(t *testing.T) {
	// Feed a record type that miekg/dns does not recognize.
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NOTAREALTYPE foobar
`
	path := writeZoneFile(t, content)
	_, err := ParseFile(path, "root.com.", nil)
	if err == nil {
		t.Fatal("expected error for unknown RR type, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention file path %q", err.Error(), path)
	}
}

// TestParseFile_WildcardOwnerName verifies that the parser correctly stores
// wildcard owner names in the zone's Records map, acting as a regression guard
// for wildcard support.
func TestParseFile_WildcardOwnerName(t *testing.T) {
	t.Run("bare wildcard A record stored under *.example.com.", func(t *testing.T) {
		// Scenario (a): bare "*" owner with A record, must be keyed as *.example.com.
		content := `$TTL 3600
@ IN SOA ns1.example.com. root.ns1.example.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.example.com.
* IN A   1.2.3.4
`
		path := writeZoneFile(t, content)
		z, err := ParseFile(path, "example.com.", nil)
		if err != nil {
			t.Fatalf("ParseFile error: %v", err)
		}

		const wantKey = "*.example.com."
		rrs := z.Records[wantKey]
		if len(rrs) != 1 {
			t.Fatalf("expected 1 record at %q, got %d", wantKey, len(rrs))
		}
		a, ok := rrs[0].(*dns.A)
		if !ok {
			t.Fatal("record is not *dns.A")
		}
		if a.A.String() != "1.2.3.4" {
			t.Errorf("A rdata: got %q, want %q", a.A.String(), "1.2.3.4")
		}
	})

	t.Run("sub-label wildcard CNAME stored under *.sub.example.com.", func(t *testing.T) {
		// Scenario (b): "*.sub" owner with CNAME record, must be keyed as *.sub.example.com.
		content := `$TTL 3600
@ IN SOA ns1.example.com. root.ns1.example.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.example.com.
*.sub IN CNAME target.
`
		path := writeZoneFile(t, content)
		z, err := ParseFile(path, "example.com.", nil)
		if err != nil {
			t.Fatalf("ParseFile error: %v", err)
		}

		const wantKey = "*.sub.example.com."
		rrs := z.Records[wantKey]
		if len(rrs) != 1 {
			t.Fatalf("expected 1 record at %q, got %d", wantKey, len(rrs))
		}
		cname, ok := rrs[0].(*dns.CNAME)
		if !ok {
			t.Fatal("record is not *dns.CNAME")
		}
		if cname.Target != "target." {
			t.Errorf("CNAME target: got %q, want %q", cname.Target, "target.")
		}
	})

	t.Run("multiple A records at same wildcard owner are all stored", func(t *testing.T) {
		// Scenario (c): two A records under the same wildcard owner must both be appended.
		content := `$TTL 3600
@ IN SOA ns1.example.com. root.ns1.example.com. ( 1 300 120 86400 3600 )
@ IN NS  ns1.example.com.
* IN A   1.2.3.4
* IN A   5.6.7.8
`
		path := writeZoneFile(t, content)
		z, err := ParseFile(path, "example.com.", nil)
		if err != nil {
			t.Fatalf("ParseFile error: %v", err)
		}

		const wantKey = "*.example.com."
		rrs := z.Records[wantKey]
		if len(rrs) != 2 {
			t.Fatalf("expected 2 records at %q, got %d", wantKey, len(rrs))
		}

		// Collect both RDATA values and verify both addresses are present.
		seen := make(map[string]bool)
		for _, rr := range rrs {
			a, ok := rr.(*dns.A)
			if !ok {
				t.Fatalf("record is not *dns.A: %T", rr)
			}
			seen[a.A.String()] = true
		}
		for _, addr := range []string{"1.2.3.4", "5.6.7.8"} {
			if !seen[addr] {
				t.Errorf("expected A record with rdata %q, not found in %v", addr, seen)
			}
		}
	})
}
