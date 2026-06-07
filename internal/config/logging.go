package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// QueryLogConfig holds the parsed query-log settings extracted from a named.conf
// logging{} block. A nil pointer means query logging is disabled.
type QueryLogConfig struct {
	// FilePath is the resolved log file path. Relative paths are joined with the
	// options{} directory value, consistent with zone file path resolution.
	FilePath string
	// PrintTime is one of: "yes" | "no" | "local" | "iso8601" | "iso8601-utc".
	PrintTime string
	// PrintCategory controls whether the "queries: " category label is emitted.
	PrintCategory bool
	// PrintSeverity controls whether the "info: " severity label is emitted.
	PrintSeverity bool
	// RotationIgnored is true when the file clause carried versions or size
	// parameters. A startup warning should be emitted in that case.
	RotationIgnored bool
}

// builtinChannels is the set of BIND built-in channel names that target
// non-file destinations. Referencing them in category queries silently disables
// query logging (they are not configuration errors).
var builtinChannels = map[string]bool{
	"null":           true,
	"default_syslog": true,
	"default_stderr": true,
	"default_debug":  true,
}

// disabledSeverities are severity values that are stricter than info.
// Channels using these disable query logging with a warning.
var disabledSeverities = map[string]bool{
	"notice":   true,
	"warning":  true,
	"error":    true,
	"critical": true,
}

// channelDest describes the destination type of a parsed channel.
type channelDest int

const (
	destUnknown channelDest = iota
	destFile
	destSyslog
	destStderr
	destNull
)

// channelInfo records what we know about a parsed channel declaration.
type channelInfo struct {
	dest            channelDest
	filePath        string // set when dest == destFile
	rotationIgnored bool   // true if versions or size appeared in the file clause
	printTime       string // "yes" | "no" | "local" | "iso8601" | "iso8601-utc"
	printCategory   bool
	printSeverity   bool
	severityStr     string // raw severity value, empty means not set
}

