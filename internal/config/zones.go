package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// ZoneTypeMaster is the only zone type supported by ShadowDNS.
const ZoneTypeMaster = "master"

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// Zone represents a single zone declaration within a view block.
type Zone struct {
	Name   string // domain name, lowercased, no trailing dot (e.g. "example.com")
	Type   string // currently always "master"
	File   string // absolute path to the zone file
	Line   int    // line number in source file where this zone block opens
	Source string // path of the file the zone was declared in
}

// View represents a single view declaration.
type View struct {
	Name         string
	MatchClients []MatchRule
	Zones        []Zone
	Line         int
	Source       string
}

// Config is the result of loading a named.conf and its included files.
type Config struct {
	Path     string          // the named.conf path
	Options  OptionsBlock    // parsed options block
	Views    []View          // in declaration order across all included files
	QueryLog *QueryLogConfig // nil when query logging is disabled

	// QueryLogDisabledReason is a human-readable string explaining why query
	// logging is disabled. It is non-empty only when a logging{} block was
	// present in the named.conf but resulted in a disabled state (i.e.
	// QueryLog == nil because of a specific disable condition). When QueryLog
	// is nil and QueryLogDisabledReason is empty, no logging{} block was found.
	QueryLogDisabledReason string

	// topLevelZones accumulates zones declared outside any view block during
	// loadFile, in declaration order across all included files. It is a
	// load-time scratch field only: after loadFile completes, LoadNamedConf
	// either folds these into a synthesized _default view (viewless configs) or
	// rejects them (mixed with explicit views). It is never read by consumers
	// of Config.
	topLevelZones []Zone

	// optionsSet records whether an options{} block has already been applied
	// during loadFile, so a second block (in any file) can be flagged. It is a
	// load-time scratch field only and is never read by consumers of Config.
	optionsSet bool
}

// defaultViewName is the name BIND gives the implicit view that holds top-level
// zones when no explicit view block is present. ShadowDNS synthesizes a view
// with this name for BIND-compatible viewless configurations.
const defaultViewName = "_default"

// ---------------------------------------------------------------------------
// Tiered logging classification for skipped directives. Keys are lowercase;
// callers lower-case the directive (strings.ToLower) before lookup.
// ---------------------------------------------------------------------------

// accessControlDirectives are access-control statements ShadowDNS does not
// enforce. Skipping one is logged at WARN so the operator knows the ACL is not
// applied (the migration guide documents ShadowDNS's access-control model).
var accessControlDirectives = map[string]bool{
	"allow-query":     true,
	"allow-recursion": true,
	"allow-transfer":  true,
	"allow-update":    true,
	"allow-notify":    true,
	"blackhole":       true,
}

// recursionFamilyDirectives are recursion-related statements ShadowDNS ignores
// (it is authoritative-only). Encountering one on a BIND drop-in config is
// expected, so skipping it is logged at INFO rather than WARN to avoid noise on
// blessed configs at every startup/reload.
var recursionFamilyDirectives = map[string]bool{
	"recursion":         true,
	"forwarders":        true,
	"dnssec-validation": true,
}

// logSkippedDirective logs a skipped directive at the level dictated by the
// tiered logging strategy: access-control directives at WARN (ShadowDNS does
// not enforce them), recursion-family directives at INFO, everything else at
// DEBUG. scope names where the directive was skipped — "top level" or a view
// name.
func logSkippedDirective(logger *zap.Logger, directive, scope, path string, line int) {
	switch {
	case accessControlDirectives[directive]:
		logger.Sugar().Warnw("ShadowDNS does not enforce this access-control directive; skipping",
			"directive", directive, "scope", scope, "line", line, "file", path)
	case recursionFamilyDirectives[directive]:
		logger.Sugar().Infow("ignoring recursion-related directive (ShadowDNS is authoritative-only)",
			"directive", directive, "scope", scope, "line", line, "file", path)
	default:
		logger.Sugar().Debugw("skipping unrecognized directive",
			"directive", directive, "scope", scope, "line", line, "file", path)
	}
}

// zoneIsSkipped reports whether a parsed zone declares an explicit type other
// than master and must therefore be dropped (not served). A zone that omits
// `type` (z.Type == "") is tolerated and kept, preserving the pre-existing
// missing-type behavior shared by in-view and top-level zones.
func zoneIsSkipped(z Zone) bool {
	return z.Type != "" && z.Type != ZoneTypeMaster
}

