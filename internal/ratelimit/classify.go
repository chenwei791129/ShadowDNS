// Package ratelimit implements BIND-compatible Response Rate Limiting (RRL)
// for ShadowDNS: a token-bucket (credit) ledger that caps near-identical UDP
// responses to a single client address block, mitigating DNS amplification /
// reflection attacks. It applies only to UDP; TCP responses are never limited.
package ratelimit

import "github.com/miekg/dns"

// Category is the rate-limit response category a UDP response is classified
// into. The referrals category exists in BIND configuration for compatibility
// but is never assigned here because ShadowDNS is authoritative-only and emits
// no referral responses.
type Category int

const (
	// CategoryResponses is a NOERROR response carrying a non-empty answer.
	CategoryResponses Category = iota
	// CategoryNodata is a NOERROR response with an empty answer section.
	CategoryNodata
	// CategoryNxdomains is an NXDOMAIN response.
	CategoryNxdomains
	// CategoryErrors is any other error rcode (SERVFAIL, FORMERR, REFUSED,
	// NOTIMP, …).
	CategoryErrors

	// numCategories bounds per-category arrays. Keep last.
	numCategories
)

// String returns the prometheus-label form of the category.
func (c Category) String() string {
	switch c {
	case CategoryResponses:
		return "responses"
	case CategoryNodata:
		return "nodata"
	case CategoryNxdomains:
		return "nxdomains"
	case CategoryErrors:
		return "errors"
	default:
		return "unknown"
	}
}

// ClassifyResponse derives the rate-limit category from a response message,
// looking only at the rcode and answer section:
//   - NOERROR with a non-empty answer  → responses
//   - NOERROR with an empty answer     → nodata
//   - NXDOMAIN                         → nxdomains
//   - any other rcode                  → errors
func ClassifyResponse(m *dns.Msg) Category {
	switch m.Rcode {
	case dns.RcodeSuccess:
		if len(m.Answer) > 0 {
			return CategoryResponses
		}
		return CategoryNodata
	case dns.RcodeNameError:
		return CategoryNxdomains
	default:
		return CategoryErrors
	}
}
