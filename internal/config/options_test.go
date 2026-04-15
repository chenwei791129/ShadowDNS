package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger returns an slog.Logger that writes JSON lines to a buffer.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Test 1: Parses a standard options block and returns expected struct values for all 10 fields.
func TestParseOptions_StandardBlock(t *testing.T) {
	input := []byte(`options {
        directory       "/etc/namedb";
        geoip-directory "/usr/local/share/GeoIP/";
        listen-on       { any; };
        listen-on-v6    { none; };
        allow-transfer  {
                203.99.239.31;
                203.99.239.32;
        };
        recursion        no;
        minimal-responses yes;
        version          none;
        hostname         none;
        transfer-format  many-answers;
        pid-file         "/var/run/named/pid";
};`)

	block, end, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if end <= 0 {
		t.Errorf("endOffset should be > 0, got %d", end)
	}

	if block.Directory != "/etc/namedb" {
		t.Errorf("Directory: got %q, want %q", block.Directory, "/etc/namedb")
	}
	if block.GeoIPDirectory != "/usr/local/share/GeoIP/" {
		t.Errorf("GeoIPDirectory: got %q, want %q", block.GeoIPDirectory, "/usr/local/share/GeoIP/")
	}
	if len(block.ListenOn) != 1 || block.ListenOn[0] != "any" {
		t.Errorf("ListenOn: got %v, want [any]", block.ListenOn)
	}
	if len(block.ListenOnV6) != 1 || block.ListenOnV6[0] != "none" {
		t.Errorf("ListenOnV6: got %v, want [none]", block.ListenOnV6)
	}
	if len(block.AllowTransfer) != 2 {
		t.Fatalf("AllowTransfer: got %d entries, want 2", len(block.AllowTransfer))
	}
	if block.AllowTransfer[0] != "203.99.239.31" {
		t.Errorf("AllowTransfer[0]: got %q, want %q", block.AllowTransfer[0], "203.99.239.31")
	}
	if block.AllowTransfer[1] != "203.99.239.32" {
		t.Errorf("AllowTransfer[1]: got %q, want %q", block.AllowTransfer[1], "203.99.239.32")
	}
	if block.Recursion != false {
		t.Errorf("Recursion: got %v, want false", block.Recursion)
	}
	if block.MinimalResponses != true {
		t.Errorf("MinimalResponses: got %v, want true", block.MinimalResponses)
	}
	if block.Version != "none" {
		t.Errorf("Version: got %q, want %q", block.Version, "none")
	}
	if block.Hostname != "none" {
		t.Errorf("Hostname: got %q, want %q", block.Hostname, "none")
	}
	if block.TransferFormat != "many-answers" {
		t.Errorf("TransferFormat: got %q, want %q", block.TransferFormat, "many-answers")
	}
	if block.PidFile != "/var/run/named/pid" {
		t.Errorf("PidFile: got %q, want %q", block.PidFile, "/var/run/named/pid")
	}
}

// TestParseOptions_PidFileAbsent verifies pid-file defaults to empty when not specified.
func TestParseOptions_PidFileAbsent(t *testing.T) {
	input := []byte(`options {
        directory "/tmp";
        geoip-directory "/tmp/geoip";
};`)
	block, _, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.PidFile != "" {
		t.Errorf("PidFile: got %q, want empty string", block.PidFile)
	}
}

// Test 2: Unknown option emits warning but parsing succeeds.
func TestParseOptions_UnknownOptionEmitsWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	input := []byte(`options {
        directory "/etc/namedb";
        some-unknown-option value123;
        recursion no;
};`)

	block, _, err := ParseOptions(input, 0, "named.conf", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Directory != "/etc/namedb" {
		t.Errorf("Directory: got %q, want %q", block.Directory, "/etc/namedb")
	}
	if block.Recursion != false {
		t.Errorf("Recursion: got %v, want false", block.Recursion)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "some-unknown-option") {
		t.Errorf("expected warning to contain option name %q, got log: %s", "some-unknown-option", logOutput)
	}
	// The log should also contain a line number reference (any digit).
	if !strings.ContainsAny(logOutput, "0123456789") {
		t.Errorf("expected warning to contain a line number, got log: %s", logOutput)
	}
}

