package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
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
// (or `slog.Default()`).
//
// The function MUST not panic on any input.
func LoadAliases(path string, logger *slog.Logger) (AliasMap, error) {
	// Handle missing or empty path gracefully.
	if path == "" {
		logger.Info("aliases file path not provided; starting with empty alias map")
		return AliasMap{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Info("aliases file not found; starting with empty alias map", "path", path)
			return AliasMap{}, nil
		}
		return nil, fmt.Errorf("reading aliases file %q: %w", path, err)
	}

	// Decode YAML into a raw map[string][]string.
	var raw map[string][]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing aliases file %q: %w", path, err)
	}

	// An empty or null YAML document results in a nil map — treat as empty.
	result := make(AliasMap, len(raw))

	for root, backups := range raw {
		normalizedRoot, err := normalizeDomain(root)
		if err != nil {
			return nil, fmt.Errorf("invalid root domain %q in aliases file %q: %w", root, path, err)
		}

		for _, backup := range backups {
			normalizedBackup, err := normalizeDomain(backup)
			if err != nil {
				return nil, fmt.Errorf("invalid backup domain %q under root %q in aliases file %q: %w", backup, root, path, err)
			}

			// Reject self-aliases.
			if normalizedBackup == normalizedRoot {
				return nil, fmt.Errorf("self-alias not allowed: %q is listed as a backup of itself", normalizedRoot)
			}

			// Reject duplicate backups across different roots.
			if existingRoot, exists := result[normalizedBackup]; exists && existingRoot != normalizedRoot {
				return nil, fmt.Errorf(
					"backup domain %q is claimed by two roots: %q and %q",
					normalizedBackup, existingRoot, normalizedRoot,
				)
			}

			result[normalizedBackup] = normalizedRoot
		}
	}

	return result, nil
}

// normalizeDomain converts a domain name to lowercase with a trailing dot.
// It rejects empty strings and names containing whitespace.
func normalizeDomain(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("domain name must not be empty")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return "", fmt.Errorf("domain name %q must not contain whitespace", name)
	}
	normalized := strings.TrimSuffix(strings.ToLower(name), ".") + "."
	return normalized, nil
}