// logSkippedZone logs a dropped non-master zone at INFO. scope names where the
// zone was declared — "top level" or a view name — using the same `scope` field
// as logSkippedDirective so log queries are uniform across both.
func logSkippedZone(logger *zap.Logger, z Zone, scope, path string) {
	logger.Sugar().Infow("skipping zone with unsupported type (only 'master' is served)",
		"zone", z.Name, "type", z.Type, "scope", scope, "line", z.Line, "file", path)
}

// ---------------------------------------------------------------------------
// LoadNamedConf
// ---------------------------------------------------------------------------

// LoadNamedConf reads `path`, follows `include "..."` directives, parses the
// options block plus every view/zone declaration. The returned Views preserve
// declaration order across files.
//
// include paths are resolved relative to the file containing the include.
//
// logger MUST NOT be nil; the caller passes zap.NewNop() if needed.
//
// MUST NOT panic on any input.
func LoadNamedConf(path string, logger *zap.Logger) (*Config, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	cfg := &Config{Path: path}

	if err := loadFile(path, cfg, logger); err != nil {
		return nil, err
	}

	// Resolve top-level zones: reject mixing with explicit views, otherwise
	// synthesize the implicit _default view. This must run before
	// warnShadowedViews so that shadow-warning sees the final view list.
	if err := cfg.resolveTopLevelZones(logger); err != nil {
		return nil, err
	}

	// Warn when a non-last view uses `any` (subsequent views would be unreachable).
	warnShadowedViews(cfg.Views, logger)

	return cfg, nil
}

// resolveTopLevelZones post-processes zones declared outside any view block,
// implementing BIND-compatible viewless behavior:
//
//   - >=1 view AND >=1 top-level zone → fatal mixing error (order-independent),
//     naming the first top-level zone with its source:line.
//   - 0 views AND >=1 top-level zone → synthesize a single _default view
//     (match-clients any) holding every top-level zone in declaration order,
//     warning once per duplicated zone name.
//   - 0 top-level zones → no-op (preserves existing behavior, including the
//     empty-config case and explicitly declared view "_default").
func (cfg *Config) resolveTopLevelZones(logger *zap.Logger) error {
	if len(cfg.topLevelZones) == 0 {
		return nil
	}
	first := cfg.topLevelZones[0]

	if len(cfg.Views) > 0 {
		return fmt.Errorf("%s:%d: zone %q declared at top level but %d view(s) are defined; when any view is present all zones must be declared inside views",
			first.Source, first.Line, first.Name, len(cfg.Views))
	}

	warnDuplicateTopLevelZones(cfg.topLevelZones, logger)

	// `any;` is a constant, well-formed match-clients body: it yields exactly one
	// AnyRule and never drops a token, so the dropped slice is ignored here.
	anyRules, _, err := ParseMatchClients([]byte("any;"), first.Source, first.Line)
	if err != nil {
		// A parse failure here would indicate a programming error, not bad user input.
		return fmt.Errorf("synthesize _default view: %w", err)
	}
	cfg.Views = append(cfg.Views, View{
		Name:         defaultViewName,
		MatchClients: anyRules,
		Zones:        cfg.topLevelZones,
		Line:         first.Line,
		Source:       first.Source,
	})
	return nil
}

// warnDuplicateTopLevelZones logs exactly one warning per top-level zone name
// declared two or more times, listing every declaration position. Duplicates
// are tolerated (not fatal): BuildState's per-view map keeps the last
// declaration, so the warning states that the last declaration wins at serving
// time. Groups are reported in first-declaration order for deterministic logs.
func warnDuplicateTopLevelZones(zones []Zone, logger *zap.Logger) {
	type group struct {
		name  string
		sites []string
	}
	indexByName := make(map[string]int, len(zones))
	var groups []group
	for _, z := range zones {
		site := fmt.Sprintf("%s:%d", z.Source, z.Line)
		if idx, ok := indexByName[z.Name]; ok {
			groups[idx].sites = append(groups[idx].sites, site)
		} else {
			indexByName[z.Name] = len(groups)
			groups = append(groups, group{name: z.Name, sites: []string{site}})
		}
	}
	for _, g := range groups {
		if len(g.sites) < 2 {
			continue
		}
		logger.Sugar().Warnw(
			"duplicate top-level zone name; the last declaration takes effect at serving time",
			"zone", g.name,
			"declarations", strings.Join(g.sites, ", "),
		)
	}
}

