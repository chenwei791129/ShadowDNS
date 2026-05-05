// Package shadowdnscfg loads and validates the unified ShadowDNS YAML
// configuration file. The file contains two optional top-level sections,
// aliases and ephemeral_api, each independently validated; any failure
// leaves the previous in-memory configuration untouched (see SIGHUP reload
// handling in cmd/shadowdns).
package shadowdnscfg

import (
	"fmt"
	"net"
	"net/netip"
	"os"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// Config is the parsed unified ShadowDNS configuration.
//
// Aliases and AliasFlags are keyed by the lookup-fold backup FQDN (lowercase,
// trailing dot) per RFC 4343 case-insensitive matching. BackupOriginalCase
// maps that same fold key back to the operator-authored case (with trailing
// dot) so the alias rewrite path can emit on-wire names that preserve the
// case the operator wrote in YAML.
type Config struct {
	Aliases            config.AliasMap
	AliasFlags         config.AliasFlags
	BackupOriginalCase map[string]string
	EphemeralAPI       *EphemeralAPIConfig
}

// EphemeralAPIConfig holds the settings for the ephemeral TXT API server.
type EphemeralAPIConfig struct {
	// Listen is the host:port to bind, as provided in YAML (e.g. "127.0.0.1:8053").
	Listen string
	// Allow is the non-empty list of permitted source prefixes. Single IPs in
	// YAML are normalized into /32 or /128 prefixes here.
	Allow []netip.Prefix
	// Token is an optional pre-shared bearer token. Empty means token
	// authentication is disabled.
	Token string
}

type rawConfig struct {
	Aliases      map[string]rawAliasGroup `yaml:"aliases"`
	EphemeralAPI *rawEphemeralAPI         `yaml:"ephemeral_api"`
}

type rawEphemeralAPI struct {
	Listen string   `yaml:"listen"`
	Allow  []string `yaml:"allow"`
	Token  string   `yaml:"token"`
}

// rawAliasGroup is the per-key YAML shape for the aliases map. Each entry
// MUST be a mapping with a non-empty `members` list and an optional
// `rewrite_rdata_labels` bool (default false). Strict decoding rejects any
// other YAML node type (sequence, bare string) and unknown fields inside
// the mapping.
type rawAliasGroup struct {
	Members            []string `yaml:"members"`
	RewriteRDATALabels bool     `yaml:"rewrite_rdata_labels"`
}

// UnmarshalYAML accepts only the mapping form for each alias entry. Any
// other YAML node type (sequence of backup strings, bare string, etc.)
// yields a type-mismatch error so misshapen configs fail loudly.
func (g *rawAliasGroup) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: aliases entry must be an object with 'members' (non-empty list of backup domains) and optional 'rewrite_rdata_labels' (bool)", value.Line)
	}
	// yaml.Node.Decode does not honor the parent decoder's KnownFields
	// setting, so we walk the mapping content directly to reject unknown
	// keys before decoding into g.
	allowed := map[string]bool{"members": true, "rewrite_rdata_labels": true}
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		if !allowed[keyNode.Value] {
			return fmt.Errorf("line %d: unknown alias field %q (expected one of: members, rewrite_rdata_labels)", keyNode.Line, keyNode.Value)
		}
	}
	// Avoid recursing into this UnmarshalYAML by decoding into a distinct
	// named type that shares the same field tags.
	type aliasGroupAlias rawAliasGroup
	var obj aliasGroupAlias
	if err := value.Decode(&obj); err != nil {
		return err
	}
	if len(obj.Members) == 0 {
		return fmt.Errorf("line %d: aliases entry requires non-empty 'members' list", value.Line)
	}
	*g = rawAliasGroup(obj)
	return nil
}

// Load parses and validates the unified ShadowDNS YAML config file at path.
// Strict decoding is used: unknown top-level keys or unknown fields inside
// recognized sections are rejected. Passing a nil logger is safe.
func Load(path string, logger *zap.Logger) (*Config, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening config %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var raw rawConfig
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	cfg := &Config{}

	// Surface the "no aliases declared" case so operators can tell an
	// intentional empty config from a typo that makes BuildAliasMap silently
	// skip entries.
	if raw.Aliases == nil {
		logger.Sugar().Infow("config has no aliases section; starting with empty alias map", "path", path)
	}

	// Translate the per-root rawAliasGroup map into the canonical
	// config.AliasGroup map BuildAliasMap expects.
	groups := make(map[string]config.AliasGroup, len(raw.Aliases))
	for root, g := range raw.Aliases {
		groups[root] = config.AliasGroup{
			Members:            g.Members,
			RewriteRDATALabels: g.RewriteRDATALabels,
		}
	}

	aliasMap, aliasFlags, err := config.BuildAliasMap(groups)
	if err != nil {
		return nil, fmt.Errorf("aliases: %w", err)
	}
	cfg.Aliases = aliasMap
	cfg.AliasFlags = aliasFlags

	// Snapshot operator-authored backup case (FQDN form) keyed by the same
	// lookup-fold used by Aliases / AliasFlags. The alias rewrite path reads
	// this when emitting on-wire names so case-randomized 0x20 queries and
	// mixed-case YAML configs are echoed back with their original case.
	backupOriginal := make(map[string]string, len(aliasMap))
	for _, g := range raw.Aliases {
		for _, backup := range g.Members {
			backupOriginal[dnsutil.LookupKey(backup)] = dnsutil.Canonicalize(backup)
		}
	}
	cfg.BackupOriginalCase = backupOriginal

	if raw.EphemeralAPI != nil {
		apiCfg, err := buildEphemeralAPI(raw.EphemeralAPI)
		if err != nil {
			return nil, fmt.Errorf("ephemeral_api: %w", err)
		}
		cfg.EphemeralAPI = apiCfg
	}

	return cfg, nil
}

func buildEphemeralAPI(raw *rawEphemeralAPI) (*EphemeralAPIConfig, error) {
	if raw.Listen == "" {
		return nil, fmt.Errorf("listen: required field is missing")
	}
	if _, _, err := net.SplitHostPort(raw.Listen); err != nil {
		return nil, fmt.Errorf("listen: invalid host:port %q: %w", raw.Listen, err)
	}
	if len(raw.Allow) == 0 {
		return nil, fmt.Errorf("allow: at least one IP or CIDR is required")
	}
	prefixes := make([]netip.Prefix, 0, len(raw.Allow))
	for i, entry := range raw.Allow {
		prefix, err := parseIPOrCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("allow[%d]: %w", i, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return &EphemeralAPIConfig{
		Listen: raw.Listen,
		Allow:  prefixes,
		Token:  raw.Token,
	}, nil
}

func parseIPOrCIDR(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		// Keep the stored prefix symmetric with remote IPs seen by the
		// API's ipACLMiddleware, which calls .Unmap() on incoming
		// connections. Without this, a config entry in IPv4-mapped
		// IPv6 form would silently fail to match the same address.
		addr := p.Addr().Unmap()
		bits := p.Bits()
		if addr.Is4() && bits > 32 {
			bits -= 96
		}
		return netip.PrefixFrom(addr, bits), nil
	}
	if a, err := netip.ParseAddr(s); err == nil {
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	return netip.Prefix{}, fmt.Errorf("invalid IP or CIDR: %q", s)
}
