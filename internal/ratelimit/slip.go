package ratelimit

import "github.com/miekg/dns"

// slipAction maps the slip parameter and an account's 1-based over-limit
// counter to the enforcement action:
//
//   - slip == 0 : every over-limit response is dropped.
//   - slip == 1 : every over-limit response is truncated (TC=1).
//   - slip == n : every nth over-limit response is truncated, the rest dropped.
//
// The first over-limit response (count == 1) is truncated, so a slip of 2 over
// four responses yields truncate, drop, truncate, drop.
func slipAction(slip int, count uint32) Action {
	if slip <= 0 {
		return Drop
	}
	if (count-1)%uint32(slip) == 0 {
		return Slip
	}
	return Drop
}

// truncateResponse rewrites m in place into a minimal TC=1 truncation: the
// answer, authority, and additional sections are cleared except for the OPT
// record echo, the TC bit is set, and the rcode and question section are
// preserved so a legitimate resolver retries the same query over TCP.
func truncateResponse(m *dns.Msg) {
	var opt *dns.OPT
	for _, rr := range m.Extra {
		if o, ok := rr.(*dns.OPT); ok {
			opt = o
			break
		}
	}
	m.Answer = nil
	m.Ns = nil
	if opt != nil {
		m.Extra = []dns.RR{opt}
	} else {
		m.Extra = nil
	}
	m.Truncated = true
}