// ParseLogging parses a top-level `logging { ... };` block from input, starting
// at startOffset (which must point to the "logging" keyword). It returns the
// query-log configuration (or nil if query logging should be disabled), the byte
// offset just past the closing `};` (mirroring ParseOptions' signature), a
// human-readable disabled reason (non-empty only when cfg is nil and no error
// occurred — i.e. logging was explicitly disabled by configuration), and any
// error. The reason is empty on error.
//
// opts supplies the options{} directory for relative file-path resolution.
// logger must not be nil; pass zap.NewNop() if logging is undesired.
func ParseLogging(input []byte, startOffset int, path string, opts OptionsBlock, logger *zap.Logger) (*QueryLogConfig, int, string, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	lx := newLexer(input, startOffset)

	// Consume the "logging" keyword.
	kw := lx.next()
	if kw.kind == tokenEOF {
		return nil, startOffset, "", fmt.Errorf("%s:%d: unexpected end of input, expected 'logging'", path, kw.line)
	}
	if (kw.kind != tokenWord && kw.kind != tokenString) || !strings.EqualFold(kw.value, "logging") {
		return nil, startOffset, "", fmt.Errorf("%s:%d: expected 'logging' keyword, got %q", path, kw.line, kw.value)
	}

	// Consume the opening '{'.
	open := lx.next()
	if open.kind != tokenLBrace {
		return nil, startOffset, "", fmt.Errorf("%s:%d: expected '{' after 'logging', got %q", path, open.line, open.value)
	}

	channels := map[string]*channelInfo{}
	var queriesChannelNames []string

	// Parse the logging block body.
	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return nil, startOffset, "", fmt.Errorf("%s:%d: unterminated logging block", path, tok.line)
		}
		if tok.kind == tokenRBrace {
			// Consume optional ';' after '}'.
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			break
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return nil, startOffset, "", fmt.Errorf("%s:%d: unexpected token %q in logging block", path, tok.line, tok.value)
		}

		directive := strings.ToLower(tok.value)
		switch directive {
		case "channel":
			name, ch, err := parseChannel(lx, path, logger)
			if err != nil {
				return nil, startOffset, "", err
			}
			channels[name] = ch

		case "category":
			catName, chanNames, err := parseCategory(lx, path)
			if err != nil {
				return nil, startOffset, "", err
			}
			if strings.ToLower(catName) == "queries" {
				queriesChannelNames = chanNames
			} else {
				logger.Sugar().Warnw("non-queries category ignored",
					"category", catName,
					"file", path,
					"line", tok.line,
				)
			}

		default:
			logger.Sugar().Warnw("unknown directive in logging block, skipping",
				"directive", tok.value,
				"file", path,
				"line", tok.line,
			)
			if err := lx.skipOptionValue(path); err != nil {
				return nil, startOffset, "", err
			}
		}
	}

	endOff := lx.offset()

	// No category queries → disabled, silent.
	if len(queriesChannelNames) == 0 {
		return nil, endOff, "no category queries{} block", nil //nolint:nilnil
	}

	// Resolve channels listed in category queries, picking the first file channel.
	var chosen *channelInfo
	var extraIgnored []string

	for _, chName := range queriesChannelNames {
		chNameLower := strings.ToLower(chName)

		// Built-in channels: silent disable.
		if builtinChannels[chNameLower] {
			if chosen == nil {
				return nil, endOff, "null or built-in channel in category queries{}", nil //nolint:nilnil
			}
			extraIgnored = append(extraIgnored, chName)
			continue
		}

		ch, ok := channels[chName]
		if !ok {
			// Named channel not declared — warn and skip.
			logger.Sugar().Warnw("category queries references undeclared channel, ignoring",
				"channel", chName,
				"file", path,
			)
			continue
		}

		if chosen != nil {
			// Already have a file channel — note the extra for warning below.
			extraIgnored = append(extraIgnored, chName)
			continue
		}

		switch ch.dest {
		case destFile:
			chosen = ch
		case destNull:
			// User-declared channel whose destination is `null;` — distinct
			// from the built-in channel *name* "null" handled above, but the
			// same silent-disable outcome.
			return nil, endOff, "null or built-in channel in category queries{}", nil //nolint:nilnil
		case destSyslog, destStderr:
			// User-defined non-file channel: warn and disable.
			logger.Sugar().Warnw(
				"channel in category queries does not write to a file; query logging disabled",
				"channel", chName,
				"file", path,
			)
			return nil, endOff, "non-file channel in category queries{}", nil //nolint:nilnil
		default:
			// Unknown destination: warn and disable.
			logger.Sugar().Warnw(
				"channel in category queries has unknown destination type; query logging disabled — use a file channel",
				"channel", chName,
				"file", path,
			)
			return nil, endOff, "non-file channel in category queries{}", nil //nolint:nilnil
		}
	}

	if chosen == nil {
		// No usable file channel found (e.g., only undeclared channels listed).
		return nil, endOff, "no usable file channel in category queries{}", nil //nolint:nilnil
	}

	// Emit warnings for extra ignored channels.
	if len(extraIgnored) > 0 {
		logger.Sugar().Warnw(
			"category queries lists multiple channels; only the first file channel is used, rest are ignored",
			"ignored", strings.Join(extraIgnored, ", "),
			"file", path,
		)
	}

	// Check severity. Empty severityStr means absent → treated as info (enabled).
	if chosen.severityStr != "" {
		sevLower := strings.ToLower(chosen.severityStr)
		if disabledSeverities[sevLower] {
			logger.Sugar().Warnw(
				"channel severity is stricter than info; query logging disabled",
				"severity", chosen.severityStr,
				"file", path,
			)
			return nil, endOff, "channel severity stricter than info", nil //nolint:nilnil
		}
		// Accepted values: info, debug (with optional level), dynamic.
	}

	// Resolve file path, joining relative paths with options directory.
	fp := chosen.filePath
	if !filepath.IsAbs(fp) {
		baseDir := opts.Directory
		if baseDir != "" {
			fp = filepath.Join(baseDir, fp)
		}
	}

	return &QueryLogConfig{
		FilePath: fp,
		// printTime defaults to "yes" in parseChannel's channelInfo literal
		// and is validated there, so it is always one of the five canonical
		// values here.
		PrintTime:       chosen.printTime,
		PrintCategory:   chosen.printCategory,
		PrintSeverity:   chosen.printSeverity,
		RotationIgnored: chosen.rotationIgnored,
	}, endOff, "", nil
}

