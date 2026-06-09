package config

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// RateLimitConfig holds the parsed `rate-limit { ... }` block from the options
// block. A nil *RateLimitConfig on OptionsBlock means rate limiting is
// unconfigured (distinct from a block with all-zero limits). Field defaults are
// BIND-compatible and are filled in by parseRateLimit, so every field is
// meaningful regardless of which sub-options the operator wrote.
type RateLimitConfig struct {
	// Per-second credit rates per response category. 0 disables limiting for
	// that category. The per-category rates default to ResponsesPerSecond when
	// not set individually (BIND behaviour); AllPerSecond is an independent
	// aggregate gate that defaults to 0 and does not inherit.
	ResponsesPerSecond int
	ReferralsPerSecond int
	NodataPerSecond    int
	NxdomainsPerSecond int
	ErrorsPerSecond    int
	AllPerSecond       int

	// Window is the rolling-window length in seconds; credit caps at
	// Window × rate. Slip selects drop vs TC=1 truncation for over-limit
	// responses (0 = always drop, 1 = always truncate, n = truncate every nth).
	Window int
	Slip   int

	// IPv4PrefixLength / IPv6PrefixLength mask the client address before it
	// becomes part of an account key, so neighbouring addresses share a budget.
	IPv4PrefixLength int
	IPv6PrefixLength int

	// ExemptClients is the raw address-match-list (IPs / CIDRs) whose responses
	// bypass rate limiting entirely. Validation of each entry happens when the
	// limiter is constructed.
	ExemptClients []string

	// LogOnly, when true, records would-be drops/slips but delivers every
	// response unchanged (dry-run before enforcing).
	LogOnly bool

	// MaxTableSize / MinTableSize bound the number of tracked accounts.
	MaxTableSize int
	MinTableSize int
}

// BIND-compatible defaults and validation bounds for rate-limit sub-options.
const (
	rlDefaultWindow       = 15
	rlDefaultSlip         = 2
	rlDefaultIPv4Prefix   = 24
	rlDefaultIPv6Prefix   = 56
	rlDefaultMaxTableSize = 20000
	rlDefaultMinTableSize = 500

	rlMaxPerSecond = 1_000_000_000 // matches BIND's per-second upper bound
	rlMaxWindow    = 3600
	rlMaxSlip      = 10
	rlMaxTableSize = 1_000_000_000
)

