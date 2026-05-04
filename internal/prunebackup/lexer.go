package prunebackup

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// Zone file directive names the lexer and downstream pipeline care about.
// Everything else starting with `$` is still classified as a directive but
// passes through without interpretation.
const (
	directiveTTL      = "$TTL"
	directiveOrigin   = "$ORIGIN"
	directiveInclude  = "$INCLUDE"
	directiveGenerate = "$GENERATE"
)

type lexemeKind int

const (
	kindDirective lexemeKind = iota
	kindBlankOrComment
	kindRR
)

// lexeme is one logical region produced by lexFile. StartLine and EndLine
// are 1-based inclusive file line numbers. For directives, DirectiveName
// holds the uppercase form (e.g. "$TTL") and DirectiveArg holds the first
// argument with any wrapping `"` stripped. For RRs, RR is the parsed dns.RR.
type lexeme struct {
	Kind      lexemeKind
	RawLines  []string
	StartLine int
	EndLine   int

	DirectiveName string
	DirectiveArg  string

	RR dns.RR
}

// lexFile reads path and returns the raw bytes, the split-into-lines view
// of those bytes, and the lexemes. Returning lines lets later stages
// rewrite the file without re-splitting. Parse failures return an error
// containing path:line of the offending RR.
func lexFile(path, initialOrigin string, initialTTL uint32) ([]byte, []string, []lexeme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("prunebackup: read %q: %w", path, err)
	}
	lines, lexemes, err := lexBytes(path, data, initialOrigin, initialTTL)
	if err != nil {
		return nil, nil, nil, err
	}
	return data, lines, lexemes, nil
}

func lexBytes(path string, data []byte, initialOrigin string, initialTTL uint32) ([]string, []lexeme, error) {
	lines := splitLines(data)

	var out []lexeme
	activeOrigin := initialOrigin
	activeTTL := initialTTL

	i := 0
	for i < len(lines) {
		line := lines[i]
		lineNo := i + 1

		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || trimmed[0] == ';' {
			out = append(out, lexeme{
				Kind:      kindBlankOrComment,
				RawLines:  []string{line},
				StartLine: lineNo,
				EndLine:   lineNo,
			})
			i++
			continue
		}

		if trimmed[0] == '$' {
			name, arg := parseDirective(trimmed)
			upper := strings.ToUpper(name)
			switch upper {
			case directiveTTL:
				if n, perr := strconv.ParseUint(arg, 10, 32); perr == nil {
					activeTTL = uint32(n)
				}
			case directiveOrigin:
				activeOrigin = dnsutil.Canonicalize(arg)
			}
			out = append(out, lexeme{
				Kind:          kindDirective,
				RawLines:      []string{line},
				StartLine:     lineNo,
				EndLine:       lineNo,
				DirectiveName: upper,
				DirectiveArg:  arg,
			})
			i++
			continue
		}

		// RR (single or multi-line).
		start := i
		opens, closes := countParens(line)
		rawLines := []string{line}
		for opens > closes {
			i++
			if i >= len(lines) {
				return nil, nil, fmt.Errorf("prunebackup: %s:%d: unterminated '(' in RR", path, start+1)
			}
			o, c := countParens(lines[i])
			opens += o
			closes += c
			rawLines = append(rawLines, lines[i])
		}

		rr, perr := parseRRWithContext(rawLines, activeOrigin, activeTTL)
		if perr != nil {
			return nil, nil, fmt.Errorf("prunebackup: %s:%d: parse RR: %w", path, start+1, perr)
		}

		out = append(out, lexeme{
			Kind:      kindRR,
			RawLines:  rawLines,
			StartLine: start + 1,
			EndLine:   i + 1,
			RR:        rr,
		})
		i++
	}
	return lines, out, nil
}

// splitLines splits data on '\n' without reintroducing a phantom empty
// final line for files that end in a newline. Resulting strings do not
// include the newline or trailing '\r'.
func splitLines(data []byte) []string {
	trimmed := bytes.TrimSuffix(data, []byte("\n"))
	if len(trimmed) == 0 {
		if len(data) == 0 {
			return nil
		}
		return []string{""}
	}
	parts := bytes.Split(trimmed, []byte("\n"))
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(bytes.TrimRight(p, "\r"))
	}
	return out
}

// parseDirective extracts the directive name (including the `$`) and its
// first argument from a trimmed line. Arguments have wrapping `"` removed
// so `$INCLUDE "path"` and `$INCLUDE path` produce the same DirectiveArg.
func parseDirective(trimmed string) (name, arg string) {
	if idx := findUnquotedSemicolon(trimmed); idx >= 0 {
		trimmed = strings.TrimRight(trimmed[:idx], " \t")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", ""
	}
	name = fields[0]
	if len(fields) >= 2 {
		arg = fields[1]
		if len(arg) >= 2 && arg[0] == '"' && arg[len(arg)-1] == '"' {
			arg = arg[1 : len(arg)-1]
		}
	}
	return name, arg
}

// findUnquotedSemicolon returns the index of the first `;` that is not
// enclosed in a `"..."` string, or -1 if none. Backslash escapes inside
// strings are respected.
func findUnquotedSemicolon(s string) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && c == ';' {
			return i
		}
	}
	return -1
}

// countParens counts unescaped, unquoted `(` and `)` in one physical line.
// Parens inside `"..."` and after an unquoted `;` are ignored.
func countParens(line string) (opens, closes int) {
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			i++
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch c {
		case ';':
			return
		case '(':
			opens++
		case ')':
			closes++
		}
	}
	return
}

// parseRRWithContext feeds rawLines to miekg/dns.NewRR with active
// $ORIGIN / $TTL prepended so relative owners and implicit TTLs resolve the
// same way the server loader does.
//
// A ttl of 0 means "no $TTL has been set yet": no directive is injected and
// miekg's own default (0) takes over, which matches the behaviour of a real
// zone file that has not declared $TTL. An operator who literally wants a
// 0-second TTL must rely on a per-RR TTL column.
func parseRRWithContext(rawLines []string, origin string, ttl uint32) (dns.RR, error) {
	var b strings.Builder
	if origin != "" {
		b.WriteString(directiveOrigin + " ")
		b.WriteString(origin)
		b.WriteByte('\n')
	}
	if ttl != 0 {
		fmt.Fprintf(&b, "%s %d\n", directiveTTL, ttl)
	}
	for _, l := range rawLines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return dns.NewRR(b.String())
}
