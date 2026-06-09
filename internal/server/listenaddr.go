package server

import (
	"fmt"
	"net"
	"slices"

	"go.uber.org/zap"
)

// listen-on tokens recognised by resolveListenOnTokens. Declared as constants
// so a typo in the switch below surfaces at compile time rather than being
// silently skipped as an "unsupported token".
const (
	listenOnAny  = "any"
	listenOnNone = "none"
)

// listenFamily selects the address family a token list resolves to. It drives
// the literal-parse rule, the "any" expansion, and the directive name used in
// WARN/error messages, keeping the IPv4 and IPv6 paths symmetric. Named
// distinctly from handler.go's addrFamily helper to avoid collision.
type listenFamily int

const (
	familyIPv4 listenFamily = iota
	familyIPv6
)

// directive returns the named.conf directive a family maps to, used in operator
// facing log and error messages.
func (f listenFamily) directive() string {
	if f == familyIPv6 {
		return "listen-on-v6"
	}
	return "listen-on"
}

// expandAny enumerates every local interface address belonging to this family,
// applying the family's "any" filtering rules. It dispatches to the family's
// expansion helper so the token resolver stays family-agnostic.
func (f listenFamily) expandAny() ([]string, error) {
	if f == familyIPv6 {
		return expandAnyIPv6()
	}
	return expandAnyIPv4()
}

// normalizeLiteral parses tok as a literal address of this family, returning
// its canonical string form and true on a match. It returns ("", false) when
// tok is not an IP at all or belongs to the other family (e.g. an IPv4 literal
// in listen-on-v6, or an IPv6 literal in listen-on).
func (f listenFamily) normalizeLiteral(tok string) (string, bool) {
	ip := net.ParseIP(tok)
	if ip == nil {
		return "", false
	}
	if f == familyIPv6 {
		if ip.To4() == nil && ip.To16() != nil {
			return ip.String(), true
		}
		return "", false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String(), true
	}
	return "", false
}

// ifaceAddrs is the source of local interface addresses. Tests overwrite it
// to inject deterministic fixtures; production code never touches it.
var ifaceAddrs = net.InterfaceAddrs

// ResolveListenAddresses returns the set of host:port strings to bind,
// applying this precedence based on the -listen flag and named.conf
// listen-on / listen-on-v6 tokens:
//
//  1. If listenFlag has a non-empty host component (e.g. "127.0.0.1:5353",
//     "10.0.0.1:53", "[::1]:5353"), the returned set is exactly {listenFlag}.
//     Both listen-on and listen-on-v6 are ignored. The host may be an IPv4
//     literal or an IPv6 literal in bracket form. This is the operator escape
//     hatch.
//  2. Otherwise (listenFlag is ":PORT" form) the returned set is the union of
//     the IPv4 addresses resolved from listenOn and the IPv6 addresses
//     resolved from listenOnV6, each combined with the port from listenFlag.
//     IPv4 addresses appear before IPv6 addresses; each family preserves
//     first-appearance order. IPv6 addresses are emitted in bracket form
//     "[addr]:port".
//
// Within case 2 the two families differ in their default-when-absent rule:
//   - listenOn absent → resolved as if listen-on { any; } (all IPv4 interfaces)
//   - listenOnV6 absent → empty set (IPv6 is opt-in; absence does NOT imply any)
//
// An error is returned only when the unioned set would be empty (e.g. both
// families resolve to nothing). The message distinguishes an explicit "none"
// from "every token unsupported" so operators can tell a deliberate empty-set
// config from a typo. We prefer a loud startup failure over silently binding
// no listeners.
func ResolveListenAddresses(listenFlag string, listenOn []string, listenOnV6 []string, logger *zap.Logger) ([]string, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	host, port, err := net.SplitHostPort(listenFlag)
	if err != nil {
		return nil, fmt.Errorf("invalid -listen %q: %w", listenFlag, err)
	}

	// Precedence 1: non-empty host → override, ignore both listen-on blocks.
	if host != "" {
		return []string{listenFlag}, nil
	}

	// Precedence 2: empty host. IPv4 family defaults to implicit "any" when
	// listen-on is absent; IPv6 family stays empty when listen-on-v6 is absent.
	v4Tokens := listenOn
	if len(v4Tokens) == 0 {
		v4Tokens = []string{listenOnAny}
	}
	v4IPs, v4NoneExplicit, err := resolveListenOnTokens(v4Tokens, familyIPv4, logger)
	if err != nil {
		return nil, err
	}
	v6IPs, v6NoneExplicit, err := resolveListenOnTokens(listenOnV6, familyIPv6, logger)
	if err != nil {
		return nil, err
	}

	addrs := make([]string, 0, len(v4IPs)+len(v6IPs))
	for _, ip := range v4IPs {
		addrs = append(addrs, net.JoinHostPort(ip, port))
	}
	for _, ip := range v6IPs {
		// net.JoinHostPort brackets the host when it contains a colon, so IPv6
		// addresses come out as "[addr]:port".
		addrs = append(addrs, net.JoinHostPort(ip, port))
	}
	if len(addrs) == 0 {
		return nil, emptyListenSetError(v4NoneExplicit, v6NoneExplicit, len(listenOnV6) > 0)
	}
	return addrs, nil
}