// Test 3: Malformed input returns an error mentioning path and line number.
func TestParseOptions_MalformedReturnError(t *testing.T) {
	input := []byte(`options {
        directory "/etc/namedb"
        recursion no;
};`)
	// Missing semicolon after the directory value — should error.
	_, _, err := ParseOptions(input, 0, "/etc/namedb/named.conf", slog.Default())
	if err == nil {
		t.Fatal("expected an error for malformed input, got nil")
	}
	if !strings.Contains(err.Error(), "/etc/namedb/named.conf") {
		t.Errorf("error should mention path, got: %v", err)
	}
	// Should contain a digit (line number).
	if !strings.ContainsAny(err.Error(), "0123456789") {
		t.Errorf("error should mention a line number, got: %v", err)
	}
}

// Test 4: recursion yes/no maps to bool correctly.
func TestParseOptions_Recursion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"recursion no", `options { recursion no; };`, false},
		{"recursion yes", `options { recursion yes; };`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, _, err := ParseOptions([]byte(tc.input), 0, "named.conf", slog.Default())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if block.Recursion != tc.want {
				t.Errorf("Recursion: got %v, want %v", block.Recursion, tc.want)
			}
		})
	}
}

// Test 5: version none; and version "9.x"; both work.
func TestParseOptions_Version(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"version none bareword", `options { version none; };`, "none"},
		{"version quoted string", `options { version "9.x"; };`, "9.x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, _, err := ParseOptions([]byte(tc.input), 0, "named.conf", slog.Default())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if block.Version != tc.want {
				t.Errorf("Version: got %q, want %q", block.Version, tc.want)
			}
		})
	}
}

// Test 6: Multi-IP allow-transfer produces a slice with all entries.
func TestParseOptions_AllowTransferMultipleIPs(t *testing.T) {
	input := []byte(`options {
        allow-transfer {
                10.0.0.1;
                10.0.0.2;
                10.0.0.3;
        };
};`)
	block, _, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(block.AllowTransfer) != 3 {
		t.Fatalf("AllowTransfer: got %d entries, want 3: %v", len(block.AllowTransfer), block.AllowTransfer)
	}
	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for i, w := range want {
		if block.AllowTransfer[i] != w {
			t.Errorf("AllowTransfer[%d]: got %q, want %q", i, block.AllowTransfer[i], w)
		}
	}
}

// Test 7: Comments (// # /* */) inside the block are ignored.
func TestParseOptions_CommentsAreIgnored(t *testing.T) {
	input := []byte(`options {
        // This is a line comment
        directory "/etc/namedb"; // inline comment
        # hash comment
        /* block
           comment */
        recursion no;
};`)
	block, _, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Directory != "/etc/namedb" {
		t.Errorf("Directory: got %q, want %q", block.Directory, "/etc/namedb")
	}
	if block.Recursion != false {
		t.Errorf("Recursion: got %v, want false", block.Recursion)
	}
}

