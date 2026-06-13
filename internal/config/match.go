package config

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// MatchRule is a single leaf predicate within an address-match-list element
// (the value held in Element.Leaf when Element.Kind == ElemLeaf). Implementations
// are concrete struct types (not interfaces) for easy switch dispatch in the
// view-matcher. The built-in ACLs (any/none/localhost/localnets), named-acl
// references, and nested groups are NOT MatchRule values — they are distinct
// Element kinds (see Element / ElementKind).
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

func (CountryRule) isMatchRule() {}
func (ASNRule) isMatchRule()     {}
func (IPRule) isMatchRule()      {}
func (CIDRRule) isMatchRule()    {}

// ElementKind discriminates the forms an address-match-list element can take.
// The grammar is shared between `match-clients { ... };` blocks and `acl`
// bodies, so both produce ordered []Element values.
type ElementKind uint8

const (
	// ElemLeaf is an address/geo predicate held in Element.Leaf
	// (CountryRule/ASNRule/IPRule/CIDRRule).
	ElemLeaf ElementKind = iota
	// ElemAny is the built-in `any` ACL — it always matches.
	ElemAny
	// ElemNone is the built-in `none` ACL — it never matches.
	ElemNone
	// ElemLocalhost is the built-in `localhost` ACL — the server's own addresses.
	ElemLocalhost
	// ElemLocalnets is the built-in `localnets` ACL — networks attached to the
	// server's interfaces.
	ElemLocalnets
	// ElemRef is a reference to a named acl. RefName holds the name before
	// resolution; Sub holds the resolved target element list after the
	// build-phase resolve step (nil when the name is undefined or cyclic, which
	// is fail-closed: the element never matches).
	ElemRef
	// ElemGroup is a nested `{ ... }` group; Sub holds its child element list.
	ElemGroup
)

// Element is one entry in an ordered address-match-list. The list is evaluated
// in declaration order by the view-matcher: the first element that fires decides
// the list outcome — a non-negated firing element accepts, a negated firing
// element (`!`) rejects. Exactly one payload field is meaningful for a given Kind.
type Element struct {
	Kind    ElementKind
	Negated bool // leading '!' — when this element fires, the enclosing list rejects

	// Leaf is the predicate when Kind == ElemLeaf.
	Leaf MatchRule

	// RefName is the referenced acl name when Kind == ElemRef (before resolution).
	RefName string

	// Sub is the resolved target element list when Kind == ElemRef (nil ⇒
	// undefined/cyclic ⇒ never matches) or the nested child list when
	// Kind == ElemGroup.
	Sub []Element
}

// MaxElementDepth bounds recursion through resolved references and nested groups.
// References are resolved to a finite DAG at build time (cycles are broken), so
// this is a defensive backstop, not a functional limit. It is the single source
// of truth shared by the config-side geo-rule scan and the view-matcher.
const MaxElementDepth = 64

// FirstGeoRuleView scans all views' match-clients in declaration order and
// reports the first view containing a geo-class leaf (CountryRule or ASNRule),
// recursing through nested groups and resolved named-acl references. It returns
// that view's Name, Source, and Line, and whether such a view was found. Callers
// use this to decide whether a GeoIP database is required and to point error
// messages at the offending view.
func FirstGeoRuleView(views []View) (viewName, source string, line int, found bool) {
	for _, v := range views {
		if listHasGeoLeaf(v.MatchClients, 0) {
			return v.Name, v.Source, v.Line, true
		}
	}
	return "", "", 0, false
}

// listHasGeoLeaf reports whether elems contains a geo-class leaf, recursing into
// nested groups and resolved references.
func listHasGeoLeaf(elems []Element, depth int) bool {
	if depth > MaxElementDepth {
		return false
	}
	for _, el := range elems {
		switch el.Kind {
		case ElemLeaf:
			// IMPORTANT: any newly added geo-class leaf type (one that requires
			// GeoIP/mmdb data to evaluate) MUST be listed here. Forgetting it
			// silently degrades to "geo rules exist but no mmdb is required".
			switch el.Leaf.(type) {
			case CountryRule, ASNRule:
				return true
			}
		case ElemGroup, ElemRef:
			if listHasGeoLeaf(el.Sub, depth+1) {
				return true
			}
		}
	}
	return false
}

// reASN matches a leading "AS<number>" pattern inside an asnum value.
var reASN = regexp.MustCompile(`^AS(\d+)(\s|$)`)

// ParseMatchClients parses the body of a `match-clients { ... };` block (or an
// `acl` body — both share this grammar) into an ordered []Element. body is the
// raw text between the outer { and } (the caller will have already extracted it).
// path and startLine are used purely for error messages so "line N" reflects the
// line in the original file.
//
// The grammar accepts: the built-in ACLs any/none/localhost/localnets; a
// `geoip country <code>` / `geoip asnum "<value>"` predicate; an IPv4 address or
// CIDR prefix; a bare word or quoted string naming an acl (resolved at build
// time); a nested `{ ... }` group; and a leading `!` negation on any of these.
// Elements may be one-per-line or multiple-per-line separated by ';'. Comments
// (// ..., # ..., /* ... */) are ignored.
//
// A bare word that is none of the recognized keywords/addresses is treated as a
// named-acl reference, NOT dropped here — undefined references are dropped
// (fail-closed, WARN) at the build-phase resolve step after all files load. A
// malformed instance of a recognized form (e.g. a `geoip` sub-command written
// incorrectly) remains a fatal error.
//
// The function does not panic on any input.
func ParseMatchClients(body []byte, path string, startLine int) ([]Element, error) {
	lx := newLexer(body, 0)
	return parseElementList(lx, path, startLine, false)
}

// lineNo maps a body-relative token line (1-based within body) back to the
// 1-based line in the original file.
func lineNo(startLine, tokLine int) int {
	return startLine + tokLine - 1
}

