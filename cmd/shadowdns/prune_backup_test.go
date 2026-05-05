package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestPruneBackupCmd_HelpListsFlags(t *testing.T) {
	cmd := newPruneBackupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help should not error: %v", err)
	}
	help := buf.String()
	for _, want := range []string{"--named-conf", "--config", "--apply"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing flag %s:\n%s", want, help)
		}
	}
}

func TestPruneBackupCmd_MissingRequiredFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		miss string
	}{
		{"no flags", []string{}, "named-conf"},
		{"only named-conf", []string{"--named-conf", "/tmp/fake"}, "config"},
		{"only config", []string{"--config", "/tmp/fake"}, "named-conf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newPruneBackupCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("want error for missing required flag, got nil")
			}
			if !strings.Contains(err.Error(), tc.miss) {
				t.Errorf("error %q missing mention of %q", err.Error(), tc.miss)
			}
		})
	}
}

func TestRunPruneBackup_DryRunAndApply(t *testing.T) {
	dir := t.TempDir()

	// Minimal named.conf + one view with a root and a backup zone.
	namedConf := filepath.Join(dir, "named.conf")
	namedConfBody := `options {
    directory "` + dir + `";
};

view "default" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "` + dir + `/root.fwd";
    };
    zone "backup.example" {
        type master;
        file "` + dir + `/backup.fwd";
    };
};
`
	if err := os.WriteFile(namedConf, []byte(namedConfBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "shadowdns.yaml")
	cfgBody := `aliases:
  example.com:
    members:
      - backup.example
`
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	rootBody := `$TTL 600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. 1 600 120 604800 300
@ IN NS  ns1.example.com.
@ IN MX  10 shared.example.net.
`
	if err := os.WriteFile(filepath.Join(dir, "root.fwd"), []byte(rootBody), 0o644); err != nil {
		t.Fatal(err)
	}

	backupBody := `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
@ IN NS  ns1.backup.example.
@ IN MX  10 shared.example.net.
www IN A 192.0.2.10
`
	backupPath := filepath.Join(dir, "backup.fwd")
	if err := os.WriteFile(backupPath, []byte(backupBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Dry-run should NOT modify the file but MUST print deletions.
	var out bytes.Buffer
	if err := runPruneBackup(&out, namedConf, cfgPath, false, zap.NewNop()); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	printed := out.String()
	// Both types should appear: the www A record (non-overridable → always
	// deleted) and the apex MX RRSet (equal to root → deleted under the
	// overridable-equality rule). A loose `||` would let a silent regression
	// in either branch slip through.
	if !strings.Contains(printed, " A ") {
		t.Errorf("dry-run output missing A deletion: %q", printed)
	}
	if !strings.Contains(printed, " MX ") {
		t.Errorf("dry-run output missing MX deletion: %q", printed)
	}
	orig, _ := os.ReadFile(backupPath)
	if string(orig) != backupBody {
		t.Errorf("dry-run modified file: %q", orig)
	}
	if _, err := os.Stat(backupPath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("dry-run created .bak")
	}

	// --apply should write and create .bak.
	out.Reset()
	if err := runPruneBackup(&out, namedConf, cfgPath, true, zap.NewNop()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	bak, err := os.ReadFile(backupPath + ".bak")
	if err != nil || string(bak) != backupBody {
		t.Errorf(".bak missing or wrong: err=%v content=%q", err, bak)
	}
	pruned, _ := os.ReadFile(backupPath)
	if strings.Contains(string(pruned), "www") {
		t.Errorf("pruned file still contains www A: %q", pruned)
	}
	if !strings.Contains(string(pruned), "SOA") {
		t.Errorf("pruned file lost SOA: %q", pruned)
	}
}

// TestRunPruneBackup_TwoPairsStreamInOrder pins the two-level dry-run order:
// pairs in ascending (view, backup origin) order, and within each pair lines
// in ascending (file, start-line). The backup files use deliberately
// reversed lexical paths (z9.fwd comes before z1.fwd in pair order even
// though z1 < z9 alphabetically) so a global file:line sort would emit
// z1.fwd first — that would catch a regression to the pre-streaming
// "accumulate then global-sort" model.
func TestRunPruneBackup_TwoPairsStreamInOrder(t *testing.T) {
	dir := t.TempDir()
	zonesDir := filepath.Join(dir, "zones")
	if err := os.Mkdir(zonesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pair 1's backup origin "alpha.example" sorts before pair 2's
	// "beta.example", but pair 1's file (z9.fwd) sorts AFTER pair 2's
	// (z1.fwd). Per-pair streaming → alpha (z9) lines first.
	pair1File := filepath.Join(zonesDir, "z9.fwd")
	pair2File := filepath.Join(zonesDir, "z1.fwd")
	rootFile := filepath.Join(dir, "root.fwd")

	rootBody := `$TTL 600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. 1 600 120 604800 300
@ IN NS  ns1.example.com.
@ IN MX  10 shared.example.net.
`
	if err := os.WriteFile(rootFile, []byte(rootBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two non-overridable A records per backup so each pair contributes
	// multiple deletions for within-pair ordering verification.
	pair1Body := `$TTL 300
$ORIGIN alpha.example.
@ IN SOA ns1.alpha.example. hostmaster.alpha.example. 1 300 120 604800 300
@ IN NS  ns1.alpha.example.
www IN A 192.0.2.10
api IN A 192.0.2.11
`
	if err := os.WriteFile(pair1File, []byte(pair1Body), 0o644); err != nil {
		t.Fatal(err)
	}

	pair2Body := `$TTL 300
$ORIGIN beta.example.
@ IN SOA ns1.beta.example. hostmaster.beta.example. 1 300 120 604800 300
@ IN NS  ns1.beta.example.
www IN A 192.0.2.20
api IN A 192.0.2.21
`
	if err := os.WriteFile(pair2File, []byte(pair2Body), 0o644); err != nil {
		t.Fatal(err)
	}

	namedConf := filepath.Join(dir, "named.conf")
	namedBody := `options {
    directory "` + dir + `";
};

view "view-a" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "` + rootFile + `";
    };
    zone "alpha.example" {
        type master;
        file "` + pair1File + `";
    };
    zone "beta.example" {
        type master;
        file "` + pair2File + `";
    };
};
`
	if err := os.WriteFile(namedConf, []byte(namedBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "shadowdns.yaml")
	cfgBody := `aliases:
  example.com:
    members:
      - alpha.example
      - beta.example
`
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runPruneBackup(&out, namedConf, cfgPath, false, zap.NewNop()); err != nil {
		t.Fatalf("runPruneBackup: %v", err)
	}

	printed := out.String()
	idxPair1 := strings.Index(printed, pair1File+":")
	idxPair2 := strings.Index(printed, pair2File+":")
	if idxPair1 < 0 || idxPair2 < 0 {
		t.Fatalf("expected both pair files in output; got:\n%s", printed)
	}
	if idxPair1 >= idxPair2 {
		t.Errorf("pair1 (alpha → %s) should appear before pair2 (beta → %s); got:\n%s",
			pair1File, pair2File, printed)
	}

	// Pair 1's last line must precede pair 2's first line: no interleaving.
	lastPair1 := strings.LastIndex(printed, pair1File+":")
	if lastPair1 > idxPair2 {
		t.Errorf("pair1 lines interleaved with pair2 lines; pair2 starts at %d but pair1's last line is at %d:\n%s",
			idxPair2, lastPair1, printed)
	}

	// Within each pair, start-lines must be ascending. Walk the output
	// lines, partition by file, assert monotonic StartLine.
	type rec struct {
		file  string
		start int
	}
	var records []rec
	for _, line := range strings.Split(strings.TrimRight(printed, "\n"), "\n") {
		colon := strings.Index(line, ":")
		dash := strings.Index(line, "-")
		space := strings.Index(line, " ")
		if colon < 0 || dash < 0 || space < 0 || colon >= dash || dash >= space {
			t.Fatalf("malformed line: %q", line)
		}
		var start int
		if _, err := fmt.Sscanf(line[colon+1:dash], "%d", &start); err != nil {
			t.Fatalf("parse start-line in %q: %v", line, err)
		}
		records = append(records, rec{file: line[:colon], start: start})
	}
	for i := 1; i < len(records); i++ {
		if records[i].file == records[i-1].file && records[i].start < records[i-1].start {
			t.Errorf("within-pair ordering violated: %s:%d before %s:%d",
				records[i-1].file, records[i-1].start, records[i].file, records[i].start)
		}
	}
}

// TestRunPruneBackup_NoTrailingLineLost verifies the buffered writer wrapping
// stdout is flushed before runPruneBackup returns: every emitted candidate
// must reach the destination buffer, including the last line, which must
// end with a newline.
func TestRunPruneBackup_NoTrailingLineLost(t *testing.T) {
	dir := t.TempDir()
	namedConf := filepath.Join(dir, "named.conf")
	namedBody := `options {
    directory "` + dir + `";
};

view "default" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "` + dir + `/root.fwd";
    };
    zone "backup.example" {
        type master;
        file "` + dir + `/backup.fwd";
    };
};
`
	if err := os.WriteFile(namedConf, []byte(namedBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "shadowdns.yaml")
	cfgBody := `aliases:
  example.com:
    members:
      - backup.example
`
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	rootBody := `$TTL 600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. 1 600 120 604800 300
@ IN NS  ns1.example.com.
`
	if err := os.WriteFile(filepath.Join(dir, "root.fwd"), []byte(rootBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Five non-overridable A records → five deletion lines guaranteed.
	backupBody := `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
@ IN NS  ns1.backup.example.
www IN A 192.0.2.10
api IN A 192.0.2.11
db  IN A 192.0.2.12
mx1 IN A 192.0.2.13
mx2 IN A 192.0.2.14
`
	if err := os.WriteFile(filepath.Join(dir, "backup.fwd"), []byte(backupBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runPruneBackup(&out, namedConf, cfgPath, false, zap.NewNop()); err != nil {
		t.Fatalf("runPruneBackup: %v", err)
	}

	printed := out.String()
	if printed == "" {
		t.Fatalf("expected dry-run output, got empty")
	}
	if printed[len(printed)-1] != '\n' {
		t.Errorf("last byte should be newline; got %q (full output: %q)", printed[len(printed)-1], printed)
	}
	lines := strings.Split(strings.TrimRight(printed, "\n"), "\n")
	const wantLines = 5
	if len(lines) != wantLines {
		t.Errorf("expected %d candidate lines, got %d:\n%s", wantLines, len(lines), printed)
	}
	for i, l := range lines {
		if l == "" {
			t.Errorf("line %d is empty (flush dropped it?): full output:\n%s", i, printed)
		}
	}
}

// TestRunPruneBackup_ApplyWritesPerPair verifies fail-stop semantics with
// per-pair apply: pair 1's apply succeeds and lands on disk (.bak created,
// file pruned), then pair 2's apply fails (parent directory chmodded to
// read-only blocks the orig→.bak rename), runPruneBackup returns the
// error, and pair 2's file remains untouched.
func TestRunPruneBackup_ApplyWritesPerPair(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod-based unwritable directory does not enforce")
	}
	dir := t.TempDir()
	pair1Dir := filepath.Join(dir, "p1")
	pair2Dir := filepath.Join(dir, "p2")
	if err := os.Mkdir(pair1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pair2Dir, 0o755); err != nil {
		t.Fatal(err)
	}

	rootBody := `$TTL 600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. 1 600 120 604800 300
@ IN NS  ns1.example.com.
`
	rootFile := filepath.Join(dir, "root.fwd")
	if err := os.WriteFile(rootFile, []byte(rootBody), 0o644); err != nil {
		t.Fatal(err)
	}

	pair1Backup := filepath.Join(pair1Dir, "backup.fwd")
	pair1Body := `$TTL 300
$ORIGIN alpha.example.
@ IN SOA ns1.alpha.example. hostmaster.alpha.example. 1 300 120 604800 300
@ IN NS  ns1.alpha.example.
www IN A 192.0.2.10
`
	if err := os.WriteFile(pair1Backup, []byte(pair1Body), 0o644); err != nil {
		t.Fatal(err)
	}

	pair2Backup := filepath.Join(pair2Dir, "backup.fwd")
	pair2Body := `$TTL 300
$ORIGIN beta.example.
@ IN SOA ns1.beta.example. hostmaster.beta.example. 1 300 120 604800 300
@ IN NS  ns1.beta.example.
www IN A 192.0.2.20
`
	if err := os.WriteFile(pair2Backup, []byte(pair2Body), 0o644); err != nil {
		t.Fatal(err)
	}

	namedConf := filepath.Join(dir, "named.conf")
	namedBody := `options {
    directory "` + dir + `";
};

view "view-a" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "` + rootFile + `";
    };
    zone "alpha.example" {
        type master;
        file "` + pair1Backup + `";
    };
    zone "beta.example" {
        type master;
        file "` + pair2Backup + `";
    };
};
`
	if err := os.WriteFile(namedConf, []byte(namedBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "shadowdns.yaml")
	cfgBody := `aliases:
  example.com:
    members:
      - alpha.example
      - beta.example
`
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Make pair 2's directory read-only so its rename (orig → .bak) fails
	// while pair 1's apply still succeeds. Restore on cleanup so t.TempDir
	// can remove it.
	if err := os.Chmod(pair2Dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pair2Dir, 0o755) })

	var out bytes.Buffer
	err := runPruneBackup(&out, namedConf, cfgPath, true, zap.NewNop())
	if err == nil {
		t.Fatalf("expected error from pair-2 apply failure, got nil")
	}

	// Pair 1 must have written successfully: .bak present, file pruned.
	if _, statErr := os.Stat(pair1Backup + ".bak"); statErr != nil {
		t.Errorf("pair1 .bak missing (apply did not run before pair2 failure): %v", statErr)
	}
	pair1After, _ := os.ReadFile(pair1Backup)
	if strings.Contains(string(pair1After), "www") {
		t.Errorf("pair1 file not pruned: %q", pair1After)
	}

	// Pair 2 must be untouched: no .bak, original content preserved.
	if _, statErr := os.Stat(pair2Backup + ".bak"); !os.IsNotExist(statErr) {
		t.Errorf("pair2 .bak exists despite apply failure: stat err=%v", statErr)
	}
	pair2After, _ := os.ReadFile(pair2Backup)
	if string(pair2After) != pair2Body {
		t.Errorf("pair2 file mutated after failed apply:\nwant: %q\ngot:  %q", pair2Body, pair2After)
	}
}

// TestRunPruneBackup_ParseFailureOnPairKStopsRun covers spec Requirement 3
// scenario "parse failure on pair K stops the run before any later pair
// runs". Three pairs in (view, backup-origin) order: alpha (good) → beta
// (corrupted zone file) → gamma (good). Under streaming, alpha applies
// successfully, beta's PlanPair errors, gamma is never touched.
func TestRunPruneBackup_ParseFailureOnPairKStopsRun(t *testing.T) {
	dir := t.TempDir()

	rootBody := `$TTL 600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. 1 600 120 604800 300
@ IN NS  ns1.example.com.
`
	rootFile := filepath.Join(dir, "root.fwd")
	if err := os.WriteFile(rootFile, []byte(rootBody), 0o644); err != nil {
		t.Fatal(err)
	}

	alphaBackup := filepath.Join(dir, "alpha.fwd")
	alphaBody := `$TTL 300
$ORIGIN alpha.example.
@ IN SOA ns1.alpha.example. hostmaster.alpha.example. 1 300 120 604800 300
@ IN NS  ns1.alpha.example.
www IN A 192.0.2.10
`
	if err := os.WriteFile(alphaBackup, []byte(alphaBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Beta's zone file is syntactically invalid DNS — no SOA, garbage tokens.
	// loadZoneTree should fail to parse this and propagate the error.
	betaBackup := filepath.Join(dir, "beta.fwd")
	betaBody := "this is not a valid zone file $$$ @@@@ !!!\n"
	if err := os.WriteFile(betaBackup, []byte(betaBody), 0o644); err != nil {
		t.Fatal(err)
	}

	gammaBackup := filepath.Join(dir, "gamma.fwd")
	gammaBody := `$TTL 300
$ORIGIN gamma.example.
@ IN SOA ns1.gamma.example. hostmaster.gamma.example. 1 300 120 604800 300
@ IN NS  ns1.gamma.example.
www IN A 192.0.2.30
`
	if err := os.WriteFile(gammaBackup, []byte(gammaBody), 0o644); err != nil {
		t.Fatal(err)
	}

	namedConf := filepath.Join(dir, "named.conf")
	namedBody := `options {
    directory "` + dir + `";
};

view "view-a" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "` + rootFile + `";
    };
    zone "alpha.example" {
        type master;
        file "` + alphaBackup + `";
    };
    zone "beta.example" {
        type master;
        file "` + betaBackup + `";
    };
    zone "gamma.example" {
        type master;
        file "` + gammaBackup + `";
    };
};
`
	if err := os.WriteFile(namedConf, []byte(namedBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "shadowdns.yaml")
	cfgBody := `aliases:
  example.com:
    members:
      - alpha.example
      - beta.example
      - gamma.example
`
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runPruneBackup(&out, namedConf, cfgPath, true, zap.NewNop())
	if err == nil {
		t.Fatalf("expected error from beta parse failure, got nil")
	}

	// Pair alpha (1) must have applied: .bak present, file pruned.
	if _, statErr := os.Stat(alphaBackup + ".bak"); statErr != nil {
		t.Errorf("alpha .bak missing (apply did not run before beta parse failure): %v", statErr)
	}
	alphaAfter, _ := os.ReadFile(alphaBackup)
	if strings.Contains(string(alphaAfter), "www") {
		t.Errorf("alpha file not pruned: %q", alphaAfter)
	}

	// Pair gamma (3) must be untouched: no .bak, original content preserved.
	if _, statErr := os.Stat(gammaBackup + ".bak"); !os.IsNotExist(statErr) {
		t.Errorf("gamma .bak exists; later pair was processed despite earlier parse failure: stat err=%v", statErr)
	}
	gammaAfter, _ := os.ReadFile(gammaBackup)
	if string(gammaAfter) != gammaBody {
		t.Errorf("gamma file mutated after beta parse failure:\nwant: %q\ngot:  %q", gammaBody, gammaAfter)
	}
}

func TestRunPruneBackup_NoCandidatesPrintsClean(t *testing.T) {
	dir := t.TempDir()
	namedConf := filepath.Join(dir, "named.conf")
	if err := os.WriteFile(namedConf, []byte(`options { directory "`+dir+`"; };
view "default" { match-clients { any; }; };
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "shadowdns.yaml")
	if err := os.WriteFile(cfgPath, []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runPruneBackup(&out, namedConf, cfgPath, false, zap.NewNop()); err != nil {
		t.Fatalf("runPruneBackup: %v", err)
	}
	if !strings.Contains(out.String(), "no redundant records found") {
		t.Errorf("expected clean-state message, got %q", out.String())
	}
}
