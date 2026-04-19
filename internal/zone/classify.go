package zone

import (
	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// Classify assigns z.Role based on whether z.Origin appears as a backup in aliases.
// For RoleBackupOverride, it also strips records of types other than TXT/MX/SRV
// from z.Records and logs a warning per discarded record.
//
// Modifies z in place; returns z for chaining convenience.
func Classify(z *Zone, aliases config.AliasMap, logger *zap.Logger) *Zone {
	if _, isBackup := aliases[z.Origin]; isBackup {
		z.Role = RoleBackupOverride
		filterBackupRecords(z, logger)
	} else {
		z.Role = RoleRoot
	}
	return z
}

// filterBackupRecords removes records of types other than TXT/MX/SRV from the
// zone, logging a warning for each discarded record. SOA is always among the
// discarded types, so z.SOA is cleared unconditionally.
func filterBackupRecords(z *Zone, logger *zap.Logger) {
	z.SOA = nil
	for owner, sub := range z.Records {
		for rrtype, rrs := range sub {
			if dnsutil.OverridableTypes[rrtype] {
				continue
			}
			for _, rr := range rrs {
				logger.Sugar().Warnw("backup-override zone: discarding disallowed record type",
					"zone", z.Origin,
					"owner", owner,
					"type", dns.TypeToString[rr.Header().Rrtype],
				)
			}
			delete(sub, rrtype)
		}
		if len(sub) == 0 {
			delete(z.Records, owner)
		}
	}
}
