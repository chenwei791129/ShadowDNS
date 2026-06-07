package config

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// TestParseLogging series (tasks 1.1 + 1.2)
// ---------------------------------------------------------------------------

// TestParseLogging_ProductionShape verifies that a production-shaped logging
// block (including versions/size) parses to the expected QueryLogConfig with
// RotationIgnored set.
func TestParseLogging_ProductionShape(t *testing.T) {
	input := `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log" versions 3 size 5000m;
		severity debug;
		print-severity yes;
		print-time yes;
		print-category yes;
	};
	category queries { queries_log; };
};`

	opts := OptionsBlock{} // no directory
	cfg, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil QueryLogConfig, got nil")
	}
	if cfg.FilePath != "/var/log/shadowdns/queries.log" {
		t.Errorf("FilePath: got %q, want %q", cfg.FilePath, "/var/log/shadowdns/queries.log")
	}
	if cfg.PrintTime != "yes" {
		t.Errorf("PrintTime: got %q, want %q", cfg.PrintTime, "yes")
	}
	if !cfg.PrintCategory {
		t.Error("PrintCategory: got false, want true")
	}
	if !cfg.PrintSeverity {
		t.Error("PrintSeverity: got false, want true")
	}
	if !cfg.RotationIgnored {
		t.Error("RotationIgnored: got false, want true (versions/size were present)")
	}
}

// TestParseLogging_RelativePath verifies that a relative file path is joined
// with the options directory, matching zone file path semantics.
func TestParseLogging_RelativePath(t *testing.T) {
	input := `logging {
	channel queries_log {
		file "queries.log";
		severity debug;
	};
	category queries { queries_log; };
};`

	opts := OptionsBlock{Directory: "/etc/namedb"}
	cfg, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil QueryLogConfig, got nil")
	}
	if cfg.FilePath != "/etc/namedb/queries.log" {
		t.Errorf("FilePath: got %q, want %q", cfg.FilePath, "/etc/namedb/queries.log")
	}
}

// TestParseLogging_MultiChannel verifies that when category queries lists
// multiple channels, the first file channel is used and a warning is emitted.
func TestParseLogging_MultiChannel(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	input := `logging {
	channel chan_a {
		file "/var/log/shadowdns/chan_a.log";
		severity debug;
	};
	channel chan_b {
		file "/var/log/shadowdns/chan_b.log";
		severity debug;
	};
	category queries { chan_a; chan_b; };
};`

	opts := OptionsBlock{}
	cfg, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil QueryLogConfig, got nil")
	}
	if cfg.FilePath != "/var/log/shadowdns/chan_a.log" {
		t.Errorf("FilePath: got %q, want %q (first file channel)", cfg.FilePath, "/var/log/shadowdns/chan_a.log")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "chan_b") {
		t.Errorf("expected warning mentioning ignored channel chan_b, got log: %s", logOutput)
	}
}

// TestParseLogging_SyntaxError verifies that an unbalanced brace produces a
// fatal error mentioning the file path and a line number.
func TestParseLogging_SyntaxError(t *testing.T) {
	input := `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity debug;
	/* missing closing brace for channel and logging */
`

	opts := OptionsBlock{}
	_, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for unbalanced braces, got nil")
	}
	if !strings.Contains(err.Error(), "named.conf") {
		t.Errorf("error should mention file path, got: %v", err)
	}
	if !strings.ContainsAny(err.Error(), "0123456789") {
		t.Errorf("error should mention a line number, got: %v", err)
	}
}

// TestParseLogging_UnknownChannelParam verifies that unknown channel parameters
// emit a warning and parsing succeeds.
func TestParseLogging_UnknownChannelParam(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	input := `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity debug;
		buffered yes;
	};
	category queries { queries_log; };
};`

	opts := OptionsBlock{}
	cfg, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil QueryLogConfig, got nil")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "buffered") {
		t.Errorf("expected warning mentioning unknown param 'buffered', got log: %s", logOutput)
	}
}

// TestParseLogging_PrintTimeLocal verifies that print-time local is accepted
// and stored as "local".
func TestParseLogging_PrintTimeLocal(t *testing.T) {
	input := `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity info;
		print-time local;
	};
	category queries { queries_log; };
};`

	opts := OptionsBlock{}
	cfg, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil QueryLogConfig, got nil")
	}
	if cfg.PrintTime != "local" {
		t.Errorf("PrintTime: got %q, want %q", cfg.PrintTime, "local")
	}
}

