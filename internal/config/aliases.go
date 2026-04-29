package config

import (
	"fmt"
	"strings"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// AliasMap is a one-way lookup: backup domain (FQDN, lowercase-folded for
// case-insensitive matching per RFC 4343, with trailing dot) → root domain
// (same fold). Both keys and values are derived via dnsutil.LookupKey; the
// fold is for comparison/lookup only, original case is preserved on
// AliasGroup.Members and on the backups field below for on-wire output.
//
// The map is empty when no aliases are declared.
type AliasMap map[string]string

// AliasFlags is a backup-domain → rewrite_rdata_labels lookup keyed on the
// LookupKey fold of the backup. A missing key is equivalent to false
// (default behavior: in-bailiwick suffix-only rewrite).
type AliasFlags map[string]bool

// AliasGroup describes one root-keyed alias group: its backup members and
// the per-group rewrite_rdata_labels flag. Members are pre-normalization
// strings as supplied by the loader (yaml original case is preserved
// byte-for-byte); BuildAliasMap derives the lookup-fold for the AliasMap
// key without mutating Members.
type AliasGroup struct {
	Members            []string
	RewriteRDATALabels bool
}

// BuildAliasMap validates a root → AliasGroup map and emits two flat
// lookup tables for runtime use:
//   - AliasMap: backup → root (canonicalized FQDNs)
//   - AliasFlags: backup → group's RewriteRDATALabels flag
//
// Returns an error when:
//   - any domain is empty or contains whitespace,
//   - the same backup domain (after normalization) appears under two
//     different roots,
//   - an entry lists a backup equal to its root (self-alias).
//
// A root whose Members list is empty contributes nothing to either map but
// is not an error (matches the YAML "empty list is valid" semantics).
func BuildAliasMap(raw map[string]AliasGroup) (AliasMap, AliasFlags, error) {
	resultMap := make(AliasMap)
	resultFlags := make(AliasFlags)

	for root, group := range raw {
		normRoot, err := normalizeDomain(root)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid root domain %q: %w", root, err)
		}

		for _, backup := range group.Members {
			normBackup, err := normalizeDomain(backup)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid backup domain %q under root %q: %w", backup, root, err)
			}
			if normBackup == normRoot {
				return nil, nil, fmt.Errorf("self-alias not allowed: %q is listed as a backup of itself", normRoot)
			}
			if existingRoot, exists := resultMap[normBackup]; exists && existingRoot != normRoot {
				return nil, nil, fmt.Errorf(
					"backup domain %q is claimed by two roots: %q and %q",
					normBackup, existingRoot, normRoot,
				)
			}
			resultMap[normBackup] = normRoot
			resultFlags[normBackup] = group.RewriteRDATALabels
		}
	}
	return resultMap, resultFlags, nil
}

// normalizeDomain validates a domain name and returns its lookup-fold form
// (lowercased, with a trailing dot) suitable as an AliasMap key/value per
// RFC 4343 case-insensitive matching. It rejects empty strings and names
// containing whitespace before delegating the pure fold transformation to
// dnsutil.LookupKey. Original yaml case is preserved on AliasGroup.Members;
// this helper is used only for the case-insensitive comparison path.
func normalizeDomain(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("domain name must not be empty")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return "", fmt.Errorf("domain name %q must not contain whitespace", name)
	}
	return dnsutil.LookupKey(name), nil
}
