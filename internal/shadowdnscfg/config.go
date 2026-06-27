// Package shadowdnscfg loads and validates the unified ShadowDNS YAML
// configuration file. The file contains optional top-level sections —
// aliases, ephemeral_api, and doh — each independently validated; any failure
// leaves the previous in-memory configuration untouched (see SIGHUP reload
// handling in cmd/shadowdns).
package shadowdnscfg

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"

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
	CollapseFlags      config.CollapseFlags
	BackupOriginalCase map[string]string
	EphemeralAPI       *EphemeralAPIConfig
	// DoH holds the DNS-over-HTTPS server settings. Nil when the doh section
	// is absent, in which case no DoH/ACME/HTTP-01 listeners are started.
	DoH *DoHConfig
}

// DoHConfig holds the settings for the DNS-over-HTTPS (RFC 8484) server.
// Present only when the doh section appears in the YAML; every field is
// required and validated by buildDoH so a partial section fails the load.
type DoHConfig struct {
	// Listen is the host:port the DoH HTTPS service binds, as provided in
	// YAML (e.g. "203.0.113.10:443").
	Listen string
	// ACME holds the certificate-issuance settings. Always populated when
	// DoHConfig is non-nil (the acme section is required).
	ACME DoHACMEConfig
}

// DoHACMEConfig holds the ACME settings used to obtain and renew the DoH
// server's TLS certificate for an IP address (RFC 8738) via HTTP-01.
type DoHACMEConfig struct {
	// DirectoryURL is the ACME directory URL of the issuing CA.
	DirectoryURL string
	// IP is the IP address the certificate is issued for, parsed from YAML
	// so a malformed value fails the load rather than surfacing later as an
	// ACME order error.
	IP netip.Addr
	// HTTP01Listen is the host:port the ACME HTTP-01 challenge responder
	// binds; it MUST be reachable from the public Internet as port 80.
	HTTP01Listen string
	// AccountKeyFile is the absolute path to the persisted ACME account
	// private key (PKCS#8 PEM, mode 0600). The key is generated on first use
	// when the file is absent and reused across restarts and registration
	// retries so re-registration is idempotent and does not consume the
	// per-source-IP new-account rate limit.
	AccountKeyFile string
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
	DoH          *rawDoH                  `yaml:"doh"`
}

type rawEphemeralAPI struct {
	Listen string   `yaml:"listen"`
	Allow  []string `yaml:"allow"`
	Token  string   `yaml:"token"`
}

type rawDoH struct {
	Listen string      `yaml:"listen"`
	ACME   *rawDoHACME `yaml:"acme"`
}

type rawDoHACME struct {
	DirectoryURL   string `yaml:"directory_url"`
	IP             string `yaml:"ip"`
	HTTP01Listen   string `yaml:"http01_listen"`
	AccountKeyFile string `yaml:"account_key_file"`
}

// rawAliasGroup is the per-key YAML shape for the aliases map. Each entry
// MUST be a mapping with a non-empty `members` list and optional
// `rewrite_rdata_labels` / `collapse_cname_chain` bools (default false).
// Strict decoding rejects any other YAML node type (sequence, bare string)
// and unknown fields inside the mapping.
type rawAliasGroup struct {
	Members            []string `yaml:"members"`
	RewriteRDATALabels bool     `yaml:"rewrite_rdata_labels"`
	CollapseCNAMEChain bool     `yaml:"collapse_cname_chain"`
}

// UnmarshalYAML accepts only the mapping form for each alias entry. Any
// other YAML node type (sequence of backup strings, bare string, etc.)
// yields a type-mismatch error so misshapen configs fail loudly.
func (g *rawAliasGroup) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: aliases entry must be an object with 'members' (non-empty list of backup domains) and optional 'rewrite_rdata_labels' / 'collapse_cname_chain' (bool)", value.Line)
	}
	// yaml.Node.Decode does not honor the parent decoder's KnownFields
	// setting, so we walk the mapping content directly to reject unknown
	// keys before decoding into g.
	allowed := map[string]bool{"members": true, "rewrite_rdata_labels": true, "collapse_cname_chain": true}
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		if !allowed[keyNode.Value] {
			return fmt.Errorf("line %d: unknown alias field %q (expected one of: members, rewrite_rdata_labels, collapse_cname_chain)", keyNode.Line, keyNode.Value)
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
			CollapseCNAMEChain: g.CollapseCNAMEChain,
		}
	}

	aliasMap, aliasFlags, collapseFlags, err := config.BuildAliasMap(groups)
	if err != nil {
		return nil, fmt.Errorf("aliases: %w", err)
	}
	cfg.Aliases = aliasMap
	cfg.AliasFlags = aliasFlags
	cfg.CollapseFlags = collapseFlags

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

	if raw.DoH != nil {
		dohCfg, err := buildDoH(raw.DoH)
		if err != nil {
			return nil, fmt.Errorf("doh: %w", err)
		}
		cfg.DoH = dohCfg
	}

	return cfg, nil
}

