package zone

import (
	"sort"

	"github.com/miekg/dns"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// msgDiscardDisallowed is the structured-log message emitted for each record
// dropped from a backup-override zone. Held as a const so the classifier and
// its tests reference the same string.
const msgDiscardDisallowed = "backup-override zone: discarding disallowed record type"

// dropLabelSOA / dropLabelApexNS are the literal histogram-bucket labels for
// SOA and apex NS drops. Apex NS uses a non-standard label ("apex_NS") so
// the per-zone INFO summary disambiguates apex-NS drops (RFC 1035 mandated
// in the zone file, always discarded) from sub-delegation NS drops (which
// indicate operator data leaking into a backup zone).
const (
	dropLabelSOA    = "SOA"
	dropLabelApexNS = "apex_NS"
)

// dropHistogram is a by-RR-type tally of records discarded from one
// backup-override zone, emitted as the `dropped` field of the per-zone INFO
// summary. Standard RR-type names (A, CNAME, NS, ...) are joined by the two
// literal labels SOA and apex_NS. The MarshalLogObject implementation sorts
// keys alphabetically (ASCII) at serialization time so the JSON / console
// rendering is grep-stable across runs even though Go map iteration is not.
type dropHistogram map[string]int

// MarshalLogObject implements zapcore.ObjectMarshaler with deterministic
// alphabetic key order. zap.Object preserves the call order, so sorting the
// keys here is what makes the serialized output deterministic.
func (h dropHistogram) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		enc.AddInt64(k, int64(h[k]))
	}
	return nil
}

// Classify assigns z.Role based on whether z.Origin appears as a backup in aliases.
// For RoleBackupOverride, it also strips records of types other than TXT/MX/SRV
// from z.Records and emits a DEBUG entry per discarded record plus one INFO
// summary per zone (see filterBackupRecords).
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
// Every discarded record is logged at DEBUG level — including non-overridable
// drops that previously logged at WARN. The per-zone INFO summary aggregates
// counts into a by-type histogram and fires whenever the zone produced at
// least one drop (zero-drop zones stay silent). Routing all per-record
// detail to DEBUG keeps INFO-level log volume bounded by zone count rather
// than RR count, which is what protects production logs from the 7-view ×
// 2854-CNAME blow-up that motivated this change.
func filterBackupRecords(z *Zone, logger *zap.Logger) {
	z.SOA = nil
	canonOrigin := dnsutil.LookupKey(z.Origin)
	dropped := dropHistogram{}
	// Cache the level check once per zone: backup-override startup hits this
	// closure once per discarded RR (~tens of thousands at production scale),
	// and zap evaluates variadic zap.Field args at the call site even when
	// the level filter would discard the entry. Skipping the call entirely
	// when DEBUG is disabled keeps the per-RR cost to one map increment.
	debugEnabled := logger.Core().Enabled(zapcore.DebugLevel)

	logDrop := func(rr dns.RR, owner string) {
		rrtype := rr.Header().Rrtype
		var label string
		switch {
		case rrtype == dns.TypeSOA:
			label = dropLabelSOA
		case rrtype == dns.TypeNS && owner == canonOrigin:
			label = dropLabelApexNS
		default:
			label = dns.TypeToString[rrtype]
		}
		dropped[label]++
		if debugEnabled {
			logger.Debug(msgDiscardDisallowed,
				zap.String("zone", z.Origin),
				zap.String("owner", owner),
				zap.String("type", dns.TypeToString[rrtype]),
			)
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

	if len(dropped) > 0 {
		logger.Info("backup-override zone: drop summary",
			zap.String("zone", z.Origin),
			zap.Object("dropped", dropped),
		)
	}
}