// loadFile reads a single file (named.conf or any included file) and appends
// parsed views / options into cfg.
func loadFile(path string, cfg *Config, logger *zap.Logger) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: cannot read file: %w", path, err)
	}

	lx := newLexer(data, 0)
	fileDir := filepath.Dir(path)

	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			break
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return fmt.Errorf("%s:%d: unexpected token %q at top level", path, tok.line, tok.value)
		}

		directive := strings.ToLower(tok.value)

		switch directive {
		case "options":
			// Re-derive the byte offset of the "options" keyword start.
			// The lexer already consumed "options", so the keyword begins at
			// lx.pos - len("options").
			optOffset := lx.pos - len("options")
			block, endOff, oErr := ParseOptions(data, optOffset, path, logger)
			if oErr != nil {
				return oErr
			}
			// Apply the options{} block regardless of which file declares it.
			// `include` is a textual inclusion in BIND, so an options block in an
			// included file (e.g. the Debian-idiomatic named.conf.options) is
			// honored exactly as if inlined into named.conf. BIND permits only one
			// options statement; if a second block is seen across the include
			// tree, warn and let the later block win.
			if cfg.optionsSet {
				logger.Sugar().Warnw("multiple options{} blocks across the include tree; the last block takes effect",
					"file", path, "line", countLines(data, 0, optOffset))
			}
			cfg.Options = block
			cfg.optionsSet = true
			lx.pos = endOff
			// Recount lines after jumping pos.
			lx.line = countLines(data, 0, endOff)

		case "include":
			// include "filename";
			fileTok := lx.next()
			if fileTok.kind != tokenString && fileTok.kind != tokenWord {
				return fmt.Errorf("%s:%d: expected filename after 'include', got %q", path, fileTok.line, fileTok.value)
			}
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return fmt.Errorf("%s:%d: expected ';' after include filename, got %q", path, fileTok.line, semi.value)
			}
			includePath := fileTok.value
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(fileDir, includePath)
			}
			if err := loadFile(includePath, cfg, logger); err != nil {
				return err
			}

		case "view":
			v, err := parseView(lx, path, cfg.Options, logger)
			if err != nil {
				return err
			}
			cfg.Views = append(cfg.Views, v)

		case "zone":
			// Top-level zone (outside any view). Reuse parseZone so zone-body
			// rules (relative-path resolution against cfg.Options.Directory,
			// missing type/file tolerance) match in-view zones exactly.
			// Accumulate for post-processing in LoadNamedConf.
			z, err := parseZone(lx, path, cfg.Options)
			if err != nil {
				return err
			}
			// A zone with an explicit non-master type is dropped (not served,
			// file never opened), matching the in-view rule. Common in
			// named.conf.default-zones (the root `type hint` zone).
			if zoneIsSkipped(z) {
				logSkippedZone(logger, z, "top level", path)
				continue
			}
			cfg.topLevelZones = append(cfg.topLevelZones, z)

		case "logging":
			// logging { ... }; — parse and extract query log configuration.
			// Re-derive the byte offset of the "logging" keyword start.
			logOffset := lx.pos - len("logging")
			qlCfg, endOff, disabledReason, lErr := ParseLogging(data, logOffset, path, cfg.Options, logger)
			if lErr != nil {
				return lErr
			}
			if cfg.Path == path {
				// Only apply logging config from the root named.conf.
				cfg.QueryLog = qlCfg
				// Propagate the disabled reason so that --dry-run can report WHY
				// query logging is disabled even when no QueryLog config is set.
				cfg.QueryLogDisabledReason = disabledReason
			}
			// logging{} in included files is parsed (for syntax checking and
			// lexer advancement) but its config AND disabled reason are
			// intentionally discarded — only the root named.conf is honored.
			// Advance the outer lexer past the logging block using the offset
			// returned by ParseLogging (same pattern as ParseOptions).
			lx.pos = endOff
			lx.line = countLines(data, 0, endOff)

		default:
			// Skip-unknown posture: any directive ShadowDNS does not act on at
			// the top level (acl, key, controls, server, statistics-channels,
			// trusted-keys, …) is consumed and skipped, not fatal. Only a genuine
			// syntax error (unbalanced brace, missing ';') surfaces from the skip
			// helper. The tiered logger classifies the directive.
			logSkippedDirective(logger, directive, "top level", path, tok.line)
			if err := lx.skipNamedDirective(path); err != nil {
				return err
			}
		}
	}

	return nil
}