// Test 8: startOffset is respected — options keyword not at position 0.
func TestParseOptions_StartOffset(t *testing.T) {
	prefix := []byte("// preamble\n")
	body := []byte(`options {
        directory "/var/named";
};`)
	input := append(prefix, body...)
	offset := len(prefix) // start of "options"
	block, end, err := ParseOptions(input, offset, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if end <= offset {
		t.Errorf("endOffset %d should be > startOffset %d", end, offset)
	}
	if block.Directory != "/var/named" {
		t.Errorf("Directory: got %q, want %q", block.Directory, "/var/named")
	}
}

// Test 9: Unmatched brace returns error with path.
func TestParseOptions_UnmatchedBrace(t *testing.T) {
	input := []byte(`options {
        directory "/etc/namedb";
        listen-on {
                any;
        /* missing closing brace for listen-on and options */
`)
	_, _, err := ParseOptions(input, 0, "test.conf", slog.Default())
	if err == nil {
		t.Fatal("expected error for unmatched brace, got nil")
	}
	if !strings.Contains(err.Error(), "test.conf") {
		t.Errorf("error should mention file path, got: %v", err)
	}
}

// TestParseOptions_NotifyYes verifies that `notify yes;` parses to a pointer
// whose target is true.
func TestParseOptions_NotifyYes(t *testing.T) {
	input := []byte(`options { notify yes; };`)
	block, _, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Notify == nil {
		t.Fatal("Notify should be non-nil for `notify yes;`")
	}
	if *block.Notify != true {
		t.Errorf("Notify: got %v, want true", *block.Notify)
	}
}

// TestParseOptions_NotifyNo verifies that `notify no;` parses to a pointer
// whose target is false.
func TestParseOptions_NotifyNo(t *testing.T) {
	input := []byte(`options { notify no; };`)
	block, _, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Notify == nil {
		t.Fatal("Notify should be non-nil for `notify no;`")
	}
	if *block.Notify != false {
		t.Errorf("Notify: got %v, want false", *block.Notify)
	}
}

// TestParseOptions_NotifyAbsent verifies that omitting the `notify` directive
// leaves Notify as nil, distinguishing "not set" from both true and false.
func TestParseOptions_NotifyAbsent(t *testing.T) {
	input := []byte(`options {
        directory "/etc/namedb";
        recursion no;
};`)
	block, _, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Notify != nil {
		t.Errorf("Notify: got %v, want nil (directive absent)", *block.Notify)
	}
}

// TestParseOptions_NotifyCaseInsensitive verifies that YES and NO (any case)
// are accepted, matching BIND's case-insensitive behavior.
func TestParseOptions_NotifyCaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"uppercase YES", `options { notify YES; };`, true},
		{"mixed-case Yes", `options { notify Yes; };`, true},
		{"uppercase NO", `options { notify NO; };`, false},
		{"mixed-case No", `options { notify No; };`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, _, err := ParseOptions([]byte(tc.input), 0, "named.conf", slog.Default())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if block.Notify == nil {
				t.Fatalf("Notify should be non-nil for %q", tc.input)
			}
			if *block.Notify != tc.want {
				t.Errorf("Notify: got %v, want %v", *block.Notify, tc.want)
			}
		})
	}
}

// TestParseOptions_NotifyInvalidValue verifies that a value other than yes/no
// produces a parse error mentioning the file path, line number, and bad value.
func TestParseOptions_NotifyInvalidValue(t *testing.T) {
	input := []byte(`options {
        directory "/etc/namedb";
        notify bogus;
};`)
	_, _, err := ParseOptions(input, 0, "/etc/namedb/named.conf", slog.Default())
	if err == nil {
		t.Fatal("expected error for invalid notify value, got nil")
	}
	if !strings.Contains(err.Error(), "/etc/namedb/named.conf") {
		t.Errorf("error should mention file path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should mention the invalid value %q, got: %v", "bogus", err)
	}
	// Line number: the `notify bogus;` line is line 3 of the input.
	if !strings.ContainsAny(err.Error(), "0123456789") {
		t.Errorf("error should mention a line number, got: %v", err)
	}
}

// Test 10: endOffset points past the closing }; of the options block.
func TestParseOptions_EndOffsetCorrect(t *testing.T) {
	suffix := []byte("\n// after options")
	body := []byte(`options { directory "/etc/namedb"; };`)
	input := append(body, suffix...)

	_, end, err := ParseOptions(input, 0, "named.conf", slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// end should be at the position where suffix starts or just after };
	if end < len(body)-1 {
		t.Errorf("endOffset %d seems too small for input of body length %d", end, len(body))
	}
	if end > len(input) {
		t.Errorf("endOffset %d exceeds total input length %d", end, len(input))
	}
}
