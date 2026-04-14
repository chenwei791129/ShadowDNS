package server

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// BuildState assembles a ServerState from parsed configuration: it parses
// every zone file into memory, classifies zones as root or backup-override,
// builds the view matcher, and constructs the allow-transfer ACL.
//
// Callers (the shadowdns binary and integration tests) share this so they
// agree on how config becomes in-memory state.
func BuildState(
	cfg *config.Config,
	aliases config.AliasMap,
	country *view.CountryDB,
	asn *view.ASNDB,
	logger *slog.Logger,
) (ServerState, error) {
	rootZones := make(map[string]map[string]*zone.Zone)
	backupZones := make(map[string]map[string]*zone.Zone)
	zoneOrigins := make(map[string][]string)
	viewRuleSets := make([]view.NamedRuleSet, 0, len(cfg.Views))

	for _, v := range cfg.Views {
		viewRuleSets = append(viewRuleSets, view.NamedRuleSet{
			Name:  v.Name,
			Rules: v.MatchClients,
		})

		rootZones[v.Name] = make(map[string]*zone.Zone)
		backupZones[v.Name] = make(map[string]*zone.Zone)
		seenOrigins := make([]string, 0, len(v.Zones))

		for _, z := range v.Zones {
			origin := z.Name + "."
			parsed, err := zone.ParseFile(z.File, origin, logger)
			if err != nil {
				return ServerState{}, fmt.Errorf("view %q zone %q: %w", v.Name, z.Name, err)
			}
			zone.Classify(parsed, aliases, logger)

			switch parsed.Role {
			case zone.RoleBackupOverride:
				backupZones[v.Name][origin] = parsed
			default:
				rootZones[v.Name][origin] = parsed
			}
			seenOrigins = append(seenOrigins, origin)
		}

		// Aliased backups declared in aliases.yaml but missing from master.zones
		// still need an origin entry so Detect() can match them.
		for backup, root := range aliases {
			if _, ok := rootZones[v.Name][root]; !ok {
				continue
			}
			if _, ok := backupZones[v.Name][backup]; ok {
				continue
			}
			if !slices.Contains(seenOrigins, backup) {
				seenOrigins = append(seenOrigins, backup)
			}
		}

		zoneOrigins[v.Name] = seenOrigins
	}

	matcher := &view.Matcher{
		Views:   viewRuleSets,
		Country: country,
		ASN:     asn,
	}

	acl, err := transfer.NewACL(cfg.Options.AllowTransfer)
	if err != nil {
		return ServerState{}, fmt.Errorf("building allow-transfer ACL: %w", err)
	}

	return ServerState{
		Matcher:          matcher,
		Aliases:          aliases,
		RootZones:        rootZones,
		BackupZones:      backupZones,
		ZoneOrigins:      zoneOrigins,
		AllowTransferACL: acl,
	}, nil
}