// parseChannel parses a `channel <name> { ... };` block. The lexer has already
// consumed the "channel" keyword. Returns the channel name and its info.
func parseChannel(lx *lexer, path string, logger *zap.Logger) (string, *channelInfo, error) {
	// Expect channel name.
	nameTok := lx.next()
	if nameTok.kind != tokenWord && nameTok.kind != tokenString {
		return "", nil, fmt.Errorf("%s:%d: expected channel name, got %q", path, nameTok.line, nameTok.value)
	}
	chName := nameTok.value

	// Expect '{'.
	open := lx.next()
	if open.kind != tokenLBrace {
		return "", nil, fmt.Errorf("%s:%d: expected '{' after channel name %q, got %q", path, open.line, chName, open.value)
	}

	ch := &channelInfo{
		printTime: "yes", // BIND default
	}

	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return "", nil, fmt.Errorf("%s:%d: unterminated channel block for %q", path, tok.line, chName)
		}
		if tok.kind == tokenRBrace {
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			break
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return "", nil, fmt.Errorf("%s:%d: unexpected token %q in channel %q", path, tok.line, tok.value, chName)
		}

		key := strings.ToLower(tok.value)
		switch key {
		case "file":
			fp, rotIgnored, err := parseFileClause(lx, path)
			if err != nil {
				return "", nil, err
			}
			ch.dest = destFile
			ch.filePath = fp
			ch.rotationIgnored = rotIgnored

		case "syslog":
			ch.dest = destSyslog
			// Consume optional facility token and the terminating ';'.
			if err := lx.skipOptionValue(path); err != nil {
				return "", nil, err
			}

		case "stderr":
			ch.dest = destStderr
			// stderr is a standalone keyword followed by ';'.
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return "", nil, fmt.Errorf("%s:%d: expected ';' after 'stderr', got %q", path, semi.line, semi.value)
			}

		case "null":
			ch.dest = destNull
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return "", nil, fmt.Errorf("%s:%d: expected ';' after 'null', got %q", path, semi.line, semi.value)
			}

		case "severity":
			sev, err := parseSeverity(lx, path)
			if err != nil {
				return "", nil, err
			}
			ch.severityStr = sev

		case "print-time":
			val, err := lx.readScalarValue(path)
			if err != nil {
				return "", nil, err
			}
			// Validate here so downstream consumers (querylog formatter) can
			// rely on the field holding one of the five canonical values.
			switch v := strings.ToLower(val); v {
			case "yes", "no", "local", "iso8601", "iso8601-utc":
				ch.printTime = v
			default:
				logger.Sugar().Warnw("unrecognized print-time value, keeping default \"yes\"",
					"value", val,
					"channel", chName,
					"file", path,
				)
			}

		case "print-category":
			val, err := lx.readScalarValue(path)
			if err != nil {
				return "", nil, err
			}
			ch.printCategory = strings.EqualFold(val, "yes")

		case "print-severity":
			val, err := lx.readScalarValue(path)
			if err != nil {
				return "", nil, err
			}
			ch.printSeverity = strings.EqualFold(val, "yes")

		default:
			logger.Sugar().Warnw("unknown parameter in channel block, skipping",
				"channel", chName,
				"param", tok.value,
				"file", path,
				"line", tok.line,
			)
			if err := lx.skipOptionValue(path); err != nil {
				return "", nil, err
			}
		}
	}

	return chName, ch, nil
}

