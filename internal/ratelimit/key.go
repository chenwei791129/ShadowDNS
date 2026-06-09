package ratelimit

import (
	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// ImputedName derives the account-key name component from a response message,
// per the BIND name-imputation rules. The name is folded to the lookup key
// form so 0x20-randomized case variants share one account:
//
//   - responses (positive answers): the exact query name, so an attacker
//     reflecting one large answer is accounted per name.
//   - nxdomains / nodata: the matched zone origin, taken from the authority
//     SOA owner, so a flood of distinct non-existent names under one zone
//     (random-subdomain attack) aggregates into a single account.
//   - errors: the empty name, so all error responses to one client block
//     aggregate into a single account.
//
// When the expected source is missing (e.g. a negative reply with no SOA in
// the authority section) the empty name is returned, which still aggregates
// safely rather than panicking.
func ImputedName(m *dns.Msg, category Category) string {
	switch category {
	case CategoryResponses:
		if len(m.Question) > 0 {
			return dnsutil.LookupKey(m.Question[0].Name)
		}
		return ""
	case CategoryNxdomains, CategoryNodata:
		for _, rr := range m.Ns {
			if soa, ok := rr.(*dns.SOA); ok {
				return dnsutil.LookupKey(soa.Hdr.Name)
			}
		}
		return ""
	default:
		return ""
	}
}
