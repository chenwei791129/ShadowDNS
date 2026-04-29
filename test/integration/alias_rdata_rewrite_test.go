package integration_test

import (
	"testing"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// TestAliasRDATARewrite_TemplatedCNAME exercises the dnspyre-observed
// templated-CNAME case end-to-end: the root zone holds a CNAME whose target
// embeds the root origin as a middle label
// (`host.<root>.cdn.example.net.`), and the alias group declares
// `rewrite_rdata_labels: true`. The CNAME query against the backup origin
// must return a target whose middle label is rewritten to the backup origin.
//
// A second alias group with `rewrite_rdata_labels: false` proves the legacy
// behavior is preserved when the flag is not opted in.
func TestAliasRDATARewrite_TemplatedCNAME(t *testing.T) {
	const (
		rootOrigin           = "root.com."
		backupOriginOptIn    = "backup.com."
		backupOriginLegacy   = "mirror.com."
		templatedTarget      = "host.root.com.cdn.example.net."
		expectedTargetOptIn  = "host.backup.com.cdn.example.net."
		expectedTargetLegacy = "host.root.com.cdn.example.net."
	)

	rootZ := newRootZoneWithCNAME(t, rootOrigin, "host."+rootOrigin, templatedTarget)

	state := server.ServerState{
		Matcher: &view.Matcher{
			Views: []view.NamedRuleSet{
				{Name: "default", Rules: []config.MatchRule{config.AnyRule{}}},
			},
		},
		Aliases: config.AliasMap{
			backupOriginOptIn:  rootOrigin,
			backupOriginLegacy: rootOrigin,
		},
		AliasFlags: config.AliasFlags{
			backupOriginOptIn:  true,
			backupOriginLegacy: false,
		},
		ZoneOrigins: map[string][]string{
			"default": {rootOrigin, backupOriginOptIn, backupOriginLegacy},
		},
		RootZones: map[string]map[string]*zone.Zone{
			"default": {rootOrigin: rootZ},
		},
		BackupZones: map[string]map[string]*zone.Zone{},
	}

	srv := server.NewServer(state, zap.NewNop())
	addr, cancel := bindAndServe(t, srv, nil)
	defer cancel()

	t.Run("flag_true_rewrites_mid_label", func(t *testing.T) {
		resp := queryUDP(t, addr, "host."+backupOriginOptIn, dns.TypeCNAME)
		assertNoError(t, resp)
		assertHasCNAME(t, resp, "host."+backupOriginOptIn, expectedTargetOptIn)
	})

	t.Run("flag_false_preserves_mid_label", func(t *testing.T) {
		resp := queryUDP(t, addr, "host."+backupOriginLegacy, dns.TypeCNAME)
		assertNoError(t, resp)
		assertHasCNAME(t, resp, "host."+backupOriginLegacy, expectedTargetLegacy)
	})
}

func newRootZoneWithCNAME(t *testing.T, origin, owner, target string) *zone.Zone {
	t.Helper()
	z := &zone.Zone{Origin: origin, Role: zone.RoleRoot}
	z.AddRR(&dns.SOA{
		Hdr: dns.RR_Header{
			Name:   origin,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Ns:      "ns1." + origin,
		Mbox:    "admin." + origin,
		Serial:  2024010101,
		Refresh: 3600,
		Retry:   900,
		Expire:  604800,
		Minttl:  300,
	})
	z.AddRR(&dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   owner,
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Target: target,
	})
	return z
}
