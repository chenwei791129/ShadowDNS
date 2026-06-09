package ratelimit

import (
	"testing"

	"github.com/miekg/dns"
)

func answerA() []dns.RR {
	return []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "a.example.com.", Rrtype: dns.TypeA}}}
}

func TestClassifyResponse(t *testing.T) {
	tests := []struct {
		name   string
		rcode  int
		answer []dns.RR
		want   Category
	}{
		{"NOERROR with answer is responses", dns.RcodeSuccess, answerA(), CategoryResponses},
		{"NOERROR empty answer is nodata", dns.RcodeSuccess, nil, CategoryNodata},
		{"NXDOMAIN is nxdomains", dns.RcodeNameError, nil, CategoryNxdomains},
		{"SERVFAIL is errors", dns.RcodeServerFailure, nil, CategoryErrors},
		{"REFUSED is errors", dns.RcodeRefused, nil, CategoryErrors},
		{"FORMERR is errors", dns.RcodeFormatError, nil, CategoryErrors},
		{"NOTIMP is errors", dns.RcodeNotImplemented, nil, CategoryErrors},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := new(dns.Msg)
			m.Rcode = tc.rcode
			m.Answer = tc.answer
			if got := ClassifyResponse(m); got != tc.want {
				t.Errorf("ClassifyResponse() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCategoryString locks the metric-label strings so prometheus labels stay
// stable across refactors.
func TestCategoryString(t *testing.T) {
	cases := map[Category]string{
		CategoryResponses: "responses",
		CategoryNodata:    "nodata",
		CategoryNxdomains: "nxdomains",
		CategoryErrors:    "errors",
	}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Errorf("Category(%d).String() = %q, want %q", c, got, want)
		}
	}
}
