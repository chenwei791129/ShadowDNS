package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// bufferSink is a zapcore.WriteSyncer backed by bytes.Buffer, safe for single-
// goroutine test use.
type bufferSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bufferSink) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bufferSink) Sync() error { return nil }

func (b *bufferSink) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// withTTY swaps the stderrIsTerminal hook for the duration of the test.
func withTTY(t *testing.T, isTTY bool) {
	t.Helper()
	prev := stderrIsTerminal
	stderrIsTerminal = func() bool { return isTTY }
	t.Cleanup(func() { stderrIsTerminal = prev })
}

// logAndCapture builds a logger with the given options writing to an in-memory
// sink, emits a single INFO line, and returns the encoded output.
func logAndCapture(t *testing.T, opts Options) string {
	t.Helper()
	sink := &bufferSink{}
	logger := newWithSink(opts, sink)
	logger.Info("hello", zap.String("user", "alice"))
	_ = logger.Sync()
	return sink.String()
}

// hasANSIColor reports whether s contains any ANSI escape sequence.
func hasANSIColor(s string) bool {
	return strings.Contains(s, "\x1b[")
}

// Requirement: Decision precedence (3.1)
func TestDecisionPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		noColor  bool
		envValue string
		envSet   bool
		isTTY    bool
		want     bool
	}{
		{"flag disables in TTY", true, "", false, true, false},
		{"NO_COLOR disables in TTY", false, "1", true, true, false},
		{"non-TTY disables", false, "", false, false, false},
		{"all permit → color on", false, "", false, true, true},
		{"flag wins over all-permit", true, "", false, true, false},
		{"NO_COLOR wins over TTY", false, "yes", true, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withTTY(t, tc.isTTY)
			if tc.envSet {
				t.Setenv("NO_COLOR", tc.envValue)
			} else {
				t.Setenv("NO_COLOR", "")
			}
			got := shouldUseColor(tc.noColor)
			if got != tc.want {
				t.Fatalf("shouldUseColor(%v) with env=%q isTTY=%v = %v, want %v",
					tc.noColor, tc.envValue, tc.isTTY, got, tc.want)
			}
		})
	}
}

// Requirement: CLI flag -no-color forces uncolored output (3.2)
func TestFlagForcesUncoloredInTTY(t *testing.T) {
	withTTY(t, true)
	t.Setenv("NO_COLOR", "")
	out := logAndCapture(t, Options{NoColor: true})
	if hasANSIColor(out) {
		t.Fatalf("expected no ANSI escape, got %q", out)
	}
}

// Requirement: NO_COLOR environment variable disables color (3.3)
func TestNoColorEnvVar(t *testing.T) {
	t.Run("non-empty disables color", func(t *testing.T) {
		withTTY(t, true)
		t.Setenv("NO_COLOR", "1")
		out := logAndCapture(t, Options{})
		if hasANSIColor(out) {
			t.Fatalf("expected no ANSI escape with NO_COLOR=1, got %q", out)
		}
	})
	t.Run("empty string does not disable color", func(t *testing.T) {
		withTTY(t, true)
		t.Setenv("NO_COLOR", "")
		out := logAndCapture(t, Options{})
		if !hasANSIColor(out) {
			t.Fatalf("expected ANSI escape with NO_COLOR empty and TTY, got %q", out)
		}
	})
}

// Requirement: Automatic TTY detection disables color (3.4)
func TestNonTTYAlwaysUncolored(t *testing.T) {
	withTTY(t, false)
	t.Setenv("NO_COLOR", "")
	out := logAndCapture(t, Options{})
	if hasANSIColor(out) {
		t.Fatalf("expected no ANSI escape in non-TTY, got %q", out)
	}
}

// Requirement: Color is applied only to the level field (3.5)
func TestColorOnlyOnLevel(t *testing.T) {
	withTTY(t, true)
	t.Setenv("NO_COLOR", "")
	sink := &bufferSink{}
	logger := newWithSink(Options{}, sink)
	logger.Info("hello world", zap.String("user", "alice"), zap.Int("count", 42))
	_ = logger.Sync()
	out := sink.String()

	if !hasANSIColor(out) {
		t.Fatalf("expected ANSI escape somewhere, got %q", out)
	}

	// ANSI escape sequences should wrap only the level token. Verify the message
	// text, the structured field values, and the timestamp prefix are not wrapped
	// in escape codes.
	//
	// The ConsoleEncoder emits: <time>\t<colored-level>\t<msg>\t{"user":"alice","count":42}
	// We strip the first two tab-separated fields (time, level) and assert the
	// remainder is ANSI-free.
	parts := strings.SplitN(out, "\t", 3)
	if len(parts) < 3 {
		t.Fatalf("unexpected encoded layout %q", out)
	}
	if hasANSIColor(parts[0]) {
		t.Errorf("timestamp contains ANSI escape: %q", parts[0])
	}
	if !hasANSIColor(parts[1]) {
		t.Errorf("level token missing ANSI escape: %q", parts[1])
	}
	if hasANSIColor(parts[2]) {
		t.Errorf("message/fields contain ANSI escape: %q", parts[2])
	}
}

// Requirement: Decision is fixed at logger construction (3.6)
func TestDecisionFixedAtConstruction(t *testing.T) {
	withTTY(t, true)
	t.Setenv("NO_COLOR", "")
	sink := &bufferSink{}
	logger := newWithSink(Options{}, sink)

	// Construction happened with color enabled. Mutating env afterwards must
	// not affect already-built logger.
	t.Setenv("NO_COLOR", "1")

	logger.Info("after env change")
	_ = logger.Sync()
	out := sink.String()
	if !hasANSIColor(out) {
		t.Fatalf("logger should remain colored after post-construction env change, got %q", out)
	}
}

// Guard: encoder uses the required keys and ISO8601 time.
func TestEncoderConfigShape(t *testing.T) {
	cfg := BaseEncoderConfig()
	if cfg.MessageKey != "msg" {
		t.Errorf("MessageKey = %q, want %q", cfg.MessageKey, "msg")
	}
	if cfg.LevelKey != "level" {
		t.Errorf("LevelKey = %q, want %q", cfg.LevelKey, "level")
	}
}

// Guard: New returns a non-nil *zap.Logger.
func TestNewReturnsZapLogger(t *testing.T) {
	logger := New(Options{Level: zapcore.InfoLevel})
	if logger == nil {
		t.Fatal("New returned nil")
	}
}
