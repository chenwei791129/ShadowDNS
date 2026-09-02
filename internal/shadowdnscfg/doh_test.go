package shadowdnscfg

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// validDoHYAML returns a config body with a complete, valid doh section. The
// acme section carries no email field: the ACME account is registered without a
// contact, so email is neither required nor accepted.
const validDoHYAML = `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
    account_key_file: "/var/lib/shadowdns/acme/account.key"
`

func TestLoad_DoHValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validDoHYAML), nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DoH == nil {
		t.Fatal("DoH is nil, want populated")
	}
	if cfg.DoH.Listen != "203.0.113.10:443" {
		t.Errorf("Listen = %q, want 203.0.113.10:443", cfg.DoH.Listen)
	}
	if cfg.DoH.ACME.DirectoryURL != "https://acme.example.com/dir" {
		t.Errorf("ACME.DirectoryURL = %q", cfg.DoH.ACME.DirectoryURL)
	}
	if cfg.DoH.ACME.IP != netip.MustParseAddr("203.0.113.10") {
		t.Errorf("ACME.IP = %v, want 203.0.113.10", cfg.DoH.ACME.IP)
	}
	if cfg.DoH.ACME.HTTP01Listen != "203.0.113.10:80" {
		t.Errorf("ACME.HTTP01Listen = %q", cfg.DoH.ACME.HTTP01Listen)
	}
	if cfg.DoH.ACME.AccountKeyFile != "/var/lib/shadowdns/acme/account.key" {
		t.Errorf("ACME.AccountKeyFile = %q", cfg.DoH.ACME.AccountKeyFile)
	}
}

func TestLoad_DoHAbsentYieldsNil(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
aliases:
  root.com:
    members:
      - backup.com
`), nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DoH != nil {
		t.Errorf("DoH = %+v, want nil (section absent)", cfg.DoH)
	}
}

// TestLoad_DoHMissingRequiredField asserts that omitting any required field
// fails the load with an error naming that field.
func TestLoad_DoHMissingRequiredField(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantInErr string
	}{
		{
			name: "missing listen",
			yaml: `
doh:
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "listen",
		},
		{
			name: "missing acme section",
			yaml: `
doh:
  listen: "203.0.113.10:443"
`,
			wantInErr: "acme",
		},
		{
			name: "missing acme.directory_url",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "directory_url",
		},
		{
			name: "missing acme.ip",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "ip",
		},
		{
			name: "missing acme.http01_listen",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    account_key_file: "/var/lib/shadowdns/acme/account.key"
`,
			wantInErr: "http01_listen",
		},
		{
			name: "missing acme.account_key_file",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "account_key_file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml), nil)
			if err == nil {
				t.Fatalf("Load succeeded, want error mentioning %q", tc.wantInErr)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error = %q, want it to name %q", err.Error(), tc.wantInErr)
			}
		})
	}
}

// TestLoad_DoHInvalidValues asserts malformed field values fail the load.
func TestLoad_DoHInvalidValues(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantInErr string
	}{
		{
			name: "invalid listen host:port",
			yaml: `
doh:
  listen: "not-a-host-port"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "listen",
		},
		{
			name: "invalid ip",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "not-an-ip"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "ip",
		},
		{
			name: "invalid directory_url",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "://missing-scheme"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "directory_url",
		},
		{
			name: "plaintext http directory_url rejected",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "http://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "directory_url",
		},
		{
			name: "unknown field rejected by strict decoding",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  bogus: "x"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "bogus",
		},
		{
			name: "listen with empty port",
			yaml: `
doh:
  listen: "203.0.113.10:"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "listen",
		},
		{
			name: "listen with port 0",
			yaml: `
doh:
  listen: "203.0.113.10:0"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "listen",
		},
		{
			name: "http01_listen with non-numeric unknown port",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:bogusport"
`,
			wantInErr: "http01_listen",
		},
		{
			name: "relative account_key_file rejected",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
    account_key_file: "relative/account.key"
`,
			wantInErr: "account_key_file",
		},
		{
			// The ACME account is registered without a contact, so email is not
			// a known field; strict decoding rejects it by name.
			name: "acme.email rejected as unknown field",
			yaml: `
doh:
  listen: "203.0.113.10:443"
  acme:
    email: "ops@example.com"
    directory_url: "https://acme.example.com/dir"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
`,
			wantInErr: "email",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml), nil)
			if err == nil {
				t.Fatalf("Load succeeded, want error mentioning %q", tc.wantInErr)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error = %q, want it to name %q", err.Error(), tc.wantInErr)
			}
		})
	}
}

// dohYAMLWithInitialDelay returns validDoHYAML with the given initial_delay
// value appended to its acme block verbatim (already YAML-quoted by the
// caller). It extends the shared valid body rather than restating it, so a
// future required field added to validDoHYAML propagates here instead of
// failing these cases for an unrelated reason.
func dohYAMLWithInitialDelay(value string) string {
	return validDoHYAML + "    initial_delay: " + value + "\n"
}

// TestLoad_DoHInitialDelay covers the optional acme.initial_delay field: the
// values that load successfully (with their effective duration) and the values
// that must fail the load naming the field. It is the only optional field under
// doh.acme, so absence must yield the zero delay rather than a missing-field
// error.
func TestLoad_DoHInitialDelay(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		// want is the effective delay when the load is expected to succeed.
		want time.Duration
		// wantInErr, when non-empty, are substrings the load error must carry;
		// a non-empty slice also marks the case as expected to fail.
		wantInErr []string
	}{
		{
			name: "field absent yields zero delay",
			yaml: validDoHYAML,
			want: 0,
		},
		{
			name: "empty string yields zero delay",
			yaml: dohYAMLWithInitialDelay(`""`),
			want: 0,
		},
		{
			name: "explicit 0s yields zero delay",
			yaml: dohYAMLWithInitialDelay(`"0s"`),
			want: 0,
		},
		{
			name: "30s is accepted",
			yaml: dohYAMLWithInitialDelay(`"30s"`),
			want: 30 * time.Second,
		},
		{
			// No upper bound is enforced, so a multi-minute window loads.
			name: "2m is accepted",
			yaml: dohYAMLWithInitialDelay(`"2m"`),
			want: 2 * time.Minute,
		},
		{
			name:      "negative value is rejected",
			yaml:      dohYAMLWithInitialDelay(`"-1s"`),
			wantInErr: []string{"initial_delay", "-1s"},
		},
		{
			name:      "unitless number is rejected",
			yaml:      dohYAMLWithInitialDelay(`"30"`),
			wantInErr: []string{"initial_delay", "30"},
		},
		{
			name:      "unparseable string is rejected",
			yaml:      dohYAMLWithInitialDelay(`"soon"`),
			wantInErr: []string{"initial_delay", "soon"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, tc.yaml), nil)
			if len(tc.wantInErr) > 0 {
				if err == nil {
					t.Fatalf("Load succeeded, want error mentioning %q", tc.wantInErr)
				}
				for _, want := range tc.wantInErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q, want it to contain %q", err.Error(), want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.DoH == nil {
				t.Fatal("DoH is nil, want populated")
			}
			if cfg.DoH.ACME.InitialDelay != tc.want {
				t.Errorf("ACME.InitialDelay = %v, want %v", cfg.DoH.ACME.InitialDelay, tc.want)
			}
		})
	}
}
