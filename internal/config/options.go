// Package config provides named.conf parsing utilities for ShadowDNS.
package config

import (
	"fmt"
	"log/slog"
	"strings"
)

// OptionsBlock holds the parsed `options { ... }` directives that ShadowDNS understands.
type OptionsBlock struct {
	Directory        string
	GeoIPDirectory   string
	ListenOn         []string // raw tokens from listen-on { ... }; e.g. ["any"] or ["192.0.2.1"]
	ListenOnV6       []string
	AllowTransfer    []string // raw IP/CIDR strings, e.g. ["192.0.2.10", "192.0.2.11"]
	Recursion        bool     // false when "recursion no;"
	MinimalResponses bool     // true when "minimal-responses yes;"
	Version          string   // "none" or quoted string content
	Hostname         string
	TransferFormat   string // e.g. "many-answers"
	PidFile          string // path to PID file; empty means no PID file
}

// ParseOptions parses an `options { ... };` block from the input, starting at the
// position of the `options` keyword. It returns the parsed block, the byte position
// just past the closing `};`, and any error.
//
// `path` is used purely for error messages (file path).
// `logger` is used to emit warnings for unknown options. Pass `slog.Default()` if nil.
//
// The function MUST not panic on any input.
func ParseOptions(input []byte, startOffset int, path string, logger *slog.Logger) (block OptionsBlock, endOffset int, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	lx := newLexer(input, startOffset)

	// Consume the "options" keyword.
	tok := lx.next()
	if tok.kind == tokenEOF {
		return block, startOffset, fmt.Errorf("%s:%d: unexpected end of input, expected 'options'", path, tok.line)
	}
	if tok.kind != tokenWord || tok.value != "options" {
		return block, startOffset, fmt.Errorf("%s:%d: expected 'options' keyword, got %q", path, tok.line, tok.value)
	}

	// Consume the opening '{'.
	tok = lx.next()
	if tok.kind != tokenLBrace {
		return block, startOffset, fmt.Errorf("%s:%d: expected '{' after 'options', got %q", path, tok.line, tok.value)
	}

	// Parse key-value pairs until the matching '}'.
	for {
		tok = lx.next()
		if tok.kind == tokenEOF {
			return block, startOffset, fmt.Errorf("%s:%d: unterminated options block, expected '}'", path, tok.line)
		}
		if tok.kind == tokenRBrace {
			// Closing brace of options block — consume optional ';'.
			peek := lx.peek()
			if peek.kind == tokenSemicolon {
				lx.next()
			}
			return block, lx.offset(), nil
		}
		if tok.kind != tokenWord {
			return block, startOffset, fmt.Errorf("%s:%d: unexpected token %q in options block", path, tok.line, tok.value)
		}

		key := tok.value
		keyLine := tok.line

		switch key {
		case "directory":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			block.Directory = val

		case "geoip-directory":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			block.GeoIPDirectory = val

		case "listen-on":
			tokens, e := lx.readBracedList(path)
			if e != nil {
				return block, startOffset, e
			}
			block.ListenOn = tokens

		case "listen-on-v6":
			tokens, e := lx.readBracedList(path)
			if e != nil {
				return block, startOffset, e
			}
			block.ListenOnV6 = tokens

		case "allow-transfer":
			tokens, e := lx.readBracedList(path)
			if e != nil {
				return block, startOffset, e
			}
			block.AllowTransfer = tokens

		case "recursion":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			switch strings.ToLower(val) {
			case "yes":
				block.Recursion = true
			case "no":
				block.Recursion = false
			default:
				return block, startOffset, fmt.Errorf("%s:%d: invalid value %q for 'recursion', expected yes/no", path, keyLine, val)
			}

		case "minimal-responses":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			switch strings.ToLower(val) {
			case "yes":
				block.MinimalResponses = true
			case "no":
				block.MinimalResponses = false
			default:
				return block, startOffset, fmt.Errorf("%s:%d: invalid value %q for 'minimal-responses', expected yes/no", path, keyLine, val)
			}

		case "version":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			block.Version = val

		case "hostname":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			block.Hostname = val

		case "transfer-format":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			block.TransferFormat = val

		case "pid-file":
			val, e := lx.readScalarValue(path)
			if e != nil {
				return block, startOffset, e
			}
			block.PidFile = val

		default:
			// Unknown option: emit warning and skip until next ';' or balanced '{ };'.
			logger.Warn("unknown option in options block",
				slog.String("option", key),
				slog.Int("line", keyLine),
				slog.String("file", path),
			)
			if e := lx.skipOptionValue(path); e != nil {
				return block, startOffset, e
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenWord
	tokenString // quoted string — value is content without quotes
	tokenLBrace // {
	tokenRBrace // }
	tokenSemicolon
)

type token struct {
	kind  tokenKind
	value string
	line  int
}

type lexer struct {
	input []byte
	pos   int // current read position in input
	line  int // 1-based current line
}

func newLexer(input []byte, startOffset int) *lexer {
	// Count lines up to startOffset so line numbers are accurate.
	line := 1
	for i := 0; i < startOffset && i < len(input); i++ {
		if input[i] == '\n' {
			line++
		}
	}
	return &lexer{input: input, pos: startOffset, line: line}
}

// offset returns the current byte position in input.
func (lx *lexer) offset() int {
	return lx.pos
}

// peek returns the next token without consuming it.
func (lx *lexer) peek() token {
	saved := lx.pos
	savedLine := lx.line
	tok := lx.next()
	lx.pos = saved
	lx.line = savedLine
	return tok
}

// next returns the next meaningful token, skipping whitespace and comments.
func (lx *lexer) next() token {
	for {
		lx.skipWhitespace()
		if lx.pos >= len(lx.input) {
			return token{kind: tokenEOF, line: lx.line}
		}
		ch := lx.input[lx.pos]

		// Comments.
		if ch == '/' && lx.pos+1 < len(lx.input) {
			next := lx.input[lx.pos+1]
			if next == '/' {
				lx.skipLineComment()
				continue
			}
			if next == '*' {
				lx.skipBlockComment()
				continue
			}
		}
		if ch == '#' {
			lx.skipLineComment()
			continue
		}

		if ch == '{' {
			lx.pos++
			return token{kind: tokenLBrace, value: "{", line: lx.line}
		}
		if ch == '}' {
			lx.pos++
			return token{kind: tokenRBrace, value: "}", line: lx.line}
		}
		if ch == ';' {
			lx.pos++
			return token{kind: tokenSemicolon, value: ";", line: lx.line}
		}
		if ch == '"' {
			return lx.readQuotedString()
		}
		// Everything else is a word token.
		return lx.readWord()
	}
}

func (lx *lexer) skipWhitespace() {
	for lx.pos < len(lx.input) {
		ch := lx.input[lx.pos]
		switch ch {
		case '\n':
			lx.line++
			lx.pos++
		case ' ', '\t', '\r':
			lx.pos++
		default:
			return
		}
	}
}

func (lx *lexer) skipLineComment() {
	for lx.pos < len(lx.input) && lx.input[lx.pos] != '\n' {
		lx.pos++
	}
}

func (lx *lexer) skipBlockComment() {
	lx.pos += 2 // skip /*
	for lx.pos+1 < len(lx.input) {
		if lx.input[lx.pos] == '\n' {
			lx.line++
		}
		if lx.input[lx.pos] == '*' && lx.input[lx.pos+1] == '/' {
			lx.pos += 2
			return
		}
		lx.pos++
	}
	// Unclosed block comment: advance to end.
	lx.pos = len(lx.input)
}

func (lx *lexer) readQuotedString() token {
	startLine := lx.line
	lx.pos++ // skip opening "
	var sb strings.Builder
	for lx.pos < len(lx.input) {
		ch := lx.input[lx.pos]
		if ch == '"' {
			lx.pos++ // skip closing "
			return token{kind: tokenString, value: sb.String(), line: startLine}
		}
		if ch == '\\' && lx.pos+1 < len(lx.input) {
			lx.pos++
			sb.WriteByte(lx.input[lx.pos])
			lx.pos++
			continue
		}
		if ch == '\n' {
			lx.line++
		}
		sb.WriteByte(ch)
		lx.pos++
	}
	// Unterminated string — return what we have.
	return token{kind: tokenString, value: sb.String(), line: startLine}
}

func (lx *lexer) readWord() token {
	startLine := lx.line
	start := lx.pos
	for lx.pos < len(lx.input) {
		ch := lx.input[lx.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' ||
			ch == '{' || ch == '}' || ch == ';' || ch == '"' {
			break
		}
		// Stop at comment delimiters too.
		if ch == '/' && lx.pos+1 < len(lx.input) &&
			(lx.input[lx.pos+1] == '/' || lx.input[lx.pos+1] == '*') {
			break
		}
		if ch == '#' {
			break
		}
		lx.pos++
	}
	return token{kind: tokenWord, value: string(lx.input[start:lx.pos]), line: startLine}
}

// ---------------------------------------------------------------------------
// Parser helpers
// ---------------------------------------------------------------------------

// readScalarValue reads the value token (word or quoted string) followed by ';'.
// Returns an error if ';' is missing.
func (lx *lexer) readScalarValue(path string) (string, error) {
	tok := lx.next()
	if tok.kind == tokenEOF {
		return "", fmt.Errorf("%s:%d: unexpected end of input reading value", path, tok.line)
	}
	if tok.kind == tokenRBrace {
		return "", fmt.Errorf("%s:%d: unexpected '}' reading value", path, tok.line)
	}
	if tok.kind != tokenWord && tok.kind != tokenString {
		return "", fmt.Errorf("%s:%d: expected value, got %q", path, tok.line, tok.value)
	}
	val := tok.value
	valLine := tok.line

	// Expect ';'.
	semi := lx.next()
	if semi.kind != tokenSemicolon {
		return "", fmt.Errorf("%s:%d: expected ';' after value %q, got %q", path, valLine, val, semi.value)
	}
	return val, nil
}

// readBracedList reads a `{ token; token; ... };` list and returns tokens as a slice.
func (lx *lexer) readBracedList(path string) ([]string, error) {
	// Expect '{'.
	open := lx.next()
	if open.kind != tokenLBrace {
		return nil, fmt.Errorf("%s:%d: expected '{', got %q", path, open.line, open.value)
	}

	var items []string
	for {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return nil, fmt.Errorf("%s:%d: unterminated list, expected '}'", path, tok.line)
		}
		if tok.kind == tokenRBrace {
			// Consume the ';' that follows '}'.
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return nil, fmt.Errorf("%s:%d: expected ';' after '}', got %q", path, semi.line, semi.value)
			}
			return items, nil
		}
		if tok.kind == tokenSemicolon {
			// Empty statement or trailing semicolon — skip.
			continue
		}
		if tok.kind == tokenWord || tok.kind == tokenString {
			items = append(items, tok.value)
			// Consume the ';' after each item.
			semi := lx.next()
			if semi.kind != tokenSemicolon {
				return nil, fmt.Errorf("%s:%d: expected ';' after %q in list, got %q", path, tok.line, tok.value, semi.value)
			}
			continue
		}
		return nil, fmt.Errorf("%s:%d: unexpected token %q in list", path, tok.line, tok.value)
	}
}

// skipOptionValue skips a single option value: either `word;` / `"str";` or `{ ... };`.
// Used to gracefully ignore unknown options.
func (lx *lexer) skipOptionValue(path string) error {
	tok := lx.peek()
	if tok.kind == tokenLBrace {
		// Skip a balanced { ... }; block.
		return lx.skipBalancedBraceBlock(path)
	}
	// Otherwise consume tokens until ';'.
	for {
		tok = lx.next()
		if tok.kind == tokenEOF {
			return fmt.Errorf("%s:%d: unterminated unknown option value", path, tok.line)
		}
		if tok.kind == tokenSemicolon {
			return nil
		}
		if tok.kind == tokenRBrace {
			// We hit the enclosing block's closing brace — put it back isn't possible,
			// so treat this as a missing semicolon error.
			return fmt.Errorf("%s:%d: unexpected '}' while skipping option value", path, tok.line)
		}
	}
}

// skipBalancedBraceBlock consumes a `{ ... };` block (with arbitrary nesting).
func (lx *lexer) skipBalancedBraceBlock(path string) error {
	open := lx.next() // consume '{'
	if open.kind != tokenLBrace {
		return fmt.Errorf("%s:%d: expected '{', got %q", path, open.line, open.value)
	}
	depth := 1
	for depth > 0 {
		tok := lx.next()
		if tok.kind == tokenEOF {
			return fmt.Errorf("%s:%d: unterminated block", path, tok.line)
		}
		if tok.kind == tokenLBrace {
			depth++
		}
		if tok.kind == tokenRBrace {
			depth--
		}
	}
	// Consume ';' after '}'.
	semi := lx.next()
	if semi.kind != tokenSemicolon {
		return fmt.Errorf("%s:%d: expected ';' after '}', got %q", path, semi.line, semi.value)
	}
	return nil
}
