package server

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// BuildSummary reports how many zones were reused vs re-parsed during a
// BuildState call. Used by the caller to emit reload diff log entries.
type BuildSummary struct {
	Reused   int
	Reparsed int
}

// BuildState assembles a ServerState from parsed configuration. When prev is
// non-nil and mode is not VerifyModeNone, unchanged zones (same fingerprint)
// reuse their existing *zone.Zone pointer rather than re-parsing from disk.
//
// aliasFlags is the per-backup-origin rewrite_rdata_labels lookup; pass nil
// (or an empty map) when no alias group declares the flag, in which case all
// backup zones use the default in-bailiwick suffix-only rewrite.
//
// collapseFlags is the per-root-origin collapse_cname_chain lookup; pass nil
// (or an empty map) when no alias group declares the flag, in which case
// CNAME chains are emitted unchanged everywhere.
//
// backupOriginalCase maps the lookup-fold backup origin to the operator-
// authored original case used when emitting on-wire names; pass nil to fall
// back to the lookup-fold form (loses YAML case for that backup).
func BuildState(
	cfg *config.Config,
	aliases config.AliasMap,
	aliasFlags config.AliasFlags,
	collapseFlags config.CollapseFlags,
	backupOriginalCase map[string]string,
	prev *ServerState,
	mode VerifyMode,
	country *view.CountryDB,
	asn *view.ASNDB,
	logger *zap.Logger,
) (ServerState, BuildSummary, error) {
	rootZones := make(map[string]map[string]*zone.Zone)
	backupZones := make(map[string]map[string]*zone.Zone)
	zoneOrigins := make(map[string][]string)
	fingerprints := make(map[string]map[string]zoneFingerprint)
	viewRuleSets := make([]view.NamedRuleSet, 0, len(cfg.Views))
	var summary BuildSummary

	for _, v := range cfg.Views {
		viewRuleSets = append(viewRuleSets, view.NamedRuleSet{
			Name:  v.Name,
			Rules: v.MatchClients,
		})

		rootZones[v.Name] = make(map[string]*zone.Zone)
		backupZones[v.Name] = make(map[string]*zone.Zone)
		fingerprints[v.Name] = make(map[string]zoneFingerprint)
		seenOrigins := make([]string, 0, len(v.Zones))
		seenSet := make(map[string]bool, len(v.Zones))

		for _, z := range v.Zones {
			origin := z.Name + "."

			parsed, fp, reused, err := loadZone(z, origin, prev, v.Name, mode, aliases, logger)
			if err != nil {
				return ServerState{}, BuildSummary{}, fmt.Errorf("view %q zone %q: %w", v.Name, z.Name, err)
			}

			if reused {
				summary.Reused++
			} else {
				summary.Reparsed++
				zone.Classify(parsed, aliases, logger)
			}

			switch parsed.Role {
			case zone.RoleBackupOverride:
				backupZones[v.Name][origin] = parsed
			default:
				rootZones[v.Name][origin] = parsed
			}
			fingerprints[v.Name][origin] = fp

			seenOrigins = append(seenOrigins, origin)
			seenSet[origin] = true
		}

		// Aliased backups declared in the config aliases section but missing from
		// master.zones still need an origin entry so Detect() can match them.
		for backup, root := range aliases {
			if _, ok := rootZones[v.Name][root]; !ok {
				continue
			}
			if _, ok := backupZones[v.Name][backup]; ok {
				continue
			}
			if !seenSet[backup] {
				seenOrigins = append(seenOrigins, backup)
				seenSet[backup] = true
			}
		}

		zoneOrigins[v.Name] = seenOrigins
	}

	// Enumerate the host's own addresses and attached networks for the
	// localhost/localnets built-in ACLs. Re-enumerated on every build so the
	// sets track interface changes across reloads. On failure, log and continue
	// with empty sets (localhost/localnets then match nothing — fail-closed).
	localhostNets, localnetsNets, lerr := view.LocalInterfaceNets()
	if lerr != nil {
		logger.Sugar().Warnw("could not enumerate local interfaces; localhost/localnets match-clients will match nothing",
			"error", lerr)
	}

	matcher := &view.Matcher{
		Views:         viewRuleSets,
		Country:       country,
		ASN:           asn,
		LocalhostNets: localhostNets,
		LocalnetsNets: localnetsNets,
	}

	acl, err := transfer.NewACL(cfg.Options.AllowTransfer)
	if err != nil {
		return ServerState{}, BuildSummary{}, fmt.Errorf("building allow-transfer ACL: %w", err)
	}

	return ServerState{
		Matcher:            matcher,
		Aliases:            aliases,
		AliasFlags:         aliasFlags,
		CollapseFlags:      collapseFlags,
		BackupOriginalCase: backupOriginalCase,
		RootZones:          rootZones,
		BackupZones:        backupZones,
		ZoneOrigins:        zoneOrigins,
		AllowTransferACL:   acl,
		Fingerprints:       fingerprints,
	}, summary, nil
}

// loadZone returns the *zone.Zone for the given zone config entry. When the
// fingerprint of the zone file matches the one stored in prev (and mode allows
// reuse), the existing pointer from prev is returned without re-reading the
// file. Otherwise the zone file is parsed from disk.
//
// The returned zoneFingerprint is always the freshly-computed one for the
// current file on disk (or a zero value for VerifyModeNone).
// The returned bool is true when the zone pointer was reused from prev,
// false when the zone was parsed from disk.
func loadZone(
	z config.Zone,
	origin string,
	prev *ServerState,
	viewName string,
	mode VerifyMode,
	aliases config.AliasMap,
	logger *zap.Logger,
) (*zone.Zone, zoneFingerprint, bool, error) {
	fp, err := computeFingerprint(z.File, mode)
	if err != nil {
		return nil, zoneFingerprint{}, false, fmt.Errorf("fingerprint %q: %w", z.File, err)
	}

	if prev != nil && mode != VerifyModeNone {
		// Alias-membership check: skip reuse only when the origin flipped
		// between Root and BackupOverride, because zone.Classify would then
		// produce different z.Role and a different filtered z.Records.
		//
		// INVARIANT — safe to reuse across alias target changes: zone.Classify
		// only reads aliases[z.Origin] as a presence check (key lookup), not
		// the mapped value. And the query-time fallback target comes from
		// state.Aliases, which is rebuilt fresh every BuildState and never
		// reused. So a backup zone whose alias target moved from A → B keeps
		// its correct z.Records filter, while Detect() at query time sees the
		// new target B. If zone.Classify ever starts depending on the alias
		// target (the mapped value), this reuse condition must be tightened.
		prevIsBackup := prev.BackupZones[viewName][origin] != nil
		_, nowIsBackup := aliases[origin]
		if prevIsBackup == nowIsBackup {
			if prevFPs, ok := prev.Fingerprints[viewName]; ok {
				if prevFP, ok := prevFPs[origin]; ok {
					if !fp.changed(prevFP, mode) {
						if z := prev.RootZones[viewName][origin]; z != nil {
							return z, fp, true, nil
						}
						if z := prev.BackupZones[viewName][origin]; z != nil {
							return z, fp, true, nil
						}
					}
				}
			}
		}
	}

	parsed, err := zone.ParseFile(z.File, origin, logger)
	if err != nil {
		return nil, zoneFingerprint{}, false, err
	}
	return parsed, fp, false, nil
}
