package shadowdnscfg

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func mapLookup(values map[string]string) envLookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func TestExpandRequiredEnvironmentVariablesInYAMLStringValues(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		env       map[string]string
		want      string
		wantError string
	}{
		{
			name:  "non-empty required variable",
			value: "${API_TOKEN}",
			env:   map[string]string{"API_TOKEN": "synthetic-secret"},
			want:  "synthetic-secret",
		},
		{
			name:      "unset required variable",
			value:     "${API_TOKEN}",
			wantError: "API_TOKEN",
		},
		{
			name:      "empty required variable",
			value:     "${API_TOKEN}",
			env:       map[string]string{"API_TOKEN": ""},
			wantError: "API_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := expandString(tt.value, 7, 11, mapLookup(tt.env))
			if tt.wantError != "" {
				if err == nil {
					t.Fatal("expandString() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tt.wantError) || !strings.Contains(err.Error(), "line 7, column 11") {
					t.Fatalf("expandString() error = %q, want variable and YAML location", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expandString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expandString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandApplyLiteralDefaultsForUnsetOrEmptyVariables(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		want  string
		value string
	}{
		{
			name:  "unset uses default",
			value: "${API_LISTEN:-127.0.0.1:8053}",
			want:  "127.0.0.1:8053",
		},
		{
			name:  "empty uses default",
			value: "${API_LISTEN:-127.0.0.1:8053}",
			env:   map[string]string{"API_LISTEN": ""},
			want:  "127.0.0.1:8053",
		},
		{
			name:  "non-empty overrides default",
			value: "${API_LISTEN:-127.0.0.1:8053}",
			env:   map[string]string{"API_LISTEN": "127.0.0.1:9053"},
			want:  "127.0.0.1:9053",
		},
		{
			name:  "default is not recursive",
			value: "${PRIMARY:-${SECONDARY}}",
			env:   map[string]string{"SECONDARY": "replacement"},
			want:  "${SECONDARY}",
		},
		{
			name:  "default left brace is literal",
			value: "${API_TOKEN:-prefix{suffix}",
			want:  "prefix{suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := expandString(tt.value, 1, 1, mapLookup(tt.env))
			if err != nil {
				t.Fatalf("expandString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expandString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandEachStringExactlyOnceFromLeftToRight(t *testing.T) {
	tests := []struct {
		name  string
		value string
		env   map[string]string
		want  string
	}{
		{
			name:  "multiple expressions",
			value: "${API_HOST}:${API_PORT}",
			env:   map[string]string{"API_HOST": "127.0.0.1", "API_PORT": "8053"},
			want:  "127.0.0.1:8053",
		},
		{
			name:  "environment value is not recursive",
			value: "${OUTER}",
			env:   map[string]string{"OUTER": "${INNER}", "INNER": "replacement"},
			want:  "${INNER}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := expandString(tt.value, 1, 1, mapLookup(tt.env))
			if err != nil {
				t.Fatalf("expandString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expandString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandYAMLValuesIsolatesEnvironmentDataFromStructure(t *testing.T) {
	const source = `
${ROOT_NAME}:
  members:
    - "${MEMBER}"
flags:
  enabled: true
  count: 3
values:
  - &shared "${SHARED_VALUE}"
  - *shared
payload: "${PAYLOAD}"
---
injected: true
`
	var root yaml.Node
	if err := yaml.NewDecoder(strings.NewReader(source)).Decode(&root); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	payload := "value: injected # comment\n---\n!tag &anchor *alias"
	refs, err := expandYAMLValues(&root, mapLookup(map[string]string{
		"ROOT_NAME":    "example.com",
		"MEMBER":       "backup.example.com",
		"SHARED_VALUE": "example.net",
		"PAYLOAD":      payload,
	}))
	if err != nil {
		t.Fatalf("expandYAMLValues: %v", err)
	}
	if len(refs) != 4 {
		t.Fatalf("references = %d, want 4 including the alias use", len(refs))
	}

	var encoded bytes.Buffer
	if err := yaml.NewEncoder(&encoded).Encode(root.Content[0]); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(encoded.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal expanded YAML: %v", err)
	}
	if _, ok := got["${ROOT_NAME}"]; !ok {
		t.Fatalf("mapping key was expanded: %#v", got)
	}
	flags := got["flags"].(map[string]any)
	if flags["enabled"] != true || flags["count"] != 3 {
		t.Errorf("non-string scalars changed: %#v", flags)
	}
	values := got["values"].([]any)
	if values[0] != "example.net" || values[1] != "example.net" {
		t.Errorf("anchor values = %#v, want expanded shared values", values)
	}
	if got["payload"] != payload {
		t.Errorf("payload = %#v, want one scalar %#v", got["payload"], payload)
	}
	if _, ok := got["injected"]; ok {
		t.Fatalf("second YAML document was included: %#v", got)
	}
}

func TestExpandYAMLValuesTracksAliasReferencesWithoutExpandingAliasNodes(t *testing.T) {
	const source = `
ephemeral_api:
  listen: &listen "${API_LISTEN}"
  allow: ["192.0.2.0/24"]
  token: *listen
`
	var root yaml.Node
	if err := yaml.NewDecoder(strings.NewReader(source)).Decode(&root); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	refs, err := expandYAMLValues(&root, mapLookup(map[string]string{"API_LISTEN": "127.0.0.1:8053"}))
	if err != nil {
		t.Fatalf("expandYAMLValues: %v", err)
	}
	paths := make(map[string]bool)
	for _, ref := range refs {
		paths[ref.Path] = true
	}
	if !paths["ephemeral_api.listen"] || !paths["ephemeral_api.token"] {
		t.Fatalf("reference paths = %#v, want anchor and alias use paths", paths)
	}
}

func TestExpandYAMLValuesPreservesStrictDecodeAndSemanticValidation(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		env     map[string]string
		wantErr string
	}{
		{
			name: "unknown top-level field",
			source: `
ephemeral_api:
  listen: "${API_LISTEN}"
  allow: ["192.0.2.0/24"]
unknown_section: true
`,
			env:     map[string]string{"API_LISTEN": "127.0.0.1:8053"},
			wantErr: "unknown_section",
		},
		{
			name: "expanded invalid CIDR",
			source: `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow: ["${ALLOW_PREFIX}"]
`,
			env:     map[string]string{"ALLOW_PREFIX": "not-a-prefix"},
			wantErr: "expanded configuration validation failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadBytes([]byte(tt.source), mapLookup(tt.env), nil)
			if err == nil {
				t.Fatal("loadBytes() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadBytes() error = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestExpandPreventsEnvironmentValueDisclosure(t *testing.T) {
	values := []string{
		"Synthetic-Raw-Secret",
		`quoted-"secret"-\\value`,
		"multiline-secret\nsecond-line",
	}
	for _, secret := range values {
		t.Run(strings.ReplaceAll(secret, "\n", "-newline-"), func(t *testing.T) {
			source := `
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow: ["${ALLOW_PREFIX}"]
`
			_, err := loadBytes([]byte(source), mapLookup(map[string]string{"ALLOW_PREFIX": secret}), nil)
			if err == nil {
				t.Fatal("loadBytes() error = nil, want validation error")
			}
			message := err.Error()
			if !strings.Contains(message, "ALLOW_PREFIX") || !strings.Contains(message, "ephemeral_api.allow[0]") {
				t.Errorf("error = %q, want safe variable name and YAML path", message)
			}
			for _, forbidden := range []string{secret, strings.ToLower(secret), strings.ReplaceAll(secret, "\n", `\\n`), "invalid IP or CIDR"} {
				if forbidden != "" && strings.Contains(message, forbidden) {
					t.Errorf("error %q discloses %q", message, forbidden)
				}
			}
		})
	}
}

func TestExpandPreventsNormalizedAliasValueDisclosure(t *testing.T) {
	const secret = "Sensitive.Backup.Example.Com."
	source := `
aliases:
  example.com:
    members: ["${BACKUP_NAME}"]
  example.net:
    members: ["sensitive.backup.example.com"]
`
	_, err := loadBytes([]byte(source), mapLookup(map[string]string{"BACKUP_NAME": secret}), nil)
	if err == nil {
		t.Fatal("loadBytes() error = nil, want duplicate alias error")
	}
	message := err.Error()
	if !strings.Contains(message, "BACKUP_NAME") || !strings.Contains(message, "aliases.example.com.members[0]") {
		t.Errorf("error = %q, want safe variable name and YAML path", message)
	}
	for _, forbidden := range []string{secret, "sensitive.backup.example.com", "duplicate backup"} {
		if strings.Contains(message, forbidden) {
			t.Errorf("error %q discloses normalized value or downstream cause %q", message, forbidden)
		}
	}
}

func TestExpandRequiredVariableErrorDoesNotDiscloseOtherEnvironmentValues(t *testing.T) {
	const otherSecret = "another-synthetic-secret"
	_, _, err := expandString("${MISSING}", 2, 4, mapLookup(map[string]string{"OTHER": otherSecret}))
	if err == nil {
		t.Fatal("expandString() error = nil, want error")
	}
	if strings.Contains(err.Error(), otherSecret) {
		t.Fatalf("error %q discloses unrelated environment value", err)
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("error %q does not name missing variable", err)
	}
}

func TestExpandYAMLReportsQuotedAndMultilineSourcePositions(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "quoted", source: "token: \"${MISSING}\"\n", want: "line 1, column 9"},
		{name: "literal block", source: "token: |\n  first\n  ${MISSING}\n", want: "line 3, column 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.NewDecoder(strings.NewReader(tt.source)).Decode(&root); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			_, _, err := expandYAMLValuesFromSource(&root, mapLookup(nil), []byte(tt.source))
			if err == nil {
				t.Fatal("expandYAMLValuesFromSource() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestExpandReportsUnicodeAwareYAMLColumn(t *testing.T) {
	_, _, err := expandString("前${MISSING}", 3, 5, mapLookup(nil))
	if err == nil {
		t.Fatal("expandString() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "line 3, column 6") {
		t.Fatalf("expandString() error = %q, want Unicode-aware YAML position", err)
	}
}

func TestExpandPreserveEscapedAndUnsupportedLiteralDollarText(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      string
		wantError string
	}{
		{name: "dollar escape", value: "cost-$$5", want: "cost-$5"},
		{name: "escaped required expression", value: "$${API_TOKEN}", want: "${API_TOKEN}"},
		{name: "escaped default expression", value: "$${API_TOKEN:-fallback}", want: "${API_TOKEN:-fallback}"},
		{name: "escaped unsupported expression", value: "$${API_TOKEN:?required}", want: "${API_TOKEN:?required}"},
		{name: "plain variable", value: "$API_TOKEN", want: "$API_TOKEN"},
		{name: "plain dollar", value: "price-$5", want: "price-$5"},
		{name: "unterminated expression", value: "${API_TOKEN", wantError: "unterminated"},
		{name: "empty name", value: "${}", wantError: "invalid environment variable name"},
		{name: "invalid leading digit", value: "${9TOKEN}", wantError: "invalid environment variable name"},
		{name: "non-ASCII name", value: "${TÖKEN}", wantError: "invalid environment variable name"},
		{name: "invalid name character", value: "${API-TOKEN}", wantError: "unsupported environment expression"},
		{name: "unsupported error operator", value: "${API_TOKEN:?required}", wantError: "unsupported environment expression"},
		{name: "unsupported dash operator", value: "${API_TOKEN-default}", wantError: "unsupported environment expression"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := expandString(tt.value, 3, 5, mapLookup(map[string]string{"API_TOKEN": "synthetic-secret"}))
			if tt.wantError != "" {
				if err == nil {
					t.Fatal("expandString() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tt.wantError) || !strings.Contains(err.Error(), "line 3, column 5") {
					t.Fatalf("expandString() error = %q, want %q and YAML location", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("expandString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expandString() = %q, want %q", got, tt.want)
			}
		})
	}
}
