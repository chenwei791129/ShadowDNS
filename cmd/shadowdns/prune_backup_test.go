package main

import (
	"bytes"
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
	if err := os.WriteFile(cfgPath, []byte("aliases:\n  example.com:\n    - backup.example\n"), 0o644); err != nil {
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
