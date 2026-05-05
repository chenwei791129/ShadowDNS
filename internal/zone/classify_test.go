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
	// Per-record entries are now DEBUG-only; INFO is reserved for the
	// per-zone summary and WARN must not appear at all.
	if n := obs.FilterLevelExact(zapcore.WarnLevel).Len(); n != 0 {
		t.Errorf("expected no WARN entries for discarded records, got %d", n)
	}
	if obs.FilterMessage(msgDiscardDisallowed).FilterLevelExact(zapcore.DebugLevel).Len() == 0 {
		t.Error("expected DEBUG entries for discarded records, got none")
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

func TestFilterBackupRecords_SubDelegationNSIsDebug(t *testing.T) {
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
	if nsEntries[0].Level != zapcore.DebugLevel {
		t.Errorf("sub-delegation NS drop log level: got %v, want DebugLevel", nsEntries[0].Level)
	}
}

func TestFilterBackupRecords_NonOverridableIsDebug(t *testing.T) {
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN A 9.9.9.9
www IN CNAME root.com.
`)

	if n := obs.FilterLevelExact(zapcore.WarnLevel).Len(); n != 0 {
		t.Errorf("expected zero WARN entries (per-RR drops are DEBUG), got %d", n)
	}
	aEntries := obs.FilterMessage(msgDiscardDisallowed).FilterField(zap.String("type", "A")).All()
	if len(aEntries) != 1 {
		t.Fatalf("A drop log entries: got %d, want 1", len(aEntries))
	}
	if aEntries[0].Level != zapcore.DebugLevel {
		t.Errorf("A drop log level: got %v, want DebugLevel", aEntries[0].Level)
	}
	cEntries := obs.FilterMessage(msgDiscardDisallowed).FilterField(zap.String("type", "CNAME")).All()
	if len(cEntries) != 1 {
		t.Fatalf("CNAME drop log entries: got %d, want 1", len(cEntries))
	}
	if cEntries[0].Level != zapcore.DebugLevel {
		t.Errorf("CNAME drop log level: got %v, want DebugLevel", cEntries[0].Level)
	}
}

func TestFilterBackupRecords_SummaryEmittedForOnlyRFCMandatedDrops(t *testing.T) {
	// Spec: the per-zone INFO summary fires whenever the zone produced at
	// least one discarded record. Even a zone whose only drops are SOA +
	// apex NS (RFC 1035 mandated to be in the zone file but always discarded
	// from a backup-override) emits exactly one INFO summary. This differs
	// from the previous spec, where this case suppressed the summary.
	obs := runBackupClassify(t, `$TTL 3600
@ IN SOA ns1.backup.com. admin.backup.com. ( 1 300 120 86400 3600 )
@ IN NS ns1.backup.com.
@ IN NS ns2.backup.com.
@ IN TXT "v=spf1 ~all"
`)

	summaries := obs.FilterMessage("backup-override zone: drop summary").All()
	if len(summaries) != 1 {
		t.Fatalf("summary entries with SOA + apex-NS drops: got %d, want exactly 1", len(summaries))
	}
	if summaries[0].Level != zapcore.InfoLevel {
		t.Errorf("summary level: got %v, want InfoLevel", summaries[0].Level)
	}
	dropped, ok := summaries[0].ContextMap()["dropped"].(map[string]any)
	if !ok {
		t.Fatalf("summary dropped field missing or wrong type: %#v", summaries[0].ContextMap())
	}
	if got := dropped["SOA"]; got != int64(1) {
		t.Errorf("dropped[SOA]: got %v, want 1", got)
	}
	if got := dropped["apex_NS"]; got != int64(2) {
		t.Errorf("dropped[apex_NS]: got %v, want 2", got)
	}
}

func TestFilterBackupRecords_NoSummaryWhenZeroDrops(t *testing.T) {
	// Spec: when zero records were discarded, the per-zone INFO summary is
	// omitted. A backup zone containing only TXT/MX/SRV (no apex SOA, no
	// apex NS, no other types) should remain silent.
	//
	// Constructed synthetically (not via ParseFile) because every real
	// backup zone file must have an SOA per RFC 1035, which would itself
	// be a drop and prevent the len(dropped) == 0 path from firing. The
	// zero-drop branch is a logical contract — synthetic Zone is the
	// only way to exercise it.
	z := &Zone{Origin: "backup.com."}
	for _, line := range []string{
		`host.backup.com. 3600 IN TXT "ok"`,
		`mail.backup.com. 3600 IN MX 10 mx.backup.com.`,
		`_sip._tcp.backup.com. 3600 IN SRV 0 5 5060 sip.backup.com.`,
	} {
		rr, err := dns.NewRR(line)
		if err != nil {
			t.Fatalf("dns.NewRR(%q): %v", line, err)
		}
		z.AddRR(rr)
	}

	logger, obs := newObserverLogger()
	Classify(z, config.AliasMap{"backup.com.": "root.com."}, logger)

	if n := obs.FilterMessage("backup-override zone: drop summary").Len(); n != 0 {
		t.Errorf("summary entries with zero drops: got %d, want 0", n)
	}
	if n := obs.FilterMessage(msgDiscardDisallowed).Len(); n != 0 {
		t.Errorf("per-RR drop entries with zero drops: got %d, want 0", n)
	}
}

func TestFilterBackupRecords_EmitsHistogramSummary(t *testing.T) {
	// Spec scenario "Per-zone INFO summary is emitted whenever any drop
	// occurred": 1 SOA, 4 apex NS, 17 A, 3 sub-delegation NS produces the
	// histogram {A: 17, NS: 3, SOA: 1, apex_NS: 4} (alphabetic key order).
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

	dropped, ok := fields["dropped"].(map[string]any)
	if !ok {
		t.Fatalf("summary dropped field: got %#v, want map[string]any", fields["dropped"])
	}
	wantHistogram := map[string]int64{
		"A":       17,
		"NS":      3,
		"SOA":     1,
		"apex_NS": 4,
	}
	for k, want := range wantHistogram {
		if got, ok := dropped[k].(int64); !ok || got != want {
			t.Errorf("dropped[%q]: got %v, want %v", k, dropped[k], want)
		}
	}
	// No legacy fields.
	for _, legacy := range []string{"soa_dropped", "apex_ns_dropped", "other_dropped"} {
		if _, present := fields[legacy]; present {
			t.Errorf("legacy field %q must not appear in new summary", legacy)
		}
	}
}

func TestFilterBackupRecords_HistogramSerializationAlphabetic(t *testing.T) {
	// The summary's `dropped` field must serialize with keys in deterministic
	// alphabetic ASCII order so log-grep is stable. ASCII alphabetic order
	// for the canonical case ('A'<'N'<'S'<'a') is: A, NS, SOA, apex_NS.
	h := dropHistogram{"NS": 3, "A": 17, "SOA": 1, "apex_NS": 4}

	enc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	bufOut, err := enc.EncodeEntry(
		zapcore.Entry{Level: zapcore.InfoLevel, Message: "test"},
		[]zap.Field{zap.Object("dropped", h)},
	)
	if err != nil {
		t.Fatalf("EncodeEntry: %v", err)
	}
	out := bufOut.String()

	aIdx := strings.Index(out, `"A":`)
	nsIdx := strings.Index(out, `"NS":`)
	soaIdx := strings.Index(out, `"SOA":`)
	apexIdx := strings.Index(out, `"apex_NS":`)
	if aIdx < 0 || nsIdx < 0 || soaIdx < 0 || apexIdx < 0 {
		t.Fatalf("missing histogram keys in serialized form: A=%d NS=%d SOA=%d apex_NS=%d full=%s",
			aIdx, nsIdx, soaIdx, apexIdx, out)
	}
	if aIdx >= nsIdx || nsIdx >= soaIdx || soaIdx >= apexIdx {
		t.Errorf("histogram keys not in alphabetic order: A=%d NS=%d SOA=%d apex_NS=%d full=%s",
			aIdx, nsIdx, soaIdx, apexIdx, out)
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
