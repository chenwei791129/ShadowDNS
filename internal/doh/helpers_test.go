package doh

import (
	"net"
	"net/http"
	"net/netip"
	"testing"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// All fixtures use RFC 2606 domains and RFC 5737 (TEST-NET) addresses.

func makeSOA(origin string) *dns.SOA {
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: origin, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:      "ns1." + origin,
		Mbox:    "hostmaster." + origin,
		Serial:  1,
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
		Minttl:  300,
	}
}

func makeA(name, ip string, ttl uint32) *dns.A {
	return &dns.A{
		Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		A:   net.ParseIP(ip),
	}
}

func buildRootZone(origin string, rrs ...dns.RR) *zone.Zone {
	z := &zone.Zone{Origin: origin, Role: zone.RoleRoot}
	z.AddRR(makeSOA(origin))
	for _, rr := range rrs {
		z.AddRR(rr)
	}
	return z
}

// newAnyViewServer builds a server with a single "default" view matching all
// clients and one root zone.
func newAnyViewServer(t *testing.T, rootZ *zone.Zone) *server.Server {
	t.Helper()
	return server.NewServer(server.ServerState{
		Matcher:     &view.Matcher{Views: []view.NamedRuleSet{{Name: "default", Rules: []config.Element{{Kind: config.ElemAny}}}}},
		ZoneOrigins: map[string][]string{"default": {rootZ.Origin}},
		RootZones:   map[string]map[string]*zone.Zone{"default": {rootZ.Origin: rootZ}},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)
}

// newTwoViewServer builds a server with two views selected by source-IP CIDR,
// each serving a different A record for the same name, so a test can prove
// which source IP drove view selection.
func newTwoViewServer(t *testing.T, name, cidr1, ip1, cidr2, ip2 string) *server.Server {
	t.Helper()
	z1 := buildRootZone("example.com.", makeA(name, ip1, 300))
	z2 := buildRootZone("example.com.", makeA(name, ip2, 300))
	matcher := &view.Matcher{Views: []view.NamedRuleSet{
		{Name: "view1", Rules: []config.Element{{Kind: config.ElemLeaf, Leaf: config.CIDRRule{Prefix: netip.MustParsePrefix(cidr1)}}}},
		{Name: "view2", Rules: []config.Element{{Kind: config.ElemLeaf, Leaf: config.CIDRRule{Prefix: netip.MustParsePrefix(cidr2)}}}},
	}}
	return server.NewServer(server.ServerState{
		Matcher:     matcher,
		ZoneOrigins: map[string][]string{"view1": {"example.com."}, "view2": {"example.com."}},
		RootZones: map[string]map[string]*zone.Zone{
			"view1": {"example.com.": z1},
			"view2": {"example.com.": z2},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
		Aliases:     config.AliasMap{},
	}, nil)
}

// testDoHConfig is a minimal valid DoH config for constructing a Server in
// handler tests (the handler itself does not read these fields).
func testDoHConfig() *shadowdnscfg.DoHConfig {
	return &shadowdnscfg.DoHConfig{
		Listen: "203.0.113.10:443",
		ACME: shadowdnscfg.DoHACMEConfig{
			DirectoryURL: "https://acme.example.com/dir",
			IP:           netip.MustParseAddr("203.0.113.10"),
			HTTP01Listen: "203.0.113.10:80",
		},
	}
}

// newDoHHandler builds the DoH HTTP handler over the given DNS server.
func newDoHHandler(t *testing.T, dnsSrv *server.Server) http.Handler {
	t.Helper()
	s := NewServer(dnsSrv, testDoHConfig(), nil, nil)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	return s.Handler()
}

func tcpAddr(ipPort string) *net.TCPAddr {
	ap := netip.MustParseAddrPort(ipPort)
	return net.TCPAddrFromAddrPort(ap)
}

// queryMsg builds a wire-format A query for name.
func queryMsg(name string) []byte {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	packed, err := m.Pack()
	if err != nil {
		panic(err)
	}
	return packed
}