// emptyListenSetError builds the fatal error returned when the unioned
// listen-address set is empty. When listen-on-v6 was not declared at all, the
// IPv4-only historical messages are preserved verbatim for backward
// compatibility. When both families are in play, each family's contribution
// reason is reported so the operator can see which directive emptied the set.
func emptyListenSetError(v4NoneExplicit, v6NoneExplicit, v6Declared bool) error {
	if !v6Declared {
		if v4NoneExplicit {
			return fmt.Errorf("no IPv4 listeners would be started: listen-on contains only 'none'")
		}
		return fmt.Errorf("no IPv4 listeners would be started: every listen-on token was unsupported or skipped")
	}
	return fmt.Errorf("no listeners would be started: listen-on %s; listen-on-v6 %s",
		emptyFamilyReason(v4NoneExplicit), emptyFamilyReason(v6NoneExplicit))
}

// emptyFamilyReason describes why a family contributed no addresses.
func emptyFamilyReason(noneExplicit bool) string {
	if noneExplicit {
		return "is 'none'"
	}
	return "resolved to no usable addresses"
}

// resolveListenOnTokens applies per-token semantics for the given family and
// returns:
//   - a deduplicated list of address strings in first-appearance order (IPv4
//     in dotted form, IPv6 in canonical non-bracket form)
//   - noneExplicit: true if any token was literal "none" (used by the caller
//     to distinguish a deliberate empty-set config from "all tokens were
//     unsupported and silently skipped"; the two cases produce different
//     error messages for operator clarity)
//
// Empty tokens (e.g. listen-on-v6 absent) yield an empty result with
// noneExplicit=false and no error.
func resolveListenOnTokens(tokens []string, fam listenFamily, logger *zap.Logger) (out []string, noneExplicit bool, err error) {
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
			ips, expandErr := fam.expandAny()
			if expandErr != nil {
				return nil, false, fmt.Errorf("enumerating interfaces for %s any: %w", fam.directive(), expandErr)
			}
			for _, ip := range ips {
				add(ip)
			}
		case listenOnNone:
			// Intentional empty set; contributes nothing. Remember for the
			// caller so a pure { none; } produces a clear error.
			noneExplicit = true
		default:
			if norm, ok := fam.normalizeLiteral(tok); ok {
				add(norm)
				continue
			}
			// Unsupported: covers "!addr" exclusions, ACL names, the wrong
			// family's literals (IPv6 in listen-on / IPv4 in listen-on-v6),
			// "port N" syntax, and "interface" keyword. Skip rather than fail
			// so one bad token does not block startup.
			logger.Sugar().Warnw(
				"unsupported listen-on token; skipping",
				"token", tok,
				"directive", fam.directive(),
				"hint", "supported tokens: 'any', 'none', and a literal address of the directive's family",
			)
		}
	}
	return out, noneExplicit, nil
}

// enumerateInterfaceIPs lists local interface addresses via ifaceAddrs and
// returns the canonical string form of each one keep accepts. keep both filters
// (returns ok=false to drop an address) and normalises (returns the string form
// to emit), so each family expresses its accept rule and output form in one
// place. Returned addresses are non-bracket form; callers wrap IPv6 via
// net.JoinHostPort.
func enumerateInterfaceIPs(keep func(net.IP) (string, bool)) ([]string, error) {
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
		if s, ok := keep(ip); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// expandAnyIPv4 enumerates local IPv4 addresses, filtering out IPv6 and IPv4
// link-local (169.254.0.0/16). Loopback addresses — including aliases like
// 127.0.0.53 — are retained; binding them may fail at runtime due to
// systemd-resolved, and that failure is handled one layer up.
func expandAnyIPv4() ([]string, error) {
	return enumerateInterfaceIPs(func(ip net.IP) (string, bool) {
		v4 := ip.To4()
		if v4 == nil || v4.IsLinkLocalUnicast() {
			return "", false // IPv6 handled by expandAnyIPv6; link-local dropped
		}
		return v4.String(), true
	})
}

// expandAnyIPv6 enumerates local IPv6 addresses, mirroring expandAnyIPv4. It
// includes only genuine IPv6 addresses (To4()==nil && To16()!=nil, which
// excludes IPv4 and IPv4-mapped addresses), filters link-local fe80::/10
// (unbindable without a zone index at enumeration time), and retains loopback
// ::1 (symmetric to keeping 127.x; a bind failure is handled one layer up).
func expandAnyIPv6() ([]string, error) {
	return enumerateInterfaceIPs(func(ip net.IP) (string, bool) {
		if ip.To4() != nil || ip.To16() == nil || ip.IsLinkLocalUnicast() {
			return "", false // IPv4/v4-mapped, non-16-byte, or link-local dropped
		}
		return ip.String(), true
	})
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