// parseFileClause parses the `file "<path>" [versions N] [size M];` clause.
// The lexer has already consumed the "file" keyword. Returns the path and
// whether rotation parameters (versions/size) were present.
func parseFileClause(lx *lexer, path string) (string, bool, error) {
	// Expect file path (quoted string or word).
	pathTok := lx.next()
	if pathTok.kind != tokenString && pathTok.kind != tokenWord {
		return "", false, fmt.Errorf("%s:%d: expected file path after 'file', got %q", path, pathTok.line, pathTok.value)
	}
	filePath := pathTok.value
	rotationIgnored := false

	// Consume optional versions/size parameters and the terminating ';'.
	for {
		tok := lx.next()
		if tok.kind == tokenSemicolon {
			break
		}
		if tok.kind == tokenEOF {
			return "", false, fmt.Errorf("%s:%d: unterminated file clause", path, tok.line)
		}
		if tok.kind == tokenRBrace {
			return "", false, fmt.Errorf("%s:%d: unexpected '}' in file clause", path, tok.line)
		}
		// Any token before the terminating ';' is a rotation parameter keyword or value.
		rotationIgnored = true
	}

	return filePath, rotationIgnored, nil
}

// parseSeverity reads the severity value (and optional numeric level) followed
// by ';'. Returns the primary severity keyword.
func parseSeverity(lx *lexer, path string) (string, error) {
	tok := lx.next()
	if tok.kind != tokenWord && tok.kind != tokenString {
		return "", fmt.Errorf("%s:%d: expected severity value, got %q", path, tok.line, tok.value)
	}
	sev := tok.value

	// Check for optional numeric level (e.g., "debug 3"). Only consume the
	// extra token if it looks like a number (word consisting of digits) to
	// avoid accidentally consuming the next directive keyword.
	peek := lx.peek()
	if peek.kind == tokenWord && isDigits(peek.value) {
		lx.next() // consume the level number
		semi := lx.next()
		if semi.kind != tokenSemicolon {
			return "", fmt.Errorf("%s:%d: expected ';' after severity level, got %q", path, semi.line, semi.value)
		}
		return sev, nil
	}

	// Expect ';'.
	semi := lx.next()
	if semi.kind != tokenSemicolon {
		return "", fmt.Errorf("%s:%d: expected ';' after severity %q, got %q", path, tok.line, sev, semi.value)
	}
	return sev, nil
}

// isDigits reports whether s consists entirely of ASCII decimal digits.
func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseCategory parses a `category <name> { chan; ... };` block. The lexer has
// already consumed the "category" keyword. Returns the category name and the
// list of referenced channel names.
func parseCategory(lx *lexer, path string) (string, []string, error) {
	// Expect category name.
	nameTok := lx.next()
	if nameTok.kind != tokenWord && nameTok.kind != tokenString {
		return "", nil, fmt.Errorf("%s:%d: expected category name, got %q", path, nameTok.line, nameTok.value)
	}
	catName := nameTok.value

	// Expect '{'.
	open := lx.next()
	if open.kind != tokenLBrace {
		return "", nil, fmt.Errorf("%s:%d: expected '{' after category name %q, got %q", path, open.line, catName, open.value)
	}

	var chanNames []string
	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return "", nil, fmt.Errorf("%s:%d: unterminated category block for %q", path, tok.line, catName)
		}
		if tok.kind == tokenRBrace {
			if lx.peek().kind == tokenSemicolon {
				lx.next()
			}
			break
		}
		if tok.kind == tokenSemicolon {
			continue
		}
		if tok.kind != tokenWord && tok.kind != tokenString {
			return "", nil, fmt.Errorf("%s:%d: unexpected token %q in category %q", path, tok.line, tok.value, catName)
		}
		chanNames = append(chanNames, tok.value)
		// Consume the ';' after the channel name.
		semi := lx.next()
		if semi.kind != tokenSemicolon {
			return "", nil, fmt.Errorf("%s:%d: expected ';' after channel name %q, got %q", path, tok.line, tok.value, semi.value)
		}
	}

	return catName, chanNames, nil
}
