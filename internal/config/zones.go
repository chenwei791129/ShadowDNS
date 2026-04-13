package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	Path    string       // the named.conf path
	Options OptionsBlock // parsed options block
	Views   []View       // in declaration order across all included files
}

// ---------------------------------------------------------------------------
// Top-level directives rejected at startup (case-insensitive).
// ---------------------------------------------------------------------------

var rejectedTopLevel = map[string]bool{
	"dnssec-enable": true,
	"allow-update":  true,
	"controls":      true,
	"key":           true,
	"acl":           true,
	"server":        true,
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
// logger MUST NOT be nil; the caller passes slog.Default() if needed.
//
// MUST NOT panic on any input.
func LoadNamedConf(path string, logger *slog.Logger) (*Config, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cfg := &Config{Path: path}

	if err := loadFile(path, cfg, logger); err != nil {
		return nil, err
	}

	// Warn when a non-last view uses `any` (subsequent views would be unreachable).
	warnShadowedViews(cfg.Views, logger)

	return cfg, nil
}

// loadFile reads a single file (named.conf or any included file) and appends
// parsed views / options into cfg.
func loadFile(path string, cfg *Config, logger *slog.Logger) error {
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
			if cfg.Path == path {
				// Only apply options from the root named.conf (standard BIND behaviour).
				cfg.Options = block
			}
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

		case "logging":
			// logging { ... }; — skip silently.
			if err := lx.skipBalancedBraceBlock(path); err != nil {
				return err
			}

		default:
			// Check reject list (task 2.6).
			if rejectedTopLevel[directive] {
				return fmt.Errorf("%s:%d: unsupported directive %q at top level", path, tok.line, tok.value)
			}
			// Unknown directive — return error (could be extended later).
			return fmt.Errorf("%s:%d: unsupported directive %q at top level", path, tok.line, tok.value)
		}
	}

	return nil
}

// parseView parses a `view "name" { ... };` block. The lexer has just consumed
// the "view" keyword.
func parseView(lx *lexer, path string, opts OptionsBlock, logger *slog.Logger) (View, error) {
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
			rules, err := ParseMatchClients(body, path, startLine)
			if err != nil {
				return View{}, err
			}
			v.MatchClients = rules

		case "zone":
			z, err := parseZone(lx, path, opts)
			if err != nil {
				return View{}, err
			}
			v.Zones = append(v.Zones, z)

		case "recursion":
			// Accepted but ignored (global authoritative-only mode).
			val := lx.next()
			if val.kind == tokenEOF {
				return View{}, fmt.Errorf("%s:%d: unexpected EOF reading recursion value", path, val.line)
			}
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return View{}, fmt.Errorf("%s:%d: expected ';' after recursion value, got %q", path, val.line, semi.value)
			}
			logger.Debug("recursion directive inside view ignored", slog.String("view", viewName), slog.String("file", path))

		default:
			return View{}, fmt.Errorf("%s:%d: unsupported directive %q inside view %q", path, tok.line, tok.value, viewName)
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
			// Only "master" zone type is supported.
			if zoneType != ZoneTypeMaster {
				return Zone{}, fmt.Errorf("%s:%d: zone %q has unsupported type %q (only 'master' is supported)", path, tok.line, zoneName, zoneType)
			}
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
			// Skip unknown zone directives (e.g. forwarders after type forward
			// may appear — but the type check above will have already errored).
			// For other unknowns, skip until ';' or balanced block.
			if err := lx.skipOptionValue(path); err != nil {
				return Zone{}, err
			}
		}
	}

	return z, nil
}

// warnShadowedViews inspects the view list and emits a Warn log for any non-last
// view that contains an AnyRule (because subsequent views would be unreachable).
func warnShadowedViews(views []View, logger *slog.Logger) {
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
			logger.Warn(
				"view has match-clients 'any' but is not the last view; subsequent views are shadowed",
				slog.String("view", v.Name),
				slog.String("shadowed_views", strings.Join(shadowed, ", ")),
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
