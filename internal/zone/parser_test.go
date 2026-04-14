package zone

import (
	"bytes"
	"log/slog"
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

// ---- Task 3.4: Out-of-zone owner name ----

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
	logger := slog.New(slog.NewTextHandler(&buf, nil))

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
