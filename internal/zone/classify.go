package zone

import (
	"log/slog"

	"github.com/miekg/dns"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// Classify assigns z.Role based on whether z.Origin appears as a backup in aliases.
// For RoleBackupOverride, it also strips records of types other than TXT/MX/SRV
// from z.Records and logs a warning per discarded record.
//
// Modifies z in place; returns z for chaining convenience.
func Classify(z *Zone, aliases config.AliasMap, logger *slog.Logger) *Zone {
	if _, isBackup := aliases[z.Origin]; isBackup {
		z.Role = RoleBackupOverride
		filterBackupRecords(z, logger)
	} else {
		z.Role = RoleRoot
	}
	return z
}

// filterBackupRecords removes records of types other than TXT/MX/SRV from the zone,
// logging a warning for each discarded record.
func filterBackupRecords(z *Zone, logger *slog.Logger) {
	for owner, rrs := range z.Records {
		kept := rrs[:0] // reuse backing array
		for _, rr := range rrs {
			if dnsutil.OverridableTypes[rr.Header().Rrtype] {
				kept = append(kept, rr)
			} else {
				logger.Warn("backup-override zone: discarding disallowed record type",
					"zone", z.Origin,
					"owner", owner,
					"type", dns.TypeToString[rr.Header().Rrtype],
				)
			}
		}
		if len(kept) == 0 {
			delete(z.Records, owner)
		} else {
			z.Records[owner] = kept
		}
	}

	// Re-derive SOA pointer since filtering may have removed it.
	z.SOA = nil
	for _, rrs := range z.Records {
		for _, rr := range rrs {
			if soa, ok := rr.(*dns.SOA); ok {
				z.SOA = soa
			}
		}
	}
}
