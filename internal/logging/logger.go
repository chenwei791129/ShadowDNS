// Package logging constructs the root zap logger for ShadowDNS.
//
// The color-enablement decision is evaluated once at construction time using
// three layers of precedence (highest to lowest):
//  1. Options.NoColor (the -no-color CLI flag)
//  2. NO_COLOR environment variable is a non-empty string
//  3. isatty(stderr) reports an interactive terminal
//
// Any layer that indicates "disabled" yields a final decision of "disabled".
package logging

import (
	"os"

	"github.com/mattn/go-isatty"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Options captures every input required to build a logger.
// The zero value produces an InfoLevel logger writing to stderr with color
// enabled subject to NO_COLOR and isatty detection.
type Options struct {
	// NoColor, when true, forces uncolored output regardless of other layers.
	NoColor bool
	// Level is the minimum enabled log level. Zero value is InfoLevel.
	Level zapcore.Level
	// LogFile, when non-empty, routes all logger output to this file path
	// (opened with O_APPEND|O_CREATE at mode 0640) instead of os.Stderr.
	// Empty string keeps the legacy stderr sink for tests, dev runs, and any
	// invocation that prefers systemd-journal over a file.
	LogFile string
}

// stderrIsTerminal is a package-level hook so tests can simulate TTY and
// non-TTY stderr without touching file descriptors.
var stderrIsTerminal = func() bool {
	return isatty.IsTerminal(os.Stderr.Fd())
}

// New constructs a zap logger and (when opts.LogFile is non-empty) the
// underlying ReopenSink that drives the file-backed sink. The reopener
// return value is nil when the logger writes to stderr, signalling to
// callers (the daemon) that no SIGUSR1 reopen handler should be wired.
//
// The color decision is fixed at the moment of this call; subsequent
// environment changes have no effect.
//
// If opts.LogFile is non-empty and cannot be opened, New returns an error
// so the daemon's startup path aborts loudly instead of silently falling
// back to stderr.
func New(opts Options) (*zap.Logger, *ReopenSink, error) {
	if opts.LogFile == "" {
		return newWithSink(opts, zapcore.Lock(os.Stderr)), nil, nil
	}
	sink, err := OpenReopenSink(opts.LogFile)
	if err != nil {
		return nil, nil, err
	}
	// ReopenSink already serializes Write/Sync/Reopen via its own mutex,
	// so an outer zapcore.Lock would only add a redundant second mutex.
	return newWithSink(opts, sink), sink, nil
}

// newWithSink is the testable variant. Tests pass a bytes.Buffer wrapped in a
// WriteSyncer to inspect encoded output without mocking stderr.
func newWithSink(opts Options, sink zapcore.WriteSyncer) *zap.Logger {
	useColor := shouldUseColor(opts.NoColor)
	encoderCfg := BaseEncoderConfig()
	if useColor {
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		sink,
		opts.Level,
	)
	return zap.New(core)
}

// shouldUseColor applies the three-layer precedence. Any disabling signal wins.
func shouldUseColor(noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return stderrIsTerminal()
}

// BaseEncoderConfig returns the canonical encoder config used by production
// loggers: ISO8601 timestamps, `time`/`level`/`msg` keys, no caller or
// stacktrace fields. Exported so tests can build buffer-backed loggers whose
// format matches production without copying the config.
//
// Callers must set `EncodeLevel` themselves (color vs plain) since that is the
// one field that varies by context.
func BaseEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		MessageKey:     "msg",
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		LineEnding:     zapcore.DefaultLineEnding,
	}
}
