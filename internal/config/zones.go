package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	MatchClients []Element
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

	// ACLs is the registry of top-level `acl "<name>" { ... };` definitions,
	// keyed by acl name, each parsed into an ordered element list with the same
	// grammar as match-clients. References inside acl bodies and view
	// match-clients are resolved against this registry after all files load.
	ACLs map[string][]Element

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

// topLevelScope is the scope label used for directives/zones skipped outside any
// view block (a view's scope label is its name).
const topLevelScope = "top level"

// structuralTopLevel and structuralView are the block-introducing keywords whose
// misspelling silently drops a whole config section (a typo'd `view`/`zone`
// becomes an unrecognized directive that the skip-unknown posture quietly
// consumes). A skipped directive within one edit of one of these is almost
// certainly such a typo, so it is surfaced at WARN naming the likely intent.
var (
	structuralTopLevel = []string{"options", "include", "acl", "view", "zone", "logging"}
	structuralView     = []string{"match-clients", "zone", "rate-limit"}
)

// nearMissKeyword returns the structural keyword within edit-distance 1 of
// directive for the given scope, or "" if none. The top-level scope uses the
// top-level keyword set; any other scope (a view name) uses the view set.
func nearMissKeyword(directive, scope string) string {
	candidates := structuralView
	if scope == topLevelScope {
		candidates = structuralTopLevel
	}
	for _, kw := range candidates {
		if kw != directive && withinEditDistance1(kw, directive) {
			return kw
		}
	}
	return ""
}

