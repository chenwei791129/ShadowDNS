package zone

import (
	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// msgDiscardDisallowed is the structured-log message emitted for each record
// dropped from a backup-override zone. Held as a const so the classifier and
// its tests reference the same string.
const msgDiscardDisallowed = "backup-override zone: discarding disallowed record type"

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
// zone. SOA is always among the discarded types, so z.SOA is cleared
// unconditionally.
//
// Per-record log level is split: dropped SOA records and apex NS records (NS
// owner equals the canonical zone origin) are logged at DEBUG since their
// presence is required for RFC 1035 zone-file validity and their drop is an
// expected, non-actionable event. All other discarded records are logged at
// WARN. The per-zone INFO summary fires only when at least one WARN-level drop
// occurred (other_dropped > 0); a zone whose only drops are RFC-mandated
// SOA/apex-NS produces no summary, so a fully-pruned ns2 stays silent.
func filterBackupRecords(z *Zone, logger *zap.Logger) {
	z.SOA = nil
	canonOrigin := dnsutil.LookupKey(z.Origin)
	var soaDropped, apexNSDropped, otherDropped int

	logDrop := func(rr dns.RR, owner string) {
		rrtype := rr.Header().Rrtype
		isSOA := rrtype == dns.TypeSOA
		isApexNS := rrtype == dns.TypeNS && owner == canonOrigin

		fields := []zap.Field{
			zap.String("zone", z.Origin),
			zap.String("owner", owner),
			zap.String("type", dns.TypeToString[rrtype]),
		}
		switch {
		case isSOA:
			soaDropped++
			logger.Debug(msgDiscardDisallowed, fields...)
		case isApexNS:
			apexNSDropped++
			logger.Debug(msgDiscardDisallowed, fields...)
		default:
			otherDropped++
			logger.Warn(msgDiscardDisallowed, fields...)
		}
	}

	for owner, s := range z.Records {
		if s.single {
			if dnsutil.OverridableTypes[s.qtype] {
				continue
			}
			for _, rr := range s.rrs {
				logDrop(rr, owner)
			}
			delete(z.Records, owner)
			continue
		}
		for rrtype, rrs := range s.sub {
			if dnsutil.OverridableTypes[rrtype] {
				continue
			}
			for _, rr := range rrs {
				logDrop(rr, owner)
			}
			delete(s.sub, rrtype)
		}
		if len(s.sub) == 0 {
			delete(z.Records, owner)
		}
	}

	if otherDropped > 0 {
		logger.Info("backup-override zone: drop summary",
			zap.String("zone", z.Origin),
			zap.Int("soa_dropped", soaDropped),
			zap.Int("apex_ns_dropped", apexNSDropped),
			zap.Int("other_dropped", otherDropped),
		)
	}
}
