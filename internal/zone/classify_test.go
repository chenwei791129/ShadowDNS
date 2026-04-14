package zone

import (
	"context"
	"log/slog"
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/miekg/dns"
)

// testHandler is an slog.Handler that captures log records for assertions.
type testHandler struct {
	records []slog.Record
}

func (h *testHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *testHandler) WithGroup(name string) slog.Handler       { return h }

func (h *testHandler) warnCount() int {
	count := 0
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			count++
		}
	}
	return count
}

func TestClassify_RootZone_AllRecordsRetained(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.root.com. root.ns1.root.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.root.com.
@ IN A 1.2.3.4
www IN TXT "hello"
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "root.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	aliases := config.AliasMap{} // root.com. is not in the alias map
	h := &testHandler{}
	logger := slog.New(h)

	classified := Classify(z, aliases, logger)

	if classified.Role != RoleRoot {
		t.Errorf("Role: got %v, want RoleRoot", classified.Role)
	}

	// All records must be retained.
	aRRs := z.Lookup("root.com.", dns.TypeA)
	if len(aRRs) != 1 {
		t.Errorf("A record retained: got %d, want 1", len(aRRs))
	}
	txtRRs := z.Lookup("www.root.com.", dns.TypeTXT)
	if len(txtRRs) != 1 {
		t.Errorf("TXT record retained: got %d, want 1", len(txtRRs))
	}
	if h.warnCount() != 0 {
		t.Errorf("expected no warnings for root zone, got %d", h.warnCount())
	}
}

func TestClassify_BackupZone_OnlyTXTMXSRVRetained(t *testing.T) {
	content := `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.backup.com.
@ IN A 9.9.9.9
www IN CNAME root.com.
@ IN TXT "v=spf1 include:root.com ~all"
mail IN MX 10 mail.root.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "backup.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	aliases := config.AliasMap{
		"backup.com.": "root.com.",
	}
	h := &testHandler{}
	logger := slog.New(h)

	classified := Classify(z, aliases, logger)

	if classified.Role != RoleBackupOverride {
		t.Errorf("Role: got %v, want RoleBackupOverride", classified.Role)
	}

	// A and CNAME must be dropped.
	aRRs := z.Lookup("backup.com.", dns.TypeA)
	if len(aRRs) != 0 {
		t.Errorf("A record should be dropped, got %d", len(aRRs))
	}
	cRRs := z.Lookup("www.backup.com.", dns.TypeCNAME)
	if len(cRRs) != 0 {
		t.Errorf("CNAME record should be dropped, got %d", len(cRRs))
	}

	// TXT and MX must be retained.
	txtRRs := z.Lookup("backup.com.", dns.TypeTXT)
	if len(txtRRs) != 1 {
		t.Errorf("TXT record retained: got %d, want 1", len(txtRRs))
	}
	mxRRs := z.Lookup("mail.backup.com.", dns.TypeMX)
	if len(mxRRs) != 1 {
		t.Errorf("MX record retained: got %d, want 1", len(mxRRs))
	}

	// NS and SOA are dropped (not in allowed set).
	// Warnings must be logged for each discarded record.
	if h.warnCount() == 0 {
		t.Error("expected warnings for discarded records, got none")
	}
}

func TestClassify_BackupZone_EmptyOverrideSet(t *testing.T) {
	// Zone with only origin/SOA — no data records.
	content := `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.backup.com.
`
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "backup.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	aliases := config.AliasMap{
		"backup.com.": "root.com.",
	}
	h := &testHandler{}
	logger := slog.New(h)

	// Must not error, just classify.
	classified := Classify(z, aliases, logger)

	if classified.Role != RoleBackupOverride {
		t.Errorf("Role: got %v, want RoleBackupOverride", classified.Role)
	}
	// SOA was the only record in backup.com. origin; it should be cleared.
	// The zone struct itself is valid.
}