// parseView parses a `view "name" { ... };` block. The lexer has just consumed
// the "view" keyword.
func parseView(lx *lexer, path string, opts OptionsBlock, logger *zap.Logger) (View, error) {
	// Expect view name (quoted string or word).
	nameTok := lx.next()
	if nameTok.kind != tokenString && nameTok.kind != tokenWord {
		return View{}, fmt.Errorf("%s:%d: expected view name, got %q", path, nameTok.line, nameTok.value)
	}
	viewName := nameTok.value

	// Expect '{'.
	openTok := lx.next()
	if openTok.kind != tokenLBrace {
		return View{}, fmt.Errorf("%s:%d: expected '{' after view name, got %q", path, openTok.line, openTok.value)
	}

	v := View{
		Name:   viewName,
		Line:   nameTok.line,
		Source: path,
	}

	// Parse the view body.
	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return View{}, fmt.Errorf("%s:%d: unterminated view block for %q", path, tok.line, viewName)
		}
		if tok.kind == tokenRBrace {
			// Closing brace of view — consume optional ';'.
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			break
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return View{}, fmt.Errorf("%s:%d: unexpected token %q in view %q", path, tok.line, tok.value, viewName)
		}

		directive := strings.ToLower(tok.value)
		switch directive {
		case "match-clients":
			body, startLine, err := readBracedBodyRaw(lx, path)
			if err != nil {
				return View{}, err
			}
			// Consume trailing ';' after '}'.
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			rules, dropped, err := ParseMatchClients(body, path, startLine)
			if err != nil {
				return View{}, err
			}
			// A rule ShadowDNS cannot evaluate (named-acl reference, `!`
			// negation, nested group) was dropped: WARN naming the view and
			// token. It is fail-closed — never added to MatchClients, so the
			// view-matcher treats it as never-matching. A view whose entire
			// match-clients set is dropped ends up with empty Rules and serves
			// nothing, rather than being widened to `any`.
			for _, d := range dropped {
				// Use the `scope` field (= the view name) so dropped rules,
				// skipped directives, and skipped zones are all queryable by one
				// field across the loader's logs.
				logger.Sugar().Warnw("dropping unevaluable match-clients rule (fail-closed: it never matches)",
					"token", d.Token, "scope", viewName, "line", d.Line, "file", path)
			}
			v.MatchClients = rules

		case "zone":
			z, err := parseZone(lx, path, opts)
			if err != nil {
				return View{}, err
			}
			// Drop a zone with an explicit non-master type (e.g. type forward),
			// matching the top-level rule: not served, file never opened.
			if zoneIsSkipped(z) {
				logSkippedZone(logger, z, viewName, path)
				continue
			}
			v.Zones = append(v.Zones, z)

		case "rate-limit":
			// Rate limiting is supported only at the global options scope (v1).
			// A view-level rate-limit block is warned and ignored, not fatal, so
			// migrating a BIND config with per-view rate-limit still starts.
			logger.Sugar().Warnw("rate-limit inside a view is not supported (only options-scope rate-limit is honored); ignoring",
				"view", viewName,
				"line", tok.line,
				"file", path,
			)
			if err := lx.skipOptionValue(path); err != nil {
				return View{}, err
			}

		default:
			// Skip-unknown posture inside a view: an unrecognized view-scope
			// directive (allow-query, allow-recursion, recursion, …) is consumed
			// and skipped, not fatal. The tiered logger classifies it (access
			// control → WARN, recursion family → INFO, else DEBUG).
			logSkippedDirective(logger, directive, viewName, path, tok.line)
			if err := lx.skipNamedDirective(path); err != nil {
				return View{}, err
			}
		}
	}

	return v, nil
}

