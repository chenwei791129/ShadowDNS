package shadowdnscfg

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "shadowdns.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoad_ValidConfigBothSections(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    - backup.com
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
  token: "secret"
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Aliases["backup.com."]; got != "root.com." {
		t.Errorf("Aliases[backup.com.] = %q, want root.com.", got)
	}
	if cfg.EphemeralAPI == nil {
		t.Fatal("EphemeralAPI is nil, want populated")
	}
	if cfg.EphemeralAPI.Listen != "127.0.0.1:8053" {
		t.Errorf("Listen = %q, want 127.0.0.1:8053", cfg.EphemeralAPI.Listen)
	}
	if cfg.EphemeralAPI.Token != "secret" {
		t.Errorf("Token = %q, want secret", cfg.EphemeralAPI.Token)
	}
	if len(cfg.EphemeralAPI.Allow) != 1 {
		t.Fatalf("Allow len = %d, want 1", len(cfg.EphemeralAPI.Allow))
	}
	if !cfg.EphemeralAPI.Allow[0].Contains(netip.MustParseAddr("10.0.0.5")) {
		t.Errorf("Allow[0] does not contain 10.0.0.5")
	}
}

func TestLoad_AliasesOnly(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com: ["backup.com"]
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EphemeralAPI != nil {
		t.Errorf("EphemeralAPI = %+v, want nil (section absent)", cfg.EphemeralAPI)
	}
	if len(cfg.Aliases) != 1 {
		t.Errorf("Aliases len = %d, want 1", len(cfg.Aliases))
	}
}

func TestLoad_AliasesOneToMany(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    - backup.com
    - mirror.com
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Aliases) != 2 {
		t.Errorf("Aliases len = %d, want 2", len(cfg.Aliases))
	}
	if cfg.Aliases["backup.com."] != "root.com." {
		t.Errorf("backup.com. -> %q, want root.com.", cfg.Aliases["backup.com."])
	}
	if cfg.Aliases["mirror.com."] != "root.com." {
		t.Errorf("mirror.com. -> %q, want root.com.", cfg.Aliases["mirror.com."])
	}
}

// Same backup repeated under the same root is silently deduplicated rather
// than rejected — a user typo but not a semantic conflict.
func TestLoad_AliasesDuplicateBackupSameRootAccepted(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    - backup.com
    - backup.com
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Aliases) != 1 {
		t.Errorf("Aliases len = %d, want 1", len(cfg.Aliases))
	}
	if cfg.Aliases["backup.com."] != "root.com." {
		t.Errorf("backup.com. -> %q, want root.com.", cfg.Aliases["backup.com."])
	}
}

func TestLoad_AliasesEmptyListAccepted(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com: []
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Aliases) != 0 {
		t.Errorf("Aliases len = %d, want 0", len(cfg.Aliases))
	}
}

func TestLoad_AliasesLegacyFormatFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  backup.com: root.com
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for legacy backup: root format (bare string instead of list)")
	}
}

func TestLoad_EphemeralAPIOnly(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Aliases) != 0 {
		t.Errorf("Aliases len = %d, want 0 (section absent)", len(cfg.Aliases))
	}
	if cfg.EphemeralAPI == nil {
		t.Fatal("EphemeralAPI is nil, want populated")
	}
}

