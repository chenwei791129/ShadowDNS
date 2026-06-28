package dnsutil

import (
	"net"
	"net/netip"
	"testing"

	"github.com/miekg/dns"
)

// subnetOpt builds an EDNS0_SUBNET option for tests. addr may be empty to
// model a missing/zero address.
func subnetOpt(family uint16, netmask, scope uint8, addr string) *dns.EDNS0_SUBNET {
	e := &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		Family:        family,
		SourceNetmask: netmask,
		SourceScope:   scope,
	}
	if addr != "" {
		e.Address = net.ParseIP(addr)
	}
	return e
}

func TestClassifyECS(t *testing.T) {
	tests := []struct {
		name     string
		opt      *dns.EDNS0_SUBNET
		want     ECSClass
		wantAddr netip.Addr
	}{
		// Valid options (spec validation matrix).
		{
			name:     "ipv4 /24 zero trailing bits",
			opt:      subnetOpt(1, 24, 0, "203.0.113.0"),
			want:     ECSValid,
			wantAddr: netip.MustParseAddr("203.0.113.0"),
		},
		{
			name:     "ipv6 /56 zero trailing bits",
			opt:      subnetOpt(2, 56, 0, "2001:db8:ab::"),
			want:     ECSValid,
			wantAddr: netip.MustParseAddr("2001:db8:ab::"),
		},

		// Opt-out: SOURCE PREFIX-LENGTH 0, well-formed otherwise.
		{
			name: "family 1 prefix 0 empty address",
			opt:  subnetOpt(1, 0, 0, ""),
			want: ECSOptOut,
		},
		{
			name: "family 1 prefix 0 zero address",
			opt:  subnetOpt(1, 0, 0, "0.0.0.0"),
			want: ECSOptOut,
		},
		{
			name: "family 0 prefix 0 zero address (dig +subnet=0 form)",
			opt:  subnetOpt(0, 0, 0, ""),
			want: ECSOptOut,
		},
		{
			name: "family 2 prefix 0 empty address",
			opt:  subnetOpt(2, 0, 0, ""),
			want: ECSOptOut,
		},

		// Handler-malformed: violations of RFC 7871 query rules.
		{
			name: "non-zero query scope",
			opt:  subnetOpt(1, 24, 24, "203.0.113.0"),
			want: ECSMalformed,
		},
		{
			name: "non-zero bits beyond /24",
			opt:  subnetOpt(1, 24, 0, "203.0.113.9"),
			want: ECSMalformed,
		},
		{
			name: "prefix 0 with non-zero address bits beats opt-out",
			opt:  subnetOpt(1, 0, 0, "203.0.113.9"),
			want: ECSMalformed,
		},
		{
			name: "prefix 0 with non-zero scope beats opt-out",
			opt:  subnetOpt(1, 0, 24, "0.0.0.0"),
			want: ECSMalformed,
		},

		// Default-deny: directly-constructed values the library's unpack
		// would normally reject; the classifier must not panic.
		{
			name: "unknown family 3",
			opt:  subnetOpt(3, 24, 0, "203.0.113.0"),
			want: ECSMalformed,
		},
		{
			name: "family 0 with non-zero prefix",
			opt:  subnetOpt(0, 8, 0, ""),
			want: ECSMalformed,
		},
		{
			name: "family 1 prefix 33 out of range",
			opt:  subnetOpt(1, 33, 0, "203.0.113.0"),
			want: ECSMalformed,
		},
		{
			name: "family 2 prefix 129 out of range",
			opt:  subnetOpt(2, 129, 0, "2001:db8:ab::"),
			want: ECSMalformed,
		},
		{
			name: "nil option",
			opt:  nil,
			want: ECSMalformed,
		},
		{
			name: "family 1 nil address with non-zero prefix",
			opt:  subnetOpt(1, 24, 0, ""),
			want: ECSMalformed,
		},
		{
			name: "family 2 nil address with non-zero prefix",
			opt:  subnetOpt(2, 56, 0, ""),
			want: ECSMalformed,
		},
		{
			name: "family 1 with 16-byte non-v4 address",
			opt: &dns.EDNS0_SUBNET{
				Code:          dns.EDNS0SUBNET,
				Family:        1,
				SourceNetmask: 24,
				Address:       net.ParseIP("2001:db8:ab::"),
			},
			want: ECSMalformed,
		},
		{
			name: "family 2 with 4-byte address",
			opt: &dns.EDNS0_SUBNET{
				Code:          dns.EDNS0SUBNET,
				Family:        2,
				SourceNetmask: 56,
				Address:       net.IP{203, 0, 113, 0},
			},
			want: ECSMalformed,
		},
		{
			name: "family 0 with non-zero address bytes",
			opt: &dns.EDNS0_SUBNET{
				Code:    dns.EDNS0SUBNET,
				Family:  0,
				Address: net.IP{203, 0, 113, 0},
			},
			want: ECSMalformed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, addr := ClassifyECS(tt.opt)
			if got != tt.want {
				t.Fatalf("ClassifyECS() class = %v, want %v", got, tt.want)
			}
			if tt.want == ECSValid && addr != tt.wantAddr {
				t.Fatalf("ClassifyECS() addr = %v, want %v", addr, tt.wantAddr)
			}
		})
	}
}

