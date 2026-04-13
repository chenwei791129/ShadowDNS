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

func (CountryRule) isMatchRule() {}
func (ASNRule) isMatchRule()     {}
func (IPRule) isMatchRule()      {}
func (CIDRRule) isMatchRule()    {}
func (AnyRule) isMatchRule()     {}

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
// The function does not panic on any input.
func ParseMatchClients(body []byte, path string, startLine int) (rules []MatchRule, err error) {
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
			rule, parseErr := parseOneRule(token, path, lineNum)
			if parseErr != nil {
				return nil, parseErr
			}
			rules = append(rules, rule)
		}
	}
	return rules, nil
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
func parseOneRule(token, path string, lineNum int) (MatchRule, error) {
	switch {
	case token == "any":
		return AnyRule{}, nil

	case strings.HasPrefix(token, "geoip "):
		return parseGeoIPRule(token, path, lineNum)

	default:
		// Try CIDR first, then bare IP.
		if prefix, err := netip.ParsePrefix(token); err == nil {
			return CIDRRule{Prefix: prefix}, nil
		}
		if addr, err := netip.ParseAddr(token); err == nil {
			return IPRule{IP: addr}, nil
		}
		return nil, fmt.Errorf("%s:%d: unknown rule %q", path, lineNum, token)
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