func TestLoad_EmptyAliasesMap(t *testing.T) {
	path := writeConfig(t, `
aliases: {}
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Aliases) != 0 {
		t.Errorf("Aliases len = %d, want 0", len(cfg.Aliases))
	}
}

func TestLoad_UnknownTopLevelKeyFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com: ["backup.com"]
unknown_section:
  foo: bar
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for unknown top-level key")
	}
	if !strings.Contains(err.Error(), "unknown_section") {
		t.Errorf("error %q should name the unknown key", err.Error())
	}
}

func TestLoad_UnknownFieldInEphemeralAPIFails(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
  nope: true
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for unknown field inside ephemeral_api")
	}
}

func TestLoad_MissingFileFails(t *testing.T) {
	_, err := Load("/nonexistent/path/shadowdns.yaml", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/shadowdns.yaml") {
		t.Errorf("error %q should identify the missing path", err.Error())
	}
}

// ---------- aliases validation ----------

func TestLoad_AliasesDuplicateBackupFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root1.com: ["backup.com"]
  root2.com: ["BACKUP.com"]
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for duplicate backup across different roots")
	}
	if !strings.Contains(err.Error(), "backup.com") {
		t.Errorf("error %q should name the duplicate backup", err.Error())
	}
}

func TestLoad_AliasesSelfAliasFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  example.com: ["example.com"]
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for self-alias")
	}
	if !strings.Contains(err.Error(), "self-alias") {
		t.Errorf("error %q should mention self-alias", err.Error())
	}
}

// ---------- ephemeral_api validation ----------

func TestLoad_EphemeralAPIMissingListenFails(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  allow:
    - "10.0.0.5"
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error when listen is missing")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error %q should name the missing field", err.Error())
	}
}

func TestLoad_EphemeralAPIEmptyAllowFails(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow: []
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for empty allow list")
	}
	if !strings.Contains(err.Error(), "allow") {
		t.Errorf("error %q should name the field", err.Error())
	}
}

func TestLoad_EphemeralAPIMissingAllowFails(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error when allow is missing")
	}
}

func TestLoad_EphemeralAPIInvalidListenFails(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "not-a-host-port"
  allow:
    - "10.0.0.5"
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for invalid listen")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error %q should name the field", err.Error())
	}
}

func TestLoad_EphemeralAPIInvalidCIDRFails(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "not-an-ip"
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for invalid CIDR in allow")
	}
	if !strings.Contains(err.Error(), "not-an-ip") {
		t.Errorf("error %q should name the invalid entry", err.Error())
	}
}

func TestLoad_EphemeralAPIMixedValidIPAndCIDR(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
    - "192.168.1.0/24"
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.EphemeralAPI.Allow) != 2 {
		t.Fatalf("Allow len = %d, want 2", len(cfg.EphemeralAPI.Allow))
	}
	if !cfg.EphemeralAPI.Allow[0].Contains(netip.MustParseAddr("10.0.0.5")) {
		t.Errorf("Allow[0] should contain 10.0.0.5")
	}
	if !cfg.EphemeralAPI.Allow[1].Contains(netip.MustParseAddr("192.168.1.42")) {
		t.Errorf("Allow[1] should contain 192.168.1.42 (within /24)")
	}
}

func TestLoad_EphemeralAPIAllowUnmapsIPv4MappedIPv6(t *testing.T) {
	// parseIPOrCIDR must unmap stored prefixes so config entries in
	// IPv4-mapped IPv6 form (e.g. ::ffff:10.0.0.5) match plain IPv4
	// sources — otherwise ACL checks silently fail.
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "::ffff:10.0.0.5"
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prefix := cfg.EphemeralAPI.Allow[0]
	if !prefix.Contains(netip.MustParseAddr("10.0.0.5")) {
		t.Errorf("prefix %v should contain plain IPv4 10.0.0.5 after Unmap", prefix)
	}
}

func TestLoad_MissingAliasesLogsInfo(t *testing.T) {
	// Spec: missing aliases section yields empty map and logs an info message.
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
`)
	core, observed := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	cfg, err := Load(path, logger)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Aliases) != 0 {
		t.Errorf("Aliases len = %d, want 0", len(cfg.Aliases))
	}
	if observed.FilterMessage("config has no aliases section; starting with empty alias map").Len() == 0 {
		t.Errorf("expected info log about missing aliases section; got entries: %+v", observed.All())
	}
}

// ---------- aliases object form (rewrite_rdata_labels) ----------

