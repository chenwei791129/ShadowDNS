package alias

import "github.com/miekg/dns"

// BackupSOA constructs the SOA record to return for a backup zone's apex.
// It copies all numeric fields from rootSOA verbatim, sets the owner Name to
// backup origin, and rewrites MNAME (Ns) + RNAME (Mbox) via the in-bailiwick rule.
//
// rootSOA MUST not be nil; pass the rootZone.SOA pointer.
//
// Returns a fresh *dns.SOA (does not mutate rootSOA).
func BackupSOA(rootSOA *dns.SOA, root, backup string) *dns.SOA {
	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   backup,
			Rrtype: dns.TypeSOA,
			Class:  rootSOA.Hdr.Class,
			Ttl:    rootSOA.Hdr.Ttl,
		},
		Ns:      RewriteName(rootSOA.Ns, root, backup),
		Mbox:    RewriteName(rootSOA.Mbox, root, backup),
		Serial:  rootSOA.Serial,
		Refresh: rootSOA.Refresh,
		Retry:   rootSOA.Retry,
		Expire:  rootSOA.Expire,
		Minttl:  rootSOA.Minttl,
	}
}