func TestEchoECS(t *testing.T) {
	t.Run("echoes family, prefix and address with caller scope", func(t *testing.T) {
		q := subnetOpt(1, 24, 0, "203.0.113.0")
		resp := EchoECS(q, 24)

		if resp.Code != dns.EDNS0SUBNET {
			t.Errorf("Code = %d, want %d", resp.Code, dns.EDNS0SUBNET)
		}
		if resp.Family != q.Family {
			t.Errorf("Family = %d, want %d", resp.Family, q.Family)
		}
		if resp.SourceNetmask != q.SourceNetmask {
			t.Errorf("SourceNetmask = %d, want %d", resp.SourceNetmask, q.SourceNetmask)
		}
		if resp.SourceScope != 24 {
			t.Errorf("SourceScope = %d, want 24", resp.SourceScope)
		}
		if !resp.Address.Equal(q.Address) {
			t.Errorf("Address = %v, want %v", resp.Address, q.Address)
		}

		// Must not mutate the query option itself (the address slice is
		// deliberately aliased — the request is handler-owned — but no
		// field of q may change).
		if q.SourceScope != 0 {
			t.Errorf("query SourceScope mutated to %d", q.SourceScope)
		}
	})

	t.Run("opt-out echo preserves family with scope 0", func(t *testing.T) {
		q := subnetOpt(0, 0, 0, "")
		resp := EchoECS(q, 0)

		if resp.Family != 0 {
			t.Errorf("Family = %d, want 0", resp.Family)
		}
		if resp.SourceNetmask != 0 {
			t.Errorf("SourceNetmask = %d, want 0", resp.SourceNetmask)
		}
		if resp.SourceScope != 0 {
			t.Errorf("SourceScope = %d, want 0", resp.SourceScope)
		}
	})

	t.Run("nil-address opt-out echo is packable", func(t *testing.T) {
		// The dns library refuses to pack a FAMILY 1/2 option with a nil
		// address; the echo must therefore substitute an all-zero address of
		// the family width (identical empty wire form for prefix 0).
		for _, family := range []uint16{1, 2} {
			q := &dns.EDNS0_SUBNET{Code: dns.EDNS0SUBNET, Family: family}
			resp := EchoECS(q, 0)

			m := new(dns.Msg)
			m.SetQuestion("example.com.", dns.TypeA)
			m.SetEdns0(1232, false)
			opt := m.IsEdns0()
			opt.Option = append(opt.Option, resp)
			if _, err := m.Pack(); err != nil {
				t.Errorf("family %d: response with echoed option does not pack: %v", family, err)
			}
		}
	})
}

