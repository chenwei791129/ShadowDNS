package zone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
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

// ---- Task 3.1: RFC 1035 parsing ----

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
	z, err := ParseFile(path, "root.com.")
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
	z, err := ParseFile(path, "root.com.")
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
	z, err := ParseFile(path, "root.com.")
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
	z, err := ParseFile(path, "root.com.")
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
	_, err := ParseFile(path, "root.com.")
	if err == nil {
		t.Fatal("expected error for malformed zone, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention file path %q", err.Error(), path)
	}
}

// ---- Task 3.4: Out-of-zone owner name ----

func TestParseFile_OutOfZoneOwner_Error(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
example.org. IN A 1.2.3.4
`
	path := writeZoneFile(t, content)
	_, err := ParseFile(path, "root.com.")
	if err == nil {
		t.Fatal("expected error for out-of-zone owner, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention file path %q", err.Error(), path)
	}
	if !strings.Contains(err.Error(), "out-of-zone") {
		t.Errorf("error %q does not mention out-of-zone", err.Error())
	}
	// Per zone-parser spec: "returns a fatal error citing the file and line".
	// The offending record sits on line 3 of the fixture above.
	if !strings.Contains(err.Error(), ":3:") {
		t.Errorf("error %q does not cite line 3", err.Error())
	}
}

func TestParseFile_UnknownRRType_Error(t *testing.T) {
	// Feed a record type that miekg/dns does not recognize.
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NOTAREALTYPE foobar
`
	path := writeZoneFile(t, content)
	_, err := ParseFile(path, "root.com.")
	if err == nil {
		t.Fatal("expected error for unknown RR type, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention file path %q", err.Error(), path)
	}
}
