package server

import (
	"fmt"
	"log/slog"
	"net"
	"slices"
)

// listen-on tokens recognised by resolveListenOnTokens. Declared as constants
// so a typo in the switch below surfaces at compile time rather than being
// silently skipped as an "unsupported token".
const (
	listenOnAny  = "any"
	listenOnNone = "none"
)

// linkLocalIPv4 is 169.254.0.0/16. Addresses in this range are filtered when
// expanding "any": they are rarely useful for a DNS server and bind attempts
// on them produce noise without serving any real client.
var linkLocalIPv4 = &net.IPNet{
	IP:   net.IPv4(169, 254, 0, 0),
	Mask: net.CIDRMask(16, 32),
}

// ifaceAddrs is the source of local interface addresses. Tests overwrite it
// to inject deterministic fixtures; production code never touches it.
var ifaceAddrs = net.InterfaceAddrs

// ResolveListenAddresses returns the set of host:port strings to bind,
// applying this precedence based on the -listen flag and named.conf
// listen-on tokens:
//
//  1. If listenFlag has a non-empty host component (e.g. "127.0.0.1:5353",
//     "10.0.0.1:53"), the returned set is exactly {listenFlag}. listen-on
//     is ignored. This is the operator escape hatch.
//  2. Otherwise (listenFlag is ":PORT" form), if listenOn is non-empty each
//     token is resolved:
//     - "any" expands to every local IPv4 interface address
//     - "none" contributes nothing (see empty-set rule below)
//     - A literal IPv4 address is used as-is
//     - Anything else (e.g. "!addr", ACL names, IPv6 literals, "port N")
//     is logged at WARN level and skipped.
//     Each resolved IPv4 address is combined with the port from listenFlag.
//  3. Otherwise (listenOn empty AND listenFlag is ":PORT" form), resolution
//     behaves as if listenOn were {"any"}, still using the port from
//     listenFlag.
//
// An error is returned when the resolved address set would be empty (e.g.
// listen-on { none; } with nothing else, or every token unsupported). We
// prefer a loud startup failure over silently binding no listeners.
func ResolveListenAddresses(listenFlag string, listenOn []string, logger *slog.Logger) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	host, port, err := net.SplitHostPort(listenFlag)
	if err != nil {
		return nil, fmt.Errorf("invalid -listen %q: %w", listenFlag, err)
	}

	// Precedence 1: non-empty host → override, ignore listen-on.
	if host != "" {
		return []string{listenFlag}, nil
	}

	// Precedence 2/3: empty host. Use listen-on if present; else implicit "any".
	tokens := listenOn
	if len(tokens) == 0 {
		tokens = []string{listenOnAny}
	}

	ips, noneExplicit, err := resolveListenOnTokens(tokens, logger)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		if noneExplicit {
			return nil, fmt.Errorf("no IPv4 listeners would be started: listen-on contains only 'none'")
		}
		return nil, fmt.Errorf("no IPv4 listeners would be started: every listen-on token was unsupported or skipped")
	}

	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, net.JoinHostPort(ip, port))
	}
	return addrs, nil
}

// resolveListenOnTokens applies per-token semantics and returns:
//   - a deduplicated list of IPv4 address strings in first-appearance order
//   - noneExplicit: true if any token was literal "none" (used by the caller
//     to distinguish a deliberate empty-set config from "all tokens were
//     unsupported and silently skipped"; the two cases produce different
//     error messages for operator clarity)
func resolveListenOnTokens(tokens []string, logger *slog.Logger) (out []string, noneExplicit bool, err error) {
	seen := make(map[string]struct{}, len(tokens))
	out = make([]string, 0, len(tokens))

	add := func(ip string) {
		if _, ok := seen[ip]; ok {
			return
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}

	for _, tok := range tokens {
		switch tok {
		case listenOnAny:
			ips, expandErr := expandAnyIPv4()
			if expandErr != nil {
				return nil, false, fmt.Errorf("enumerating interfaces for listen-on any: %w", expandErr)
			}
			for _, ip := range ips {
				add(ip)
			}
		case listenOnNone:
			// Intentional empty set; contributes nothing. Remember for the
			// caller so a pure listen-on { none; } produces a clear error.
			noneExplicit = true
		default:
			// Try as literal IPv4.
			parsed := net.ParseIP(tok)
			if parsed != nil && parsed.To4() != nil {
				add(parsed.To4().String())
				continue
			}
			// Unsupported: covers "!addr" exclusions, ACL names, IPv6
			// literals, "port N" syntax, and "interface" keyword. Skip
			// rather than fail so one bad token does not block startup.
			logger.Warn(
				"unsupported listen-on token; skipping",
				"token", tok,
				"hint", "this version supports IPv4 literals, 'any', and 'none' only",
			)
		}
	}
	return out, noneExplicit, nil
}

// expandAnyIPv4 enumerates local IPv4 addresses via ifaceAddrs, filtering out
// IPv6 and IPv4 link-local (169.254.0.0/16). Loopback addresses — including
// aliases like 127.0.0.53 — are retained; binding them may fail at runtime
// due to systemd-resolved, and that failure is handled one layer up.
func expandAnyIPv4() ([]string, error) {
	addrs, err := ifaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ip := addrIP(a)
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue // IPv6 not handled by this change
		}
		if linkLocalIPv4.Contains(v4) {
			continue
		}
		out = append(out, v4.String())
	}
	return out, nil
}

// addrIP returns nil for address types other than *net.IPNet and *net.IPAddr;
// callers must nil-check. These are the two concrete types net.InterfaceAddrs
// is documented to return, but we leave the default branch in case the stdlib
// grows additional types.
func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

// AddrSetEqual reports whether a and b contain the same elements, ignoring
// order. Duplicates are treated as multiset entries: ["a","a"] is not equal
// to ["a"]. Used by the reload path to compare the currently-bound address
// set against a freshly resolved set. The sort+Equal approach is simple
// and sufficient at the bounded N of a DNS server's listener count.
func AddrSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := slices.Sorted(slices.Values(a))
	bs := slices.Sorted(slices.Values(b))
	return slices.Equal(as, bs)
}
