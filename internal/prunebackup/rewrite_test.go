package prunebackup

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestPruneFile_DropsDeleteRangesAndBlanks(t *testing.T) {
	lines := []string{
		"$TTL 300",            // 1
		"",                    // 2 — blank, dropped
		"; standalone",        // 3 — comment-only, dropped
		"@ IN A 192.0.2.1",    // 4 — in delete range
		"www IN TXT \"keep\"", // 5 — retained, trailing comment preserved
		"   ; indented",       // 6 — comment-only, dropped
		"last IN MX 10 ex.",   // 7 — retained
	}
	deletes := []LineRange{{Start: 4, End: 4}}

	out := pruneFile(lines, deletes, zap.NewNop(), "test.fwd")

	want := []string{
		"$TTL 300",
		"www IN TXT \"keep\"",
		"last IN MX 10 ex.",
	}
	if len(out) != len(want) {
		t.Fatalf("got %d lines, want %d: %#v", len(out), len(want), out)
	}
	for i, w := range want {
		if out[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, out[i], w)
		}
	}
}

func TestPruneFile_PreservesTrailingComment(t *testing.T) {
	lines := []string{
		`www   IN A   192.0.2.1 ; primary frontend`,
	}
	out := pruneFile(lines, nil, zap.NewNop(), "test.fwd")
	if len(out) != 1 || out[0] != lines[0] {
		t.Errorf("trailing comment lost; got %v", out)
	}
}

func TestPruneFile_MultiLineSOARangeRetainedContiguous(t *testing.T) {
	// SOA occupies lines 2..8; delete range is empty; expect lines 2..8
	// retained byte-for-byte in output.
	lines := []string{
		"$TTL 300", // 1
		"@ IN SOA ns1.backup.example. hostmaster.backup.example. (", // 2
		"    1           ; serial",                                  // 3
		"    300",                                                   // 4
		"    120",                                                   // 5
		"    604800",                                                // 6
		"    300",                                                   // 7
		")",                                                         // 8
		"",                                                          // 9 — blank drop
		"@   IN TXT   \"google-site-verification=BACKUP_VIEW_TH_VERIFY_TOKEN\"", // 10
	}
	out := pruneFile(lines, nil, zap.NewNop(), "test.fwd")
	want := []string{
		lines[0], lines[1], lines[2], lines[3], lines[4],
		lines[5], lines[6], lines[7], lines[9],
	}
	if len(out) != len(want) {
		t.Fatalf("got %d lines, want %d", len(out), len(want))
	}
	for i, w := range want {
		if out[i] != w {
			t.Errorf("line[%d] differs:\n got: %q\nwant: %q", i, out[i], w)
		}
	}
}

func TestPruneFile_IncludeDirectiveAlwaysRetained(t *testing.T) {
	lines := []string{
		`$include "sub/overrides"`,
		`$TTL 300`,
	}
	// Even if the caller incorrectly listed line 1 as deletable, the writer
	// drops RR lines only — directive lines pass the blank-or-comment filter
	// and have no line-based deletion triggered by RR diff, since the diff
	// engine never emits a delete for a directive.
	out := pruneFile(lines, nil, zap.NewNop(), "main.fwd")
	if len(out) != 2 || out[0] != lines[0] || out[1] != lines[1] {
		t.Errorf("directive line dropped; got %v", out)
	}
}

func TestPruneFile_GenerateEmitsInfoLog(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	lines := []string{
		`$TTL 300`,
		`$GENERATE 1-10 dyn$ IN A 192.0.2.$`,
		`@ IN TXT "ok"`,
	}
	out := pruneFile(lines, nil, logger, "gen.fwd")
	if len(out) != 3 {
		t.Errorf("want 3 output lines, got %d", len(out))
	}

	entries := logs.FilterMessageSnippet("opaque").All()
	if len(entries) != 1 {
		t.Fatalf("want 1 INFO log mentioning 'opaque', got %d: %v", len(entries), logs.All())
	}
	if !strings.Contains(entries[0].Message, "opaque directive retained") {
		t.Errorf("log message = %q, want 'opaque directive retained'", entries[0].Message)
	}
	// File path appears in fields.
	hasFile := false
	for _, f := range entries[0].Context {
		if f.Key == "file" && f.String == "gen.fwd" {
			hasFile = true
		}
	}
	if !hasFile {
		t.Errorf("log fields missing file=%q: %v", "gen.fwd", entries[0].Context)
	}
}
