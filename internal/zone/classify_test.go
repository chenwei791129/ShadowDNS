package zone

import (
	"fmt"
	"strings"
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// newObserverLogger returns a zap logger backed by an observer that tests can
// query for emitted entries.
func newObserverLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, obs := observer.New(zapcore.DebugLevel)
	return zap.New(core), obs
}

// runBackupClassify writes content as a temp zone for origin "backup.com.",
// classifies it as a backup override of "root.com.", and returns captured logs.
func runBackupClassify(t *testing.T, content string) *observer.ObservedLogs {
	t.Helper()
	path := writeZoneFile(t, content)
	z, err := ParseFile(path, "backup.com.", nil)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	logger, obs := newObserverLogger()
	Classify(z, config.AliasMap{"backup.com.": "root.com."}, logger)
	return obs
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
	logger, obs := newObserverLogger()

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
	if n := obs.FilterLevelExact(zapcore.WarnLevel).Len(); n != 0 {
		t.Errorf("expected no warnings for root zone, got %d", n)
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
	logger, obs := newObserverLogger()

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
	if obs.FilterLevelExact(zapcore.WarnLevel).Len() == 0 {
		t.Error("expected warnings for discarded records, got none")
	}
}

func TestFilterBackupRecords_SOADropIsDebug(t *testing.T) {
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN TXT "v=spf1 ~all"
`)

	soaEntries := obs.FilterMessage(msgDiscardDisallowed).
		FilterField(zap.String("type", "SOA")).All()
	if len(soaEntries) != 1 {
		t.Fatalf("SOA drop log entries: got %d, want 1", len(soaEntries))
	}
	if soaEntries[0].Level != zapcore.DebugLevel {
		t.Errorf("SOA drop log level: got %v, want DebugLevel", soaEntries[0].Level)
	}
}

func TestFilterBackupRecords_ApexNSDropIsDebug(t *testing.T) {
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.backup.com.
@ IN TXT "v=spf1 ~all"
`)

	nsEntries := obs.FilterMessage(msgDiscardDisallowed).
		FilterField(zap.String("type", "NS")).All()
	if len(nsEntries) != 1 {
		t.Fatalf("apex NS drop log entries: got %d, want 1", len(nsEntries))
	}
	entry := nsEntries[0]
	if entry.Level != zapcore.DebugLevel {
		t.Errorf("apex NS drop log level: got %v, want DebugLevel", entry.Level)
	}
	if got, want := entry.ContextMap()["owner"], "backup.com."; got != want {
		t.Errorf("apex NS drop owner field: got %q, want %q", got, want)
	}
}

func TestFilterBackupRecords_SubDelegationNSStaysWarn(t *testing.T) {
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
child IN NS ns1.child.backup.com.
@ IN TXT "v=spf1 ~all"
`)

	nsEntries := obs.FilterMessage(msgDiscardDisallowed).
		FilterField(zap.String("type", "NS")).All()
	if len(nsEntries) != 1 {
		t.Fatalf("sub-delegation NS drop log entries: got %d, want 1", len(nsEntries))
	}
	if nsEntries[0].Level != zapcore.WarnLevel {
		t.Errorf("sub-delegation NS drop log level: got %v, want WarnLevel", nsEntries[0].Level)
	}
}

func TestFilterBackupRecords_NonOverridableStaysWarn(t *testing.T) {
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN A 9.9.9.9
www IN CNAME root.com.
`)

	aEntries := obs.FilterMessage(msgDiscardDisallowed).FilterField(zap.String("type", "A")).All()
	if len(aEntries) != 1 {
		t.Fatalf("A drop log entries: got %d, want 1", len(aEntries))
	}
	if aEntries[0].Level != zapcore.WarnLevel {
		t.Errorf("A drop log level: got %v, want WarnLevel", aEntries[0].Level)
	}
	cEntries := obs.FilterMessage(msgDiscardDisallowed).FilterField(zap.String("type", "CNAME")).All()
	if len(cEntries) != 1 {
		t.Fatalf("CNAME drop log entries: got %d, want 1", len(cEntries))
	}
	if cEntries[0].Level != zapcore.WarnLevel {
		t.Errorf("CNAME drop log level: got %v, want WarnLevel", cEntries[0].Level)
	}
}

func TestFilterBackupRecords_NoSummaryWhenOnlyRFCMandatedDrops(t *testing.T) {
	// A backup zone whose only drops are SOA + apex NS (both RFC 1035 mandated
	// in zone files; both expected to be discarded at runtime) should NOT
	// produce a summary entry — the summary is reserved for actionable signal.
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.backup.com.
@ IN NS ns2.backup.com.
@ IN TXT "v=spf1 ~all"
`)

	if n := obs.FilterMessage("backup-override zone: drop summary").Len(); n != 0 {
		t.Errorf("summary entries with only SOA/apex-NS drops: got %d, want 0", n)
	}
}

func TestFilterBackupRecords_EmitsPerZoneInfoSummary(t *testing.T) {
	var buf strings.Builder
	buf.WriteString("$TTL 3600\n")
	buf.WriteString("@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )\n")
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&buf, "@ IN NS ns%d.backup.com.\n", i)
	}
	for i := 1; i <= 17; i++ {
		fmt.Fprintf(&buf, "host%d IN A 10.0.0.%d\n", i, i)
	}
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&buf, "child%d IN NS ns.child%d.backup.com.\n", i, i)
	}

	obs := runBackupClassify(t, buf.String())

	summaries := obs.FilterMessage("backup-override zone: drop summary").All()
	if len(summaries) != 1 {
		t.Fatalf("summary entries: got %d, want exactly 1", len(summaries))
	}
	entry := summaries[0]
	if entry.Level != zapcore.InfoLevel {
		t.Errorf("summary level: got %v, want InfoLevel", entry.Level)
	}

	fields := entry.ContextMap()
	if got, want := fields["zone"], "backup.com."; got != want {
		t.Errorf("summary zone field: got %v, want %v", got, want)
	}
	if got, want := fields["soa_dropped"], int64(1); got != want {
		t.Errorf("summary soa_dropped: got %v, want %v", got, want)
	}
	if got, want := fields["apex_ns_dropped"], int64(4); got != want {
		t.Errorf("summary apex_ns_dropped: got %v, want %v", got, want)
	}
	if got, want := fields["other_dropped"], int64(20); got != want {
		t.Errorf("summary other_dropped: got %v, want %v", got, want)
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
	logger, _ := newObserverLogger()

	// Must not error, just classify.
	classified := Classify(z, aliases, logger)

	if classified.Role != RoleBackupOverride {
		t.Errorf("Role: got %v, want RoleBackupOverride", classified.Role)
	}
	// SOA was the only record in backup.com. origin; it should be cleared.
	// The zone struct itself is valid.
}