// parseRateLimit parses a `rate-limit { ... };` block. The lexer has already
// consumed the "rate-limit" keyword; the next token must be '{'. It returns the
// parsed configuration with BIND defaults applied for any omitted sub-option.
//
// Per-category per-second limits (referrals/nodata/nxdomains/errors) inherit
// the value of responses-per-second when not set individually; all-per-second
// is an independent aggregate that defaults to 0.
func parseRateLimit(lx *lexer, path string, logger *zap.Logger) (*RateLimitConfig, error) {
	open := lx.next()
	if open.kind != tokenLBrace {
		return nil, fmt.Errorf("%s:%d: expected '{' after 'rate-limit', got %q", path, open.line, open.value)
	}

	cfg := &RateLimitConfig{
		Window:           rlDefaultWindow,
		Slip:             rlDefaultSlip,
		IPv4PrefixLength: rlDefaultIPv4Prefix,
		IPv6PrefixLength: rlDefaultIPv6Prefix,
		MaxTableSize:     rlDefaultMaxTableSize,
		MinTableSize:     rlDefaultMinTableSize,
	}

	// Per-category rates use pointers so an explicit value (including 0) is
	// distinguishable from "absent", which lets the absent ones inherit
	// responses-per-second after the loop.
	var rps, referrals, nodata, nxdomains, errs *int

	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return nil, fmt.Errorf("%s:%d: unterminated rate-limit block", path, tok.line)
		}
		if tok.kind == tokenRBrace {
			// Consume optional ';' after '}'.
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			break
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return nil, fmt.Errorf("%s:%d: unexpected token %q in rate-limit block", path, tok.line, tok.value)
		}

		key := strings.ToLower(tok.value)
		switch key {
		case "responses-per-second":
			v, e := lx.readIntValue(path, key, 0, rlMaxPerSecond)
			if e != nil {
				return nil, e
			}
			rps = &v
		case "referrals-per-second":
			v, e := lx.readIntValue(path, key, 0, rlMaxPerSecond)
			if e != nil {
				return nil, e
			}
			referrals = &v
		case "nodata-per-second":
			v, e := lx.readIntValue(path, key, 0, rlMaxPerSecond)
			if e != nil {
				return nil, e
			}
			nodata = &v
		case "nxdomains-per-second":
			v, e := lx.readIntValue(path, key, 0, rlMaxPerSecond)
			if e != nil {
				return nil, e
			}
			nxdomains = &v
		case "errors-per-second":
			v, e := lx.readIntValue(path, key, 0, rlMaxPerSecond)
			if e != nil {
				return nil, e
			}
			errs = &v
		case "all-per-second":
			v, e := lx.readIntValue(path, key, 0, rlMaxPerSecond)
			if e != nil {
				return nil, e
			}
			cfg.AllPerSecond = v
		case "window":
			v, e := lx.readIntValue(path, key, 1, rlMaxWindow)
			if e != nil {
				return nil, e
			}
			cfg.Window = v
		case "slip":
			v, e := lx.readIntValue(path, key, 0, rlMaxSlip)
			if e != nil {
				return nil, e
			}
			cfg.Slip = v
		case "ipv4-prefix-length":
			v, e := lx.readIntValue(path, key, 0, 32)
			if e != nil {
				return nil, e
			}
			cfg.IPv4PrefixLength = v
		case "ipv6-prefix-length":
			v, e := lx.readIntValue(path, key, 0, 128)
			if e != nil {
				return nil, e
			}
			cfg.IPv6PrefixLength = v
		case "exempt-clients":
			tokens, e := lx.readBracedList(path)
			if e != nil {
				return nil, e
			}
			cfg.ExemptClients = tokens
		case "log-only":
			v, e := lx.readScalarValue(path)
			if e != nil {
				return nil, e
			}
			switch strings.ToLower(v) {
			case "yes":
				cfg.LogOnly = true
			case "no":
				cfg.LogOnly = false
			default:
				return nil, fmt.Errorf("%s:%d: invalid value %q for 'log-only', expected yes/no", path, tok.line, v)
			}
		case "max-table-size":
			v, e := lx.readIntValue(path, key, 0, rlMaxTableSize)
			if e != nil {
				return nil, e
			}
			cfg.MaxTableSize = v
		case "min-table-size":
			v, e := lx.readIntValue(path, key, 0, rlMaxTableSize)
			if e != nil {
				return nil, e
			}
			cfg.MinTableSize = v
		case "qps-scale":
			// Load-adaptive QPS scaling is not implemented (it would require a
			// global QPS measurement that is out of scope). Warn, ignore the
			// value, and keep parsing the rest of the block.
			logger.Sugar().Warnw("rate-limit qps-scale is not supported (load-adaptive scaling not implemented); ignoring",
				"option", key,
				"line", tok.line,
				"file", path,
			)
			if e := lx.skipOptionValue(path); e != nil {
				return nil, e
			}
		default:
			// Unknown sub-option: warn and skip.
			logger.Sugar().Warnw("unknown sub-option in rate-limit block, skipping",
				"option", key,
				"line", tok.line,
				"file", path,
			)
			if e := lx.skipOptionValue(path); e != nil {
				return nil, e
			}
		}
	}

	// Reject an inconsistent table-size range. With the default min (500) this
	// also rejects `max-table-size 0`, which would otherwise collapse the
	// account table to one entry per shard and silently defeat rate limiting.
	if cfg.MinTableSize > cfg.MaxTableSize {
		return nil, fmt.Errorf("%s: rate-limit min-table-size (%d) must not exceed max-table-size (%d)", path, cfg.MinTableSize, cfg.MaxTableSize)
	}

	// Resolve per-category inheritance: absent categories fall back to
	// responses-per-second (default 0 when responses-per-second is also absent).
	base := 0
	if rps != nil {
		base = *rps
	}
	cfg.ResponsesPerSecond = base
	cfg.ReferralsPerSecond = derefOr(referrals, base)
	cfg.NodataPerSecond = derefOr(nodata, base)
	cfg.NxdomainsPerSecond = derefOr(nxdomains, base)
	cfg.ErrorsPerSecond = derefOr(errs, base)

	return cfg, nil
}

// derefOr returns *p when p is non-nil, otherwise fallback.
func derefOr(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

// readIntValue reads a `<int>;` scalar value, validating it parses as a base-10
// integer within [min, max]. The optName and bounds are used purely for the
// error message so out-of-range values surface as fatal parse errors consistent
// with other numeric option validation.
func (lx *lexer) readIntValue(path, optName string, lo, hi int) (int, error) {
	val, err := lx.readScalarValue(path)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(val)
	if convErr != nil {
		return 0, fmt.Errorf("%s: %q is not a valid integer for '%s'", path, val, optName)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%s: value %d for '%s' is out of range [%d, %d]", path, n, optName, lo, hi)
	}
	return n, nil
}
