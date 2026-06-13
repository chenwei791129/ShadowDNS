package view

import (
	"net"
	"net/netip"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// matchIP returns true iff rule.IP equals clientIP.
func matchIP(rule config.IPRule, clientIP netip.Addr) bool {
	return rule.IP == clientIP
}

// matchCIDR returns true iff rule.Prefix contains clientIP.
func matchCIDR(rule config.CIDRRule, clientIP netip.Addr) bool {
	return rule.Prefix.Contains(clientIP)
}

// anyPrefixContains reports whether any prefix in the set contains ip. Used by
// the localhost/localnets built-in ACLs.
func anyPrefixContains(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// LocalInterfaceNets enumerates, from the host's current network interfaces, the
// server's own addresses as host prefixes (for the `localhost` built-in ACL) and
// the networks directly attached to those interfaces (for `localnets`). It is
// called at build/reload time so the sets track interface changes. On
// enumeration failure it returns the error and nil sets; the caller treats nil
// sets as fail-closed (localhost/localnets then match nothing).
func LocalInterfaceNets() (localhost, localnets []netip.Prefix, err error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	for _, iface := range ifaces {
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipNet.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			// localhost: the address itself, as a host route (/32 or /128).
			localhost = append(localhost, netip.PrefixFrom(addr, addr.BitLen()))
			// localnets: the attached network (address masked to its prefix
			// length). Skip an interface whose mask is either non-canonical
			// (Mask.Size() reports bits == 0) or a /0 (ones == 0): emitting a
			// 0.0.0.0/0 or ::/0 prefix would make `localnets` match every client
			// (fail-open). A point-to-point/tunnel interface carrying a /0 address
			// is the realistic trigger for the ones == 0 case.
			ones, bits := ipNet.Mask.Size()
			// An IPv4 address can be returned with a 128-bit (IPv4-mapped) mask.
			// addr was just Unmap()'d to 32 bits, so translate the prefix length
			// into the IPv4 space (drop the 96-bit v4-mapped prefix) to keep ones
			// within addr.BitLen(); otherwise addr.Prefix(ones) would error and
			// the network would be silently dropped from localnets.
			if addr.Is4() && bits == 128 {
				if ones < 96 {
					continue
				}
				ones, bits = ones-96, 32
			}
			if bits == 0 || ones == 0 {
				continue
			}
			if p, prefErr := addr.Prefix(ones); prefErr == nil {
				localnets = append(localnets, p)
			}
		}
	}
	return localhost, localnets, nil
}