// logSkippedDirective logs a skipped directive at the level dictated by the
// tiered logging strategy: a likely misspelling of a structural keyword at WARN
// (naming the suspected intent), access-control directives at WARN (ShadowDNS
// does not enforce them), recursion-family directives at INFO, everything else
// at DEBUG. scope names where the directive was skipped — "top level" or a view
// name.
func logSkippedDirective(logger *zap.Logger, directive, scope, path string, line int) {
	switch {
	case nearMissKeyword(directive, scope) != "":
		logger.Sugar().Warnw("unrecognized directive looks like a misspelled keyword; skipping (it will not take effect)",
			"directive", directive, "suggestion", nearMissKeyword(directive, scope), "scope", scope, "line", line, "file", path)
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

// withinEditDistance1 reports whether a and b differ by at most one single-edit
// operation: one insertion, deletion, substitution, or adjacent transposition
// (Damerau). It is used only to flag a skipped directive that is almost certainly
// a misspelled structural keyword (e.g. "veiw" → "view", "zoen" → "zone").
func withinEditDistance1(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := len(a), len(b)
	switch {
	case la == lb:
		// Same length: at most one substitution, or one adjacent transposition.
		firstDiff, mismatches := -1, 0
		for i := 0; i < la; i++ {
			if a[i] == b[i] {
				continue
			}
			mismatches++
			switch mismatches {
			case 1:
				firstDiff = i
			case 2:
				// Accept an adjacent swap: a[firstDiff]a[i] == b[i]b[firstDiff].
				if i != firstDiff+1 || a[firstDiff] != b[i] || a[i] != b[firstDiff] {
					return false
				}
			default:
				return false
			}
		}
		return true
	case la+1 == lb:
		return isOneDeletionApart(a, b) // b has one extra byte
	case la == lb+1:
		return isOneDeletionApart(b, a) // a has one extra byte
	default:
		return false
	}
}

// isOneDeletionApart reports whether the longer string becomes short by deleting
// exactly one byte. short is the shorter of the two strings.
func isOneDeletionApart(short, long string) bool {
	i, j, skipped := 0, 0, false
	for i < len(short) && j < len(long) {
		if short[i] == long[j] {
			i++
			j++
			continue
		}
		if skipped {
			return false
		}
		skipped = true
		j++ // consume the extra byte in long
	}
	return true
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

	cfg := &Config{Path: path, ACLs: make(map[string][]Element)}

	if err := loadFile(path, cfg, logger); err != nil {
		return nil, err
	}

	// Resolve named-acl references now that every file (and therefore every acl
	// definition) has been loaded. Undefined and cyclic references are dropped
	// fail-closed with a WARN; defined references gain a pointer to their target
	// element list so the query hot path does no map lookups.
	cfg.resolveACLReferences(logger)

	// Resolve top-level zones: reject mixing with explicit views, otherwise
	// synthesize the implicit _default view. This must run before
	// warnShadowedViews so that shadow-warning sees the final view list.
	if err := cfg.resolveTopLevelZones(logger); err != nil {
		return nil, err
	}

	// Resolve relative zone file paths now that every options{} block (including
	// any in a file included after the zones) has been applied. All zones —
	// in-view and the folded top-level ones — live under cfg.Views at this point.
	cfg.resolveZoneFilePaths()

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
	// `any` element.
	anyRules, err := ParseMatchClients([]byte("any;"), first.Source, first.Line)
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

// resolveZoneFilePaths resolves each zone's relative `file` path against the
// final options{} directory, after the whole include tree (and therefore every
// options block) has loaded. Resolving here rather than at parse time removes an
// ordering trap: a zone declared before the options block (e.g. an options file
// included after the zones) would otherwise bind to the wrong base directory. An
// absolute path is left untouched; a zone with no file (missing-file tolerance)
// is skipped. The fallback base when no directory is configured is the directory
// of the file that declared the zone (z.Source), preserving the prior behavior.
func (cfg *Config) resolveZoneFilePaths() {
	for vi := range cfg.Views {
		for zi := range cfg.Views[vi].Zones {
			z := &cfg.Views[vi].Zones[zi]
			if z.File == "" || filepath.IsAbs(z.File) {
				continue
			}
			baseDir := cfg.Options.Directory
			if baseDir == "" {
				baseDir = filepath.Dir(z.Source)
			}
			z.File = filepath.Join(baseDir, z.File)
		}
	}
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

		case "acl":
			// Top-level `acl "<name>" { <address-match-list>; };` — parsed and
			// stored in the named registry (Change A skipped it). Its body uses
			// the same grammar as match-clients.
			if err := parseACL(lx, cfg, path, logger); err != nil {
				return err
			}

		case "view":
			v, err := parseView(lx, path, logger)
			if err != nil {
				return err
			}
			cfg.Views = append(cfg.Views, v)

		case "zone":
			// Top-level zone (outside any view). Reuse parseZone so zone-body
			// rules (relative-path resolution against cfg.Options.Directory,
			// missing type/file tolerance) match in-view zones exactly.
			// Accumulate for post-processing in LoadNamedConf.
			z, err := parseZone(lx, path)
			if err != nil {
				return err
			}
			// A zone with an explicit non-master type is dropped (not served,
			// file never opened), matching the in-view rule. Common in
			// named.conf.default-zones (the root `type hint` zone).
			if zoneIsSkipped(z) {
				logSkippedZone(logger, z, topLevelScope, path)
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
			logSkippedDirective(logger, directive, topLevelScope, path, tok.line)
			if err := lx.skipNamedDirective(path); err != nil {
				return err
			}
		}
	}

	return nil
}

// parseView parses a `view "name" { ... };` block. The lexer has just consumed
// the "view" keyword.
func parseView(lx *lexer, path string, logger *zap.Logger) (View, error) {
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
			// Named-acl references, `!` negation, and nested groups are now
			// parsed into elements. Undefined references are dropped fail-closed
			// (with a WARN) at the build-phase resolve step, not here.
			rules, err := ParseMatchClients(body, path, startLine)
			if err != nil {
				return View{}, err
			}
			v.MatchClients = rules

		case "zone":
			z, err := parseZone(lx, path)
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
func parseZone(lx *lexer, path string) (Zone, error) {
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
			// Store the path verbatim. Relative paths are resolved later, in
			// resolveZoneFilePaths, against the FINAL options{} directory once the
			// whole include tree has loaded. Resolving here would bind the path to
			// a stale directory when the options block is declared (or included)
			// after this zone.
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
// view that contains a top-level `any` element (because subsequent views would
// be unreachable).
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

// viewHasAny reports whether a view's MatchClients list contains a top-level,
// non-negated `any` element (the shadowing heuristic: such a view accepts every
// client, so any later view is unreachable). It intentionally inspects only the
// top level — references and nested groups are not unwound for this warning.
func viewHasAny(v View) bool {
	for _, el := range v.MatchClients {
		if el.Kind == ElemAny && !el.Negated {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Named acl: parsing and reference resolution
// ---------------------------------------------------------------------------

// parseACL parses a top-level `acl "<name>" { <address-match-list>; };` block
// and stores the parsed element list in cfg.ACLs. The lexer has just consumed
// the "acl" keyword. A duplicate acl name keeps the last definition and logs a
// WARN naming the acl (mirroring the duplicate-zone tolerance).
func parseACL(lx *lexer, cfg *Config, path string, logger *zap.Logger) error {
	nameTok := lx.next()
	if nameTok.kind != tokenString && nameTok.kind != tokenWord {
		return fmt.Errorf("%s:%d: expected acl name, got %q", path, nameTok.line, nameTok.value)
	}
	name := nameTok.value

	body, startLine, err := readBracedBodyRaw(lx, path)
	if err != nil {
		return err
	}
	// Consume the trailing ';' after the closing '}'.
	if lx.peek().kind == tokenSemicolon {
		lx.next()
	}

	elems, err := ParseMatchClients(body, path, startLine)
	if err != nil {
		return err
	}

	if _, exists := cfg.ACLs[name]; exists {
		logger.Sugar().Warnw("duplicate acl definition; the last definition takes effect",
			"acl", name, "line", nameTok.line, "file", path)
	}
	cfg.ACLs[name] = elems
	return nil
}

// resolveACLReferences resolves every named-acl reference (in acl bodies and in
// view match-clients) to its target element list, after all files have loaded.
// Undefined names are dropped (fail-closed, WARN); reference cycles are broken
// (WARN); defined references gain a Sub pointer so the query hot path does no
// map lookups.
func (cfg *Config) resolveACLReferences(logger *zap.Logger) {
	r := &aclResolver{registry: cfg.ACLs, state: make(map[string]int8, len(cfg.ACLs)), logger: logger}
	// Resolve in sorted name order so the edge cut to break a reference cycle is
	// deterministic — map iteration order would otherwise vary the resolved
	// structure (and its WARN logs) run-to-run for the same named.conf.
	names := make([]string, 0, len(cfg.ACLs))
	for name := range cfg.ACLs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r.resolveACL(name)
	}
	for i := range cfg.Views {
		cfg.Views[i].MatchClients = r.resolveList(cfg.Views[i].MatchClients, cfg.Views[i].Name)
	}
}

// aclResolver drives reference resolution with cycle detection.
type aclResolver struct {
	registry map[string][]Element
	state    map[string]int8 // 0 unvisited, 1 resolving, 2 done
	logger   *zap.Logger
}

// resolveACL resolves the named acl's element list in place (storing the final,
// filtered list back into the registry), so a later reference can point at it.
func (r *aclResolver) resolveACL(name string) {
	if r.state[name] != 0 {
		return // done, or currently resolving (cycle handled at the ref site)
	}
	r.state[name] = 1
	r.registry[name] = r.resolveList(r.registry[name], name)
	r.state[name] = 2
}

// rejectAllElement is the fail-closed replacement for a negated element that can
// never fire — an undefined or cyclic reference, a reference to an acl that
// resolved to empty, or a group whose entire content was dropped. A negated
// element is an exclusion; simply dropping it would WIDEN
// the list (a following `any` would then accept everyone), violating the
// matcher's "a reduced rule set can only narrow, never widen" contract. `!any`
// always fires and rejects, so it narrows the list to match no client.
var rejectAllElement = Element{Kind: ElemAny, Negated: true}

// resolveList resolves references and nested groups within elems, returning a
// filtered list. A POSITIVE undefined/cyclic reference is dropped (fail-closed:
// it never matches). A NEGATED undefined/cyclic reference — a negated reference
// to an acl that resolved to empty — or a negated group reduced to empty — is
// replaced by rejectAllElement instead of being dropped, so the lost exclusion
// narrows rather than widens the list. scope names the enclosing acl or view for
// the WARN.
func (r *aclResolver) resolveList(elems []Element, scope string) []Element {
	if !listNeedsResolution(elems) {
		// No references or nested groups: nothing to resolve, no element to drop
		// or replace. Return the input untouched to avoid a needless copy.
		return elems
	}
	var out []Element
	for _, el := range elems {
		switch el.Kind {
		case ElemRef:
			if _, ok := r.registry[el.RefName]; !ok {
				out = appendUnevaluableRef(out, el, scope, "reference to undefined acl", r.logger)
				continue
			}
			if r.state[el.RefName] == 1 {
				out = appendUnevaluableRef(out, el, scope, "acl reference cycle", r.logger)
				continue
			}
			r.resolveACL(el.RefName)
			el.Sub = r.registry[el.RefName]
			if el.Negated && len(el.Sub) == 0 {
				// A negated reference to an acl that resolved to empty — whether
				// declared empty (`acl x {};`) or emptied because its own members
				// were dropped fail-closed — can never fire. Dropping the
				// exclusion would WIDEN the list (a following `any` would then
				// accept everyone), so keep it fail-closed exactly like a negated
				// empty group below (see rejectAllElement).
				r.logger.Sugar().Warnw("negated match-clients reference to an empty acl cannot exclude anything; rejecting the whole list (fail-closed)",
					"token", el.RefName, "scope", scope)
				out = append(out, rejectAllElement)
				continue
			}
			out = append(out, el)
		case ElemGroup:
			el.Sub = r.resolveList(el.Sub, scope)
			if el.Negated && len(el.Sub) == 0 {
				// A negated group reduced to empty can never fire; keep the
				// exclusion fail-closed instead of letting it widen the list.
				out = append(out, rejectAllElement)
				continue
			}
			out = append(out, el)
		default:
			out = append(out, el)
		}
	}
	return out
}

// listNeedsResolution reports whether elems contains any reference or nested
// group that the resolver must rewrite. Lists of plain leaves/built-ins (the
// common case) need no resolution and can be returned as-is.
func listNeedsResolution(elems []Element) bool {
	for _, el := range elems {
		if el.Kind == ElemRef || el.Kind == ElemGroup {
			return true
		}
	}
	return false
}

// appendUnevaluableRef logs a dropped reference and appends its fail-closed
// replacement (if any) to out. A positive reference is dropped (nothing
// appended); a negated reference is replaced by rejectAllElement so the lost
// exclusion narrows the list rather than widening it. reason is the human phrase
// for the WARN ("reference to undefined acl" / "acl reference cycle").
func appendUnevaluableRef(out []Element, el Element, scope, reason string, logger *zap.Logger) []Element {
	if el.Negated {
		logger.Sugar().Warnw("negated match-clients "+reason+" cannot be evaluated; rejecting the whole list (fail-closed)",
			"token", el.RefName, "scope", scope)
		return append(out, rejectAllElement)
	}
	logger.Sugar().Warnw("dropping match-clients "+reason+" (fail-closed: it never matches)",
		"token", el.RefName, "scope", scope)
	return out
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