func TestLoad_AliasesObjectFormFlagTrue(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    members:
      - backup.com
      - mirror.com
    rewrite_rdata_labels: true
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Aliases["backup.com."] != "root.com." {
		t.Errorf("backup.com. -> %q, want root.com.", cfg.Aliases["backup.com."])
	}
	if cfg.Aliases["mirror.com."] != "root.com." {
		t.Errorf("mirror.com. -> %q, want root.com.", cfg.Aliases["mirror.com."])
	}
	if !cfg.AliasFlags["backup.com."] {
		t.Errorf("AliasFlags[backup.com.] = false, want true")
	}
	if !cfg.AliasFlags["mirror.com."] {
		t.Errorf("AliasFlags[mirror.com.] = false, want true")
	}
}

func TestLoad_AliasesObjectFormFlagOmittedDefaultsFalse(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    members:
      - backup.com
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AliasFlags["backup.com."] {
		t.Errorf("AliasFlags[backup.com.] = true, want false (flag omitted)")
	}
}

func TestLoad_AliasesObjectAndListFormsCoexist(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root-a.net:
    - alias-a.net
  root-b.net:
    members:
      - alias-b.net
    rewrite_rdata_labels: true
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AliasFlags["alias-a.net."] {
		t.Errorf("AliasFlags[alias-a.net.] = true, want false (list form)")
	}
	if !cfg.AliasFlags["alias-b.net."] {
		t.Errorf("AliasFlags[alias-b.net.] = false, want true (object form)")
	}
}

func TestLoad_AliasesObjectMissingMembersFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    rewrite_rdata_labels: true
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error when object-form aliases entry omits members")
	}
	if !strings.Contains(err.Error(), "members") {
		t.Errorf("error %q should mention the missing members field", err.Error())
	}
}

func TestLoad_AliasesObjectUnknownFieldFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    members:
      - backup.com
    unknown_flag: true
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for unknown field inside aliases object form")
	}
	if !strings.Contains(err.Error(), "unknown_flag") {
		t.Errorf("error %q should name the unknown field", err.Error())
	}
}

func TestLoad_AliasesObjectEmptyMembersFails(t *testing.T) {
	path := writeConfig(t, `
aliases:
  root.com:
    members: []
    rewrite_rdata_labels: true
`)
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected error for empty members list in object form")
	}
}

// ---------- aliases case preservation ----------

// Mixed-case backup names in YAML must be addressable via the lowercase fold
// while their original yaml case is preserved on the Config struct so the
// alias rewrite path can emit on-wire names with operator-authored case.
func TestLoad_AliasesMixedCaseBackupPreservesOriginalCase(t *testing.T) {
	path := writeConfig(t, `
aliases:
  Root.Com:
    - Example.Com
    - MIRROR.com
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Aliases["example.com."]; got != "root.com." {
		t.Errorf("Aliases[example.com.] = %q, want root.com.", got)
	}
	if got := cfg.Aliases["mirror.com."]; got != "root.com." {
		t.Errorf("Aliases[mirror.com.] = %q, want root.com.", got)
	}
	if got := cfg.BackupOriginalCase["example.com."]; got != "Example.Com." {
		t.Errorf("BackupOriginalCase[example.com.] = %q, want %q", got, "Example.Com.")
	}
	if got := cfg.BackupOriginalCase["mirror.com."]; got != "MIRROR.com." {
		t.Errorf("BackupOriginalCase[mirror.com.] = %q, want %q", got, "MIRROR.com.")
	}
}

// Mixed-case backup must hit the AliasFlags map via lookup fold and the flag
// value must be propagated to every member regardless of case.
func TestLoad_AliasesMixedCaseBackupFlagPropagated(t *testing.T) {
	path := writeConfig(t, `
aliases:
  Root.Com:
    members:
      - Example.Com
    rewrite_rdata_labels: true
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AliasFlags["example.com."] {
		t.Errorf("AliasFlags[example.com.] = false, want true")
	}
	if got := cfg.BackupOriginalCase["example.com."]; got != "Example.Com." {
		t.Errorf("BackupOriginalCase[example.com.] = %q, want %q", got, "Example.Com.")
	}
}

func TestLoad_EphemeralAPIWithoutToken(t *testing.T) {
	path := writeConfig(t, `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EphemeralAPI.Token != "" {
		t.Errorf("Token = %q, want empty (authentication disabled)", cfg.EphemeralAPI.Token)
	}
}
