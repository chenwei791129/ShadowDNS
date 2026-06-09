package ratelimit

import (
	"fmt"
	"net/netip"
)

// Rate-limit action labels recorded via Recorder. These are the prometheus
// `action` label values for the rate-limit counter.
const (
	actionDropped          = "dropped"
	actionSlipped          = "slipped"
	actionExempted         = "exempted"
	actionLogonlyWouldDrop = "logonly_would_drop"
)

// Recorder receives one call per UDP response for which the limiter took a
// rate-limit-relevant action (dropped, slipped, exempted, or a log-only
// would-drop). Responses allowed without being over-limit are NOT reported.
// A nil Recorder disables recording.
type Recorder interface {
	RecordRateLimit(category, action string)
}

// exemptList is a self-contained address-match-list for exempt-clients. It
// holds bare IPs and CIDR prefixes; an empty list matches nothing (no
// exemptions). Kept independent of transfer.ACL so the rate-limit core does not
// depend on the transfer subsystem.
type exemptList struct {
	addrs    []netip.Addr
	prefixes []netip.Prefix
}

// newExemptList parses raw IP / CIDR strings into an exempt list, returning an
// error on the first unparseable entry so invalid exempt-clients surfaces at
// limiter construction (startup) rather than silently matching nothing.
// Addresses are unmapped so v4-mapped-v6 entries match the unmapped client
// addresses produced by addrFromRemote.
func newExemptList(entries []string) (*exemptList, error) {
	if len(entries) == 0 {
		return nil, nil //nolint:nilnil
	}
	e := &exemptList{}
	for _, entry := range entries {
		if p, err := netip.ParsePrefix(entry); err == nil {
			// Unmap v4-mapped-v6 prefixes to plain IPv4 so they match the
			// unmapped client addresses produced by clientAddr. The prefix
			// length must drop the 96-bit v4-mapped offset, otherwise
			// PrefixFrom would build an invalid (>/32) IPv4 prefix that silently
			// matches nothing. Mirrors shadowdnscfg.parseIPOrCIDR.
			addr := p.Addr().Unmap()
			bits := p.Bits()
			if addr.Is4() && bits > 32 {
				bits -= 96
			}
			e.prefixes = append(e.prefixes, netip.PrefixFrom(addr, bits))
			continue
		}
		if a, err := netip.ParseAddr(entry); err == nil {
			e.addrs = append(e.addrs, a.Unmap())
			continue
		}
		return nil, fmt.Errorf("exempt-clients: invalid IP or CIDR %q", entry)
	}
	return e, nil
}

// contains reports whether ip is exempt. A nil list exempts nothing.
func (e *exemptList) contains(ip netip.Addr) bool {
	if e == nil {
		return false
	}
	for _, a := range e.addrs {
		if a == ip {
			return true
		}
	}
	for _, p := range e.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
