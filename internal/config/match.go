package config

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// MatchRule is a single rule within a `match-clients { ... };` block.
// Implementations are concrete struct types (not interfaces) for easy switch
// dispatch in the view-matcher.
type MatchRule interface {
	isMatchRule() // marker method
}

// CountryRule matches traffic by GeoIP country code (ISO-3166-1 alpha-2, uppercase).
type CountryRule struct{ Code string }

// ASNRule matches traffic by autonomous system number.
type ASNRule struct{ ASN uint32 }

// IPRule matches a single IPv4 address.
type IPRule struct{ IP netip.Addr }

// CIDRRule matches an IPv4 CIDR prefix.
type CIDRRule struct{ Prefix netip.Prefix }

// AnyRule is a catch-all that matches all traffic.
type AnyRule struct{}

// DroppedRule records a match-clients token the loader could not evaluate and
// therefore dropped (fail-closed). It carries the raw token and the line it was
// declared on so the caller (which holds the view name and logger) can WARN.
// A dropped rule is never added to a view's MatchClients, so the view-matcher
// treats it as never-matching.
type DroppedRule struct {
	Token string
	Line  int
}

func (CountryRule) isMatchRule() {}
func (ASNRule) isMatchRule()     {}
func (IPRule) isMatchRule()      {}
func (CIDRRule) isMatchRule()    {}
func (AnyRule) isMatchRule()     {}

// FirstGeoRuleView scans all views' match-clients in declaration order and
// reports the first view containing a geo-class rule (CountryRule or ASNRule).
// It returns that view's Name, Source, and Line, and whether such a view was
// found. Callers use this to decide whether a GeoIP database is required and
// to point error messages at the offending view.
func FirstGeoRuleView(views []View) (viewName, source string, line int, found bool) {
	for _, v := range views {
		for _, r := range v.MatchClients {
			// IMPORTANT: any newly added geo-class match rule type (i.e. a
			// rule that requires GeoIP/mmdb data to evaluate) MUST be added
			// to this type switch. Forgetting it silently degrades to
			// "geo rules exist but no mmdb is required".
			switch r.(type) {
			case CountryRule, ASNRule:
				return v.Name, v.Source, v.Line, true
			}
		}
	}
	return "", "", 0, false
}

// reASN matches a leading "AS<number>" pattern inside an asnum quoted value.
var reASN = regexp.MustCompile(`^AS(\d+)(\s|$)`)

// ParseMatchClients parses the body of a `match-clients { ... };` block.
// body is the raw text between the outer { and } (the caller will have already
// extracted it). path and startLine are used purely for error messages so that
// "line N" reflects the line in the original file.
//
// Rules within the body MAY be one-per-line or multiple-per-line separated by ;.
// Comments (// ..., # ..., /* ... */) inside the body are ignored.
//
// A token that matches no recognized rule form (a named-acl reference, a `!`
// negation, a nested group) is not appended to rules; it is returned in dropped
// so the caller can WARN with the enclosing view name. A dropped rule is
// fail-closed: it never matches. A malformed instance of a recognized form (e.g.
// a `geoip` sub-command written incorrectly) remains a fatal error.
//
// The function does not panic on any input.
func ParseMatchClients(body []byte, path string, startLine int) (rules []MatchRule, dropped []DroppedRule, err error) {
	// Strip block comments /* ... */ first (they may span lines).
	stripped := removeBlockComments(string(body))

	lines := strings.Split(stripped, "\n")

	// We iterate line-by-line so we can track which line each token came from
	// for accurate error reporting.
	for lineOffset, rawLine := range lines {
		lineNum := startLine + lineOffset

		// Remove line comments (// and #) from this line.
		cleaned := removeLineComment(rawLine)

		// Split on ; to get individual rule tokens from this line.
		segments := strings.Split(cleaned, ";")
		for _, seg := range segments {
			token := strings.TrimSpace(seg)
			if token == "" {
				continue
			}
			rule, drop, parseErr := parseOneRule(token, path, lineNum)
			if parseErr != nil {
				return nil, nil, parseErr
			}
			if drop {
				dropped = append(dropped, DroppedRule{Token: token, Line: lineNum})
				continue
			}
			rules = append(rules, rule)
		}
	}
	return rules, dropped, nil
}