// parseElementList parses elements until EOF (top level) or a closing '}'
// (nested == true, in which case the closing '}' is consumed). It is called
// recursively for nested groups.
func parseElementList(lx *lexer, path string, startLine int, nested bool) ([]Element, error) {
	var elems []Element
	for {
		tok := lx.next()
		if done, err := endOfList(tok, nested, path, startLine); done || err != nil {
			return elems, err
		}
		if tok.kind == tokenSemicolon {
			continue
		}

		// tok begins an element. Strip a leading '!' negation, whether it is a
		// standalone "!" token or prefixed to the next word ("!192.0.2.0/24").
		negated := false
		if tok.kind == tokenWord && tok.value == "!" {
			negated = true
			tok = lx.next()
			// After consuming a standalone '!', a terminator/separator means a
			// dangling '!' with nothing to negate — end or skip gracefully.
			if done, err := endOfList(tok, nested, path, startLine); done || err != nil {
				return elems, err
			}
			if tok.kind == tokenSemicolon {
				continue
			}
		} else if tok.kind == tokenWord && strings.HasPrefix(tok.value, "!") {
			negated = true
			tok.value = strings.TrimPrefix(tok.value, "!")
		}

		if tok.kind == tokenLBrace {
			sub, err := parseElementList(lx, path, startLine, true)
			if err != nil {
				return nil, err
			}
			elems = append(elems, Element{Kind: ElemGroup, Negated: negated, Sub: sub})
			continue
		}

		el, err := parseItem(lx, tok, path, startLine)
		if err != nil {
			return nil, err
		}
		el.Negated = negated
		elems = append(elems, el)
	}
}

// endOfList reports whether tok terminates an address-match-list. EOF ends a
// top-level list; '}' ends a nested list. A '}' at top level or EOF inside a
// nested group is a structural error. Any non-terminator token returns
// (false, nil), leaving the caller to parse it (a ';' separator included).
func endOfList(tok token, nested bool, path string, startLine int) (done bool, err error) {
	switch tok.kind {
	case tokenEOF:
		if nested {
			return false, fmt.Errorf("%s:%d: unterminated nested group in match-clients", path, lineNo(startLine, tok.line))
		}
		return true, nil
	case tokenRBrace:
		if nested {
			return true, nil
		}
		return false, fmt.Errorf("%s:%d: unexpected '}' in match-clients", path, lineNo(startLine, tok.line))
	}
	return false, nil
}

// parseItem parses a single non-negated, non-group element from its leading
// token (a word or quoted string). The '!' prefix and '{' group are handled by
// the caller.
func parseItem(lx *lexer, tok token, path string, startLine int) (Element, error) {
	if tok.kind == tokenString {
		// A quoted token can only be a named-acl reference.
		return Element{Kind: ElemRef, RefName: tok.value}, nil
	}

	word := tok.value
	switch strings.ToLower(word) {
	case "any":
		return Element{Kind: ElemAny}, nil
	case "none":
		return Element{Kind: ElemNone}, nil
	case "localhost":
		return Element{Kind: ElemLocalhost}, nil
	case "localnets":
		return Element{Kind: ElemLocalnets}, nil
	case "geoip":
		return parseGeoIPElement(lx, path, startLine)
	}

	// Try CIDR first, then bare IP.
	if prefix, err := netip.ParsePrefix(word); err == nil {
		return Element{Kind: ElemLeaf, Leaf: CIDRRule{Prefix: prefix}}, nil
	}
	if addr, err := netip.ParseAddr(word); err == nil {
		return Element{Kind: ElemLeaf, Leaf: IPRule{IP: addr}}, nil
	}

	// Anything else is a named-acl reference, resolved (or dropped fail-closed)
	// at build time.
	return Element{Kind: ElemRef, RefName: word}, nil
}

// parseGeoIPElement parses the "geoip ..." family. The "geoip" word has already
// been consumed by the caller. A malformed instance is a fatal error (it is a
// recognized form written wrong, not an unsupported construct).
func parseGeoIPElement(lx *lexer, path string, startLine int) (Element, error) {
	sub := lx.next()
	if sub.kind != tokenWord {
		return Element{}, fmt.Errorf("%s:%d: expected geoip sub-command (country|asnum), got %q", path, lineNo(startLine, sub.line), sub.value)
	}

	switch strings.ToLower(sub.value) {
	case "country":
		val := lx.next()
		code := ""
		if val.kind == tokenWord || val.kind == tokenString {
			code = strings.TrimSpace(val.value)
		}
		if code == "" {
			return Element{}, fmt.Errorf("%s:%d: geoip country missing code", path, lineNo(startLine, val.line))
		}
		return Element{Kind: ElemLeaf, Leaf: CountryRule{Code: strings.ToUpper(code)}}, nil

	case "asnum":
		val := lx.next()
		if val.kind != tokenWord && val.kind != tokenString {
			return Element{}, fmt.Errorf("%s:%d: geoip asnum missing value", path, lineNo(startLine, val.line))
		}
		m := reASN.FindStringSubmatch(val.value)
		if m == nil {
			return Element{}, fmt.Errorf("%s:%d: geoip asnum: cannot parse AS number from %q", path, lineNo(startLine, val.line), val.value)
		}
		n, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil {
			return Element{}, fmt.Errorf("%s:%d: geoip asnum: AS number overflow in %q", path, lineNo(startLine, val.line), val.value)
		}
		return Element{Kind: ElemLeaf, Leaf: ASNRule{ASN: uint32(n)}}, nil

	default:
		return Element{}, fmt.Errorf("%s:%d: unknown geoip sub-command in %q", path, lineNo(startLine, sub.line), "geoip "+sub.value)
	}
}
