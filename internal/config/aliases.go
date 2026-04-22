package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// AliasMap is a one-way lookup: backup domain (FQDN, lowercased, with trailing dot)
// → root domain (same normalization). The map is empty when no aliases are declared.
type AliasMap map[string]string

// LoadAliases reads and parses aliases.yaml from disk.
//
// `path` may be empty or refer to a non-existent file; in either case the
// function returns an empty AliasMap and logs an info message via `logger`.
//
// Returns a fatal error in these cases:
//   - YAML is syntactically invalid
//   - the same backup domain appears under two different roots
//   - a backup equals its declared root (self-alias)
//   - any domain is empty or not a valid DNS name
//
// `logger` MUST NOT be nil; the caller is responsible for passing a real one
// (or `zap.NewNop()`).
//
// The function MUST not panic on any input.
func LoadAliases(path string, logger *zap.Logger) (AliasMap, error) {
	// Handle missing or empty path gracefully.
	if path == "" {
		logger.Info("aliases file path not provided; starting with empty alias map")
		return AliasMap{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Sugar().Infow("aliases file not found; starting with empty alias map", "path", path)
			return AliasMap{}, nil
		}
		return nil, fmt.Errorf("reading aliases file %q: %w", path, err)
	}

	var raw map[string][]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing aliases file %q: %w", path, err)
	}

	// Invert root→[backups] to backup→root. The YAML ordering of `backups`
	// is preserved by yaml.v3, so "same backup under two different roots"
	// still produces distinct successive writes; BuildAliasMap catches the
	// conflict after normalization.
	flat := make(map[string]string)
	for root, backups := range raw {
		for _, backup := range backups {
			if existing, dup := flat[backup]; dup && existing != root {
				return nil, fmt.Errorf(
					"aliases file %q: backup domain %q is claimed by two roots: %q and %q",
					path, backup, existing, root,
				)
			}
			flat[backup] = root
		}
	}
	result, err := BuildAliasMap(flat)
	if err != nil {
		return nil, fmt.Errorf("aliases file %q: %w", path, err)
	}
	return result, nil
}

// BuildAliasMap validates and normalizes a raw backup→root map supplied by a
// config loader (e.g., shadowdnscfg) and returns the canonical AliasMap.
//
// Returns an error when:
//   - any domain is empty or contains whitespace,
//   - the same backup domain (after normalization) is mapped to two different
//     roots,
//   - an entry lists a backup equal to its root (self-alias).
func BuildAliasMap(raw map[string]string) (AliasMap, error) {
	result := make(AliasMap, len(raw))
	for backup, root := range raw {
		normBackup, err := normalizeDomain(backup)
		if err != nil {
			return nil, fmt.Errorf("invalid backup domain %q: %w", backup, err)
		}
		normRoot, err := normalizeDomain(root)
		if err != nil {
			return nil, fmt.Errorf("invalid root domain %q for backup %q: %w", root, backup, err)
		}
		if normBackup == normRoot {
			return nil, fmt.Errorf("self-alias not allowed: %q is listed as a backup of itself", normRoot)
		}
		if existingRoot, exists := result[normBackup]; exists && existingRoot != normRoot {
			return nil, fmt.Errorf(
				"backup domain %q is claimed by two roots: %q and %q",
				normBackup, existingRoot, normRoot,
			)
		}
		result[normBackup] = normRoot
	}
	return result, nil
}

// normalizeDomain validates a domain name and returns its canonical form
// (lowercased, with a trailing dot). It rejects empty strings and names
// containing whitespace before delegating the pure transformation to
// dnsutil.Canonicalize.
func normalizeDomain(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("domain name must not be empty")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return "", fmt.Errorf("domain name %q must not contain whitespace", name)
	}
	return dnsutil.Canonicalize(name), nil
}