// parseZone parses a `zone "domain" { ... };` block. The lexer has just
// consumed the "zone" keyword.
func parseZone(lx *lexer, path string, opts OptionsBlock) (Zone, error) {
	// Expect zone name (quoted string or word).
	nameTok := lx.next()
	if nameTok.kind != tokenString && nameTok.kind != tokenWord {
		return Zone{}, fmt.Errorf("%s:%d: expected zone name, got %q", path, nameTok.line, nameTok.value)
	}
	rawName := nameTok.value
	// Normalize: lowercase + strip trailing dot.
	zoneName := strings.ToLower(strings.TrimSuffix(rawName, "."))

	// Expect '{'.
	openTok := lx.next()
	if openTok.kind != tokenLBrace {
		return Zone{}, fmt.Errorf("%s:%d: expected '{' after zone name, got %q", path, openTok.line, openTok.value)
	}

	z := Zone{
		Name:   zoneName,
		Line:   nameTok.line,
		Source: path,
	}

	// Parse the zone body.
	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return Zone{}, fmt.Errorf("%s:%d: unterminated zone block for %q", path, tok.line, zoneName)
		}
		if tok.kind == tokenRBrace {
			// Closing brace of zone — consume optional ';'.
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			break
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return Zone{}, fmt.Errorf("%s:%d: unexpected token %q in zone %q", path, tok.line, tok.value, zoneName)
		}

		key := strings.ToLower(tok.value)
		switch key {
		case "type":
			valTok := lx.next()
			if valTok.kind != tokenWord && valTok.kind != tokenString {
				return Zone{}, fmt.Errorf("%s:%d: expected zone type value, got %q", path, valTok.line, valTok.value)
			}
			zoneType := strings.ToLower(valTok.value)
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return Zone{}, fmt.Errorf("%s:%d: expected ';' after zone type, got %q", path, valTok.line, semi.value)
			}
			// Record the declared type as-is. Only "master" is served; a zone
			// with any other explicit type is dropped by the caller (see
			// zoneIsSkipped) rather than being fatal, so a BIND config's
			// non-master zones (hint/forward/slave/…) load without error.
			z.Type = zoneType

		case "file":
			valTok := lx.next()
			if valTok.kind != tokenString && valTok.kind != tokenWord {
				return Zone{}, fmt.Errorf("%s:%d: expected file path, got %q", path, valTok.line, valTok.value)
			}
			filePath := valTok.value
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return Zone{}, fmt.Errorf("%s:%d: expected ';' after file path, got %q", path, valTok.line, semi.value)
			}
			// Resolve relative paths.
			if !filepath.IsAbs(filePath) {
				baseDir := opts.Directory
				if baseDir == "" {
					baseDir = filepath.Dir(path)
				}
				filePath = filepath.Join(baseDir, filePath)
			}
			z.File = filePath

		default:
			// Skip unknown zone-body directives. Use skipNamedDirective (not
			// skipOptionValue) because a non-master zone is now parsed in full
			// before the caller drops it via zoneIsSkipped, so its body can
			// contain the `keyword <name> { ... };` shape (e.g. a slave zone's
			// `masters <name> { ... };`). skipOptionValue would mis-scan the
			// first ';' inside that block and desync the token stream; only
			// skipNamedDirective consumes the leading name token before the '{'.
			if err := lx.skipNamedDirective(path); err != nil {
				return Zone{}, err
			}
		}
	}

	return z, nil
}

// warnShadowedViews inspects the view list and emits a Warn log for any non-last
// view that contains an AnyRule (because subsequent views would be unreachable).
func warnShadowedViews(views []View, logger *zap.Logger) {
	for i, v := range views {
		if i == len(views)-1 {
			break // last view — no shadowing possible
		}
		if viewHasAny(v) {
			// Collect names of shadowed views.
			var shadowed []string
			for _, sv := range views[i+1:] {
				shadowed = append(shadowed, sv.Name)
			}
			logger.Sugar().Warnw(
				"view has match-clients 'any' but is not the last view; subsequent views are shadowed",
				"view", v.Name,
				"shadowed_views", strings.Join(shadowed, ", "),
			)
		}
	}
}

// viewHasAny reports whether a view's MatchClients list contains an AnyRule.
func viewHasAny(v View) bool {
	for _, r := range v.MatchClients {
		if _, ok := r.(AnyRule); ok {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Scanner helpers
// ---------------------------------------------------------------------------

// readBracedBodyRaw reads a `{ ... }` block and returns the raw bytes between
// the braces (not including them), plus the line number of the opening '{'.
// The caller is responsible for consuming any trailing ';'.
func readBracedBodyRaw(lx *lexer, path string) ([]byte, int, error) {
	openTok := lx.next()
	if openTok.kind != tokenLBrace {
		return nil, 0, fmt.Errorf("%s:%d: expected '{', got %q", path, openTok.line, openTok.value)
	}
	startLine := openTok.line
	bodyStart := lx.pos
	depth := 1
	for depth > 0 {
		if lx.pos >= len(lx.input) {
			return nil, 0, fmt.Errorf("%s:%d: unterminated block", path, openTok.line)
		}
		ch := lx.input[lx.pos]
		switch ch {
		case '\n':
			lx.line++
		case '{':
			depth++
		case '}':
			depth--
		}
		lx.pos++
	}
	// lx.pos now points past the closing '}'.
	bodyEnd := lx.pos - 1 // exclude the closing '}'
	body := lx.input[bodyStart:bodyEnd]
	return body, startLine, nil
}

// countLines counts the 1-based line number at a given byte offset in data.
func countLines(data []byte, start, end int) int {
	line := 1
	if end > len(data) {
		end = len(data)
	}
	for i := start; i < end; i++ {
		if data[i] == '\n' {
			line++
		}
	}
	return line
}
