// Package transfer implements zone-transfer capabilities (AXFR, IXFR, NOTIFY) for ShadowDNS.
package transfer

import (
	"fmt"
	"net/netip"
)

// ACL is a list of permitted source addresses for AXFR.
// An empty ACL denies all (secure default).
type ACL struct {
	addrs    []netip.Addr
	prefixes []netip.Prefix
}

// NewACL parses raw strings (single IPs or CIDR notation) into an ACL.
// Returns an error if any entry fails to parse.
func NewACL(rawEntries []string) (*ACL, error) {
	a := &ACL{}
	for _, entry := range rawEntries {
		// Try to parse as a prefix (CIDR) first.
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			a.prefixes = append(a.prefixes, prefix)
			continue
		}
		// Try to parse as a single address.
		if addr, err := netip.ParseAddr(entry); err == nil {
			a.addrs = append(a.addrs, addr)
			continue
		}
		return nil, fmt.Errorf("allow-transfer: invalid IP or CIDR %q", entry)
	}
	return a, nil
}

// Allows returns true iff src is permitted by the ACL.
// Returns false if a == nil (defensive: nil ACL denies all).
func (a *ACL) Allows(src netip.Addr) bool {
	if a == nil {
		return false
	}
	for _, addr := range a.addrs {
		if addr == src {
			return true
		}
	}
	for _, prefix := range a.prefixes {
		if prefix.Contains(src) {
			return true
		}
	}
	return false
}
