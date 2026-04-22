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
)

// Config is the parsed unified ShadowDNS configuration.
type Config struct {
	Aliases      config.AliasMap
	EphemeralAPI *EphemeralAPIConfig
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
	Aliases      map[string]string `yaml:"aliases"`
	EphemeralAPI *rawEphemeralAPI  `yaml:"ephemeral_api"`
}

type rawEphemeralAPI struct {
	Listen string   `yaml:"listen"`
	Allow  []string `yaml:"allow"`
	Token  string   `yaml:"token"`
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
	defer f.Close()

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

	aliasMap, err := config.BuildAliasMap(raw.Aliases)
	if err != nil {
		return nil, fmt.Errorf("aliases: %w", err)
	}
	cfg.Aliases = aliasMap

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
