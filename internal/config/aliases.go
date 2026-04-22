package config

import (
	"fmt"
	"strings"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// AliasMap is a one-way lookup: backup domain (FQDN, lowercased, with trailing dot)
// → root domain (same normalization). The map is empty when no aliases are declared.
type AliasMap map[string]string

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