// validateHostPort fails the load when addr is not a host:port with a usable
// port. net.SplitHostPort alone accepts "host:" (empty port) and "host:bogus"
// (unknown service), which would pass config validation and only surface later
// as a non-fatal bind error inside the DoH goroutine — leaving DoH silently
// dead. net.LookupPort rejects an empty or unresolvable port while still
// allowing a named port (e.g. "https") that the listener would accept, so this
// matches what the eventual bind will tolerate.
func validateHostPort(field, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s: invalid host:port %q: %w", field, addr, err)
	}
	// net.LookupPort accepts an empty port string on some platforms, so reject
	// it explicitly; a listener bound to ":0"-equivalent here is never intended.
	if port == "" {
		return fmt.Errorf("%s: missing port in %q", field, addr)
	}
	portNum, err := net.LookupPort("tcp", port)
	if err != nil {
		return fmt.Errorf("%s: invalid port in %q: %w", field, addr, err)
	}
	// LookupPort resolves "0" to 0 without error, but binding to port 0 picks a
	// random ephemeral port — never what an operator intends for a DoH/HTTP-01
	// listener that must answer on a fixed port, so reject it.
	if portNum == 0 {
		return fmt.Errorf("%s: port 0 is not allowed in %q", field, addr)
	}
	return nil
}

// buildDoH validates the raw doh section and normalizes it into a DoHConfig.
// Every field is required: a missing or malformed field fails the load with an
// error naming the field, mirroring buildEphemeralAPI's fail-loud contract.
func buildDoH(raw *rawDoH) (*DoHConfig, error) {
	if raw.Listen == "" {
		return nil, fmt.Errorf("listen: required field is missing")
	}
	if err := validateHostPort("listen", raw.Listen); err != nil {
		return nil, err
	}
	if raw.ACME == nil {
		return nil, fmt.Errorf("acme: required section is missing")
	}
	acme, err := buildDoHACME(raw.ACME)
	if err != nil {
		return nil, fmt.Errorf("acme: %w", err)
	}
	return &DoHConfig{Listen: raw.Listen, ACME: acme}, nil
}

func buildDoHACME(raw *rawDoHACME) (DoHACMEConfig, error) {
	if raw.DirectoryURL == "" {
		return DoHACMEConfig{}, fmt.Errorf("directory_url: required field is missing")
	}
	u, err := url.Parse(raw.DirectoryURL)
	if err != nil {
		return DoHACMEConfig{}, fmt.Errorf("directory_url: invalid URL %q: %w", raw.DirectoryURL, err)
	}
	// Require https: ACME account registration and certificate issuance over
	// plaintext http are exposed to on-path tampering, and real ACME CAs serve
	// their directory only over https. Rejecting http is a secure default.
	if u.Scheme != "https" || u.Host == "" {
		return DoHACMEConfig{}, fmt.Errorf("directory_url: %q must be an absolute https:// URL", raw.DirectoryURL)
	}
	if raw.IP == "" {
		return DoHACMEConfig{}, fmt.Errorf("ip: required field is missing")
	}
	ip, err := netip.ParseAddr(raw.IP)
	if err != nil {
		return DoHACMEConfig{}, fmt.Errorf("ip: invalid IP address %q: %w", raw.IP, err)
	}
	if raw.HTTP01Listen == "" {
		return DoHACMEConfig{}, fmt.Errorf("http01_listen: required field is missing")
	}
	if err := validateHostPort("http01_listen", raw.HTTP01Listen); err != nil {
		return DoHACMEConfig{}, err
	}
	if raw.AccountKeyFile == "" {
		return DoHACMEConfig{}, fmt.Errorf("account_key_file: required field is missing")
	}
	// Require an absolute path: the key is read and written by the service from
	// a systemd StateDirectory, where a relative path would resolve against the
	// daemon's working directory rather than the intended persistent location.
	if !filepath.IsAbs(raw.AccountKeyFile) {
		return DoHACMEConfig{}, fmt.Errorf("account_key_file: %q must be an absolute path", raw.AccountKeyFile)
	}
	return DoHACMEConfig{
		DirectoryURL:   raw.DirectoryURL,
		IP:             ip.Unmap(),
		HTTP01Listen:   raw.HTTP01Listen,
		AccountKeyFile: raw.AccountKeyFile,
	}, nil
}

func buildEphemeralAPI(raw *rawEphemeralAPI) (*EphemeralAPIConfig, error) {
	if raw.Listen == "" {
		return nil, fmt.Errorf("listen: required field is missing")
	}
	if err := validateHostPort("listen", raw.Listen); err != nil {
		return nil, err
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
