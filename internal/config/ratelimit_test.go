package config

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

// optionsWith wraps a rate-limit block body inside a minimal options block so
// the test exercises the real ParseOptions dispatch path (rate-limit lives in
// options). When rlBlock is empty, no rate-limit block is emitted at all.
func optionsWith(rlBlock string) []byte {
	var sb strings.Builder
	sb.WriteString("options {\n\tdirectory \"/etc/namedb\";\n")
	if rlBlock != "" {
		sb.WriteString("\t")
		sb.WriteString(rlBlock)
		sb.WriteString("\n")
	}
	sb.WriteString("};")
	return []byte(sb.String())
}

func TestParseRateLimit(t *testing.T) {
	tests := []struct {
		name    string
		block   string
		wantErr bool
		check   func(t *testing.T, rl *RateLimitConfig)
	}{
		{
			name:  "full block parses with explicit values",
			block: `rate-limit { responses-per-second 10; window 20; slip 3; exempt-clients { 192.0.2.0/24; }; };`,
			check: func(t *testing.T, rl *RateLimitConfig) {
				if rl == nil {
					t.Fatal("RateLimit is nil, want configured")
				}
				if rl.ResponsesPerSecond != 10 {
					t.Errorf("ResponsesPerSecond = %d, want 10", rl.ResponsesPerSecond)
				}
				if rl.Window != 20 {
					t.Errorf("Window = %d, want 20", rl.Window)
				}
				if rl.Slip != 3 {
					t.Errorf("Slip = %d, want 3", rl.Slip)
				}
				if len(rl.ExemptClients) != 1 || rl.ExemptClients[0] != "192.0.2.0/24" {
					t.Errorf("ExemptClients = %v, want [192.0.2.0/24]", rl.ExemptClients)
				}
			},
		},
		{
			name:  "omitted sub-options take BIND defaults",
			block: `rate-limit { responses-per-second 5; };`,
			check: func(t *testing.T, rl *RateLimitConfig) {
				if rl == nil {
					t.Fatal("RateLimit is nil, want configured")
				}
				if rl.Window != 15 {
					t.Errorf("Window = %d, want 15", rl.Window)
				}
				if rl.Slip != 2 {
					t.Errorf("Slip = %d, want 2", rl.Slip)
				}
				if rl.IPv4PrefixLength != 24 {
					t.Errorf("IPv4PrefixLength = %d, want 24", rl.IPv4PrefixLength)
				}
				if rl.IPv6PrefixLength != 56 {
					t.Errorf("IPv6PrefixLength = %d, want 56", rl.IPv6PrefixLength)
				}
				if rl.MaxTableSize != 20000 {
					t.Errorf("MaxTableSize = %d, want 20000", rl.MaxTableSize)
				}
				if rl.MinTableSize != 500 {
					t.Errorf("MinTableSize = %d, want 500", rl.MinTableSize)
				}
				if rl.LogOnly {
					t.Errorf("LogOnly = true, want false (default)")
				}
			},
		},
		{
			name:  "per-category limit defaults to responses-per-second",
			block: `rate-limit { responses-per-second 8; };`,
			check: func(t *testing.T, rl *RateLimitConfig) {
				if rl.NxdomainsPerSecond != 8 {
					t.Errorf("NxdomainsPerSecond = %d, want 8 (inherited from responses-per-second)", rl.NxdomainsPerSecond)
				}
				if rl.NodataPerSecond != 8 {
					t.Errorf("NodataPerSecond = %d, want 8", rl.NodataPerSecond)
				}
				if rl.ErrorsPerSecond != 8 {
					t.Errorf("ErrorsPerSecond = %d, want 8", rl.ErrorsPerSecond)
				}
				if rl.ReferralsPerSecond != 8 {
					t.Errorf("ReferralsPerSecond = %d, want 8", rl.ReferralsPerSecond)
				}
			},
		},
		{
			name:  "per-category explicit value overrides inheritance",
			block: `rate-limit { responses-per-second 8; nxdomains-per-second 3; };`,
			check: func(t *testing.T, rl *RateLimitConfig) {
				if rl.NxdomainsPerSecond != 3 {
					t.Errorf("NxdomainsPerSecond = %d, want 3 (explicit)", rl.NxdomainsPerSecond)
				}
				if rl.NodataPerSecond != 8 {
					t.Errorf("NodataPerSecond = %d, want 8 (inherited)", rl.NodataPerSecond)
				}
			},
		},
		{
			name:  "all fields parse",
			block: `rate-limit { responses-per-second 1; referrals-per-second 2; nodata-per-second 3; nxdomains-per-second 4; errors-per-second 5; all-per-second 6; window 30; slip 1; ipv4-prefix-length 32; ipv6-prefix-length 64; log-only yes; max-table-size 1000; min-table-size 100; };`,
			check: func(t *testing.T, rl *RateLimitConfig) {
				want := RateLimitConfig{
					ResponsesPerSecond: 1, ReferralsPerSecond: 2, NodataPerSecond: 3,
					NxdomainsPerSecond: 4, ErrorsPerSecond: 5, AllPerSecond: 6,
					Window: 30, Slip: 1, IPv4PrefixLength: 32, IPv6PrefixLength: 64,
					LogOnly: true, MaxTableSize: 1000, MinTableSize: 100,
				}
				if rl.ResponsesPerSecond != want.ResponsesPerSecond ||
					rl.ReferralsPerSecond != want.ReferralsPerSecond ||
					rl.NodataPerSecond != want.NodataPerSecond ||
					rl.NxdomainsPerSecond != want.NxdomainsPerSecond ||
					rl.ErrorsPerSecond != want.ErrorsPerSecond ||
					rl.AllPerSecond != want.AllPerSecond ||
					rl.Window != want.Window ||
					rl.Slip != want.Slip ||
					rl.IPv4PrefixLength != want.IPv4PrefixLength ||
					rl.IPv6PrefixLength != want.IPv6PrefixLength ||
					rl.LogOnly != want.LogOnly ||
					rl.MaxTableSize != want.MaxTableSize ||
					rl.MinTableSize != want.MinTableSize {
					t.Errorf("parsed = %+v, want %+v", *rl, want)
				}
			},
		},
		{
			name:    "out-of-range slip is fatal",
			block:   `rate-limit { slip 99; };`,
			wantErr: true,
		},
		{
			name:    "out-of-range ipv4-prefix-length is fatal",
			block:   `rate-limit { ipv4-prefix-length 33; };`,
			wantErr: true,
		},
		{
			name:    "negative per-second is fatal",
			block:   `rate-limit { responses-per-second -1; };`,
			wantErr: true,
		},
		{
			name:    "non-numeric value is fatal",
			block:   `rate-limit { window abc; };`,
			wantErr: true,
		},
		{
			name:  "absent block is unconfigured (nil)",
			block: "",
			check: func(t *testing.T, rl *RateLimitConfig) {
				if rl != nil {
					t.Errorf("RateLimit = %+v, want nil (unconfigured)", *rl)
				}
			},
		},
		{
			name:  "empty block is configured with zero limits (distinct from absent)",
			block: `rate-limit { };`,
			check: func(t *testing.T, rl *RateLimitConfig) {
				if rl == nil {
					t.Fatal("RateLimit is nil, want non-nil configured-with-zeros")
				}
				if rl.ResponsesPerSecond != 0 {
					t.Errorf("ResponsesPerSecond = %d, want 0", rl.ResponsesPerSecond)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, _, err := ParseOptions(optionsWith(tc.block), 0, "named.conf", zap.NewNop())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected fatal parse error, got nil (RateLimit=%+v)", block.RateLimit)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, block.RateLimit)
			}
		})
	}
}
