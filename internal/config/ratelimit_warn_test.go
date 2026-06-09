package config

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestRateLimitWarnIgnore covers "Warn and ignore unsupported rate-limit
// constructs": qps-scale inside a rate-limit block, and a rate-limit block
// inside a view. Both must warn, must not be fatal, and parsing must continue.
func TestRateLimitWarnIgnore(t *testing.T) {
	t.Run("qps-scale is warned and ignored, parsing continues", func(t *testing.T) {
		core, obs := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)

		input := optionsWith(`rate-limit { responses-per-second 5; qps-scale 250; window 20; };`)
		block, _, err := ParseOptions(input, 0, "named.conf", logger)
		if err != nil {
			t.Fatalf("qps-scale should not be fatal, got error: %v", err)
		}
		if block.RateLimit == nil {
			t.Fatal("RateLimit is nil; block should have parsed")
		}
		// Parsing continued past qps-scale: both surrounding sub-options applied.
		if block.RateLimit.ResponsesPerSecond != 5 {
			t.Errorf("ResponsesPerSecond = %d, want 5", block.RateLimit.ResponsesPerSecond)
		}
		if block.RateLimit.Window != 20 {
			t.Errorf("Window = %d, want 20 (parse must continue past qps-scale)", block.RateLimit.Window)
		}
		warns := obs.FilterField(zap.String("option", "qps-scale")).All()
		if len(warns) == 0 {
			t.Errorf("expected a warning mentioning qps-scale, got logs: %+v", obs.All())
		}
	})

	t.Run("view-level rate-limit is warned and ignored, not fatal", func(t *testing.T) {
		core, obs := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)

		dir := t.TempDir()
		confPath := filepath.Join(dir, "named.conf")
		conf := `options {
	directory "/tmp";
};
view "default" {
	match-clients { any; };
	rate-limit { responses-per-second 5; };
};
`
		if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
			t.Fatalf("writing temp named.conf: %v", err)
		}

		cfg, err := LoadNamedConf(confPath, logger)
		if err != nil {
			t.Fatalf("view-level rate-limit should not be fatal, got error: %v", err)
		}
		if len(cfg.Views) != 1 {
			t.Fatalf("expected 1 view parsed, got %d", len(cfg.Views))
		}
		// A warning mentioning rate-limit at view scope must have been emitted.
		warns := obs.FilterMessageSnippet("rate-limit").All()
		if len(warns) == 0 {
			t.Errorf("expected a warning about view-level rate-limit, got logs: %+v", obs.All())
		}
	})
}