// TestClassifyECS_WireRoundTrip feeds the classifier the exact shapes the dns
// library produces after a pack→unpack round trip, not directly-constructed
// structs. Regression guard: unpack normalizes addresses into forms that
// differ from hand-built test values (FAMILY 0 gets the 16-byte v4-mapped
// net.IPv4zero whose mapping bytes are non-zero), and the classifier must
// classify those wire-real forms, since that is all the handler ever sees in
// production.
func TestClassifyECS_WireRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		opt  *dns.EDNS0_SUBNET
		want ECSClass
	}{
		{
			name: "FAMILY 0 opt-out (dig +subnet=0 form)",
			opt:  subnetOpt(0, 0, 0, ""),
			want: ECSOptOut,
		},
		{
			// The library refuses to pack a nil address even for prefix 0,
			// so the wire-realistic opt-out carries an explicit zero address.
			name: "FAMILY 1 opt-out",
			opt:  subnetOpt(1, 0, 0, "0.0.0.0"),
			want: ECSOptOut,
		},
		{
			name: "FAMILY 1 valid /24",
			opt:  subnetOpt(1, 24, 0, "203.0.113.0"),
			want: ECSValid,
		},
		{
			name: "FAMILY 2 valid /56",
			opt:  subnetOpt(2, 56, 0, "2001:db8:ab::"),
			want: ECSValid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := new(dns.Msg)
			m.SetQuestion("example.com.", dns.TypeA)
			m.SetEdns0(1232, false)
			opt := m.IsEdns0()
			opt.Option = append(opt.Option, tt.opt)

			packed, err := m.Pack()
			if err != nil {
				t.Fatalf("pack: %v", err)
			}
			rt := new(dns.Msg)
			if err := rt.Unpack(packed); err != nil {
				t.Fatalf("unpack: %v", err)
			}
			var wireOpt *dns.EDNS0_SUBNET
			for _, o := range rt.IsEdns0().Option {
				if e, ok := o.(*dns.EDNS0_SUBNET); ok {
					wireOpt = e
					break
				}
			}
			if wireOpt == nil {
				t.Fatal("ECS option lost in round trip")
			}

			got, _ := ClassifyECS(wireOpt)
			if got != tt.want {
				t.Fatalf("ClassifyECS(wire form %+v) = %v, want %v", wireOpt, got, tt.want)
			}
		})
	}
}

func TestParseECSParam(t *testing.T) {
	tests := []struct {
		name        string
		param       string
		wantOK      bool
		wantFamily  uint16
		wantNetmask uint8
		wantAddr    string
	}{
		// Default prefix by family (spec example table).
		{name: "ipv4 default /24", param: "198.51.100.0", wantOK: true, wantFamily: 1, wantNetmask: 24, wantAddr: "198.51.100.0"},
		{name: "ipv4 explicit /16", param: "198.51.100.0/16", wantOK: true, wantFamily: 1, wantNetmask: 16, wantAddr: "198.51.0.0"},
		{name: "ipv6 default /56", param: "2001:db8::", wantOK: true, wantFamily: 2, wantNetmask: 56, wantAddr: "2001:db8::"},
		// Host bits beyond the prefix are masked rather than rejected.
		{name: "ipv4 host bits masked", param: "198.51.100.5/24", wantOK: true, wantFamily: 1, wantNetmask: 24, wantAddr: "198.51.100.0"},
		// Unparseable values.
		{name: "garbage", param: "notanip", wantOK: false},
		{name: "bad prefix", param: "198.51.100.0/33", wantOK: false},
		{name: "empty", param: "", wantOK: false},
		// v4-mapped IPv6 prefix wider than the IPv4 family unmaps to IPv4 but
		// keeps an out-of-range netmask; must be rejected, not silently built
		// into an option ClassifyECS would later discard as malformed.
		{name: "v4-mapped over-width", param: "::ffff:198.51.100.0/120", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt, ok := ParseECSParam(tt.param)
			if ok != tt.wantOK {
				t.Fatalf("ParseECSParam(%q) ok = %v, want %v", tt.param, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if opt.Family != tt.wantFamily {
				t.Errorf("Family = %d, want %d", opt.Family, tt.wantFamily)
			}
			if opt.SourceNetmask != tt.wantNetmask {
				t.Errorf("SourceNetmask = %d, want %d", opt.SourceNetmask, tt.wantNetmask)
			}
			if opt.SourceScope != 0 {
				t.Errorf("SourceScope = %d, want 0 (query form)", opt.SourceScope)
			}
			if got := opt.Address.String(); got != tt.wantAddr {
				t.Errorf("Address = %s, want %s", got, tt.wantAddr)
			}
			// A built option must classify as valid (host bits masked).
			if class, _ := ClassifyECS(opt); class != ECSValid {
				t.Errorf("ClassifyECS = %v, want ECSValid", class)
			}
		})
	}
}