// removeBlockComments strips /* ... */ comments from s, preserving newlines
// so that line-number tracking remains accurate.
func removeBlockComments(s string) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			// Scan forward for closing */
			i += 2
			for i < len(s) {
				if i+1 < len(s) && s[i] == '*' && s[i+1] == '/' {
					i += 2
					break
				}
				// Preserve newlines to keep line-number accounting correct.
				if s[i] == '\n' {
					buf.WriteByte('\n')
				}
				i++
			}
			continue
		}
		buf.WriteByte(s[i])
		i++
	}
	return buf.String()
}

// removeLineComment strips a // or # line comment from a single line.
func removeLineComment(line string) string {
	// Handle // comment — but only outside quoted strings.
	if idx := indexOutsideQuotes(line, "//"); idx >= 0 {
		line = line[:idx]
	}
	// Handle # comment — only outside quoted strings.
	if idx := indexOutsideQuotes(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	return line
}

// indexOutsideQuotes returns the index of the first occurrence of needle in s
// that is not inside a double-quoted string, or -1 if not found.
func indexOutsideQuotes(s, needle string) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if strings.HasPrefix(s[i:], needle) {
			return i
		}
	}
	return -1
}

// parseOneRule parses a single rule token (trimmed, without the trailing ;).
// It returns (rule, false, nil) for a recognized form; (nil, true, nil) for a
// token matching no recognized form (a named-acl reference, a `!` negation, a
// nested group fragment), which is dropped and treated fail-closed; and
// (nil, false, err) for a malformed instance of a recognized form (e.g. a geoip
// sub-command written incorrectly), which stays fatal.
func parseOneRule(token, path string, lineNum int) (rule MatchRule, dropped bool, err error) {
	switch {
	case token == "any":
		return AnyRule{}, false, nil

	case strings.HasPrefix(token, "geoip "):
		// `geoip` is a recognized form; a malformed instance is fatal, not
		// dropped. On error the caller discards the (zero) rule.
		r, gErr := parseGeoIPRule(token, path, lineNum)
		return r, false, gErr

	default:
		// Try CIDR first, then bare IP.
		if prefix, pErr := netip.ParsePrefix(token); pErr == nil {
			return CIDRRule{Prefix: prefix}, false, nil
		}
		if addr, aErr := netip.ParseAddr(token); aErr == nil {
			return IPRule{IP: addr}, false, nil
		}
		// Unrecognized form: drop it (fail-closed). The view-matcher never
		// promotes a dropped rule to a match.
		return nil, true, nil
	}
}

// parseGeoIPRule handles the "geoip ..." family of rules.
func parseGeoIPRule(token, path string, lineNum int) (MatchRule, error) {
	// token already starts with "geoip "
	rest := strings.TrimPrefix(token, "geoip ")
	rest = strings.TrimSpace(rest)

	switch {
	case strings.HasPrefix(rest, "country "):
		code := strings.TrimSpace(strings.TrimPrefix(rest, "country "))
		if code == "" {
			return nil, fmt.Errorf("%s:%d: geoip country missing code", path, lineNum)
		}
		return CountryRule{Code: strings.ToUpper(code)}, nil

	case strings.HasPrefix(rest, "asnum "):
		raw := strings.TrimSpace(strings.TrimPrefix(rest, "asnum "))
		// Strip surrounding quotes.
		raw = strings.Trim(raw, `"`)
		m := reASN.FindStringSubmatch(raw)
		if m == nil {
			return nil, fmt.Errorf("%s:%d: geoip asnum: cannot parse AS number from %q", path, lineNum, raw)
		}
		n, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: geoip asnum: AS number overflow in %q", path, lineNum, raw)
		}
		return ASNRule{ASN: uint32(n)}, nil

	default:
		return nil, fmt.Errorf("%s:%d: unknown geoip sub-command in %q", path, lineNum, token)
	}
}