// TestParseLogging_NonQueriesCategory verifies that non-queries categories are
// warned about and skipped.
func TestParseLogging_NonQueriesCategory(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	input := `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity debug;
	};
	category queries { queries_log; };
	category default { default_syslog; };
};`

	opts := OptionsBlock{}
	cfg, _, _, err := ParseLogging([]byte(input), 0, "named.conf", opts, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil QueryLogConfig, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestParseLogging_DisableMatrix — table-driven test mirroring the spec matrix
// ---------------------------------------------------------------------------

// The spec matrix row "(no logging{} block at all)" is owned by the dispatch
// layer, not ParseLogging — it is covered by TestLoadNamedConf_LoggingBlockParsed
// in zones_test.go, so it has no row here.
func TestParseLogging_DisableMatrix(t *testing.T) {
	tests := []struct {
		name          string
		input         string // full named.conf fragment containing the logging block
		opts          OptionsBlock
		wantEnabled   bool
		wantWarning   bool   // whether a warning log entry should be emitted
		warnSubstring string // optional: substring that must appear in warning
	}{
		{
			name: "category queries points to null",
			input: `logging {
	category queries { null; };
};`,
			wantEnabled: false,
			wantWarning: false,
		},
		{
			name: "channel declared but no category queries",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity debug;
	};
};`,
			wantEnabled: false,
			wantWarning: false,
		},
		{
			name: "category queries points to default_syslog built-in",
			input: `logging {
	category queries { default_syslog; };
};`,
			wantEnabled: false,
			wantWarning: false,
		},
		{
			name: "file channel with severity warning",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity warning;
	};
	category queries { queries_log; };
};`,
			wantEnabled:   false,
			wantWarning:   true,
			warnSubstring: "severity",
		},
		{
			name: "syslog channel user-defined non-file",
			input: `logging {
	channel q {
		syslog daemon;
	};
	category queries { q; };
};`,
			wantEnabled:   false,
			wantWarning:   true,
			warnSubstring: "file",
		},
		{
			name: "file channel with severity debug 3",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity debug 3;
	};
	category queries { queries_log; };
};`,
			wantEnabled: true,
			wantWarning: false,
		},
		{
			name: "file channel with severity dynamic",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity dynamic;
	};
	category queries { queries_log; };
};`,
			wantEnabled: true,
			wantWarning: false,
		},
		{
			name: "file channel with severity info",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity info;
	};
	category queries { queries_log; };
};`,
			wantEnabled: true,
			wantWarning: false,
		},
		{
			name: "file channel with severity notice (strict)",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity notice;
	};
	category queries { queries_log; };
};`,
			wantEnabled:   false,
			wantWarning:   true,
			warnSubstring: "severity",
		},
		{
			name: "file channel with severity error (strict)",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity error;
	};
	category queries { queries_log; };
};`,
			wantEnabled:   false,
			wantWarning:   true,
			warnSubstring: "severity",
		},
		{
			name: "file channel with severity critical (strict)",
			input: `logging {
	channel queries_log {
		file "/var/log/shadowdns/queries.log";
		severity critical;
	};
	category queries { queries_log; };
};`,
			wantEnabled:   false,
			wantWarning:   true,
			warnSubstring: "severity",
		},
		{
			name: "category queries points to default_stderr built-in",
			input: `logging {
	category queries { default_stderr; };
};`,
			wantEnabled: false,
			wantWarning: false,
		},
		{
			name: "category queries points to default_debug built-in",
			input: `logging {
	category queries { default_debug; };
};`,
			wantEnabled: false,
			wantWarning: false,
		},
		{
			name: "stderr channel user-defined non-file",
			input: `logging {
	channel q {
		stderr;
	};
	category queries { q; };
};`,
			wantEnabled:   false,
			wantWarning:   true,
			warnSubstring: "file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			cfg, _, _, err := ParseLogging([]byte(tc.input), 0, "named.conf", tc.opts, logger)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			enabled := cfg != nil
			if enabled != tc.wantEnabled {
				t.Errorf("enabled: got %v, want %v (cfg=%+v)", enabled, tc.wantEnabled, cfg)
			}

			logOutput := buf.String()
			hasWarning := strings.Contains(logOutput, "WARN") || strings.Contains(logOutput, "warn")
			if hasWarning != tc.wantWarning {
				t.Errorf("warning emitted: got %v, want %v; log: %s", hasWarning, tc.wantWarning, logOutput)
			}
			if tc.wantWarning && tc.warnSubstring != "" {
				if !strings.Contains(logOutput, tc.warnSubstring) {
					t.Errorf("warning should contain %q, got log: %s", tc.warnSubstring, logOutput)
				}
			}
		})
	}
}
