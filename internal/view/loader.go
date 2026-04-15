package view

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// countryMMDBCandidates lists the country mmdb basenames the loader tries in
// priority order. Paid GeoIP2 first, free GeoLite2 second; mirrors BIND9's
// bin/named/geoip.c fallback chain so users can drop in either edition.
var countryMMDBCandidates = []string{
	"GeoIP2-Country.mmdb",
	"GeoLite2-Country.mmdb",
}

// asnMMDBCandidates lists the ASN mmdb basenames in priority order.
var asnMMDBCandidates = []string{
	"GeoIP2-ASN.mmdb",
	"GeoLite2-ASN.mmdb",
}

// LoadGeoIP opens a country mmdb and an ASN mmdb from dir. For each database
// it tries candidate filenames in priority order (GeoIP2 first, then
// GeoLite2) and accepts the first that opens successfully and passes mmdb
// validation. Country and ASN are resolved independently, so mixing editions
// is allowed.
//
// If every candidate for either database fails, the returned error names
// every attempted path together with the per-attempt failure reason. The
// caller (main) should treat any error as fatal startup.
//
// On success, logger receives an info message per opened file with the full
// path in the `path` attr so operators can identify the edition in use.
//
// MUST NOT panic.
func LoadGeoIP(dir string, logger *slog.Logger) (country *CountryDB, asn *ASNDB, err error) {
	countryDB, countryPath, err := openFirstMMDB(dir, countryMMDBCandidates, "country", OpenCountryDB)
	if err != nil {
		return nil, nil, err
	}
	if logger != nil {
		logger.Info("loaded GeoIP country database", "path", countryPath)
	}

	asnDB, asnPath, err := openFirstMMDB(dir, asnMMDBCandidates, "ASN", OpenASNDB)
	if err != nil {
		// Avoid leaking the country file handle when the ASN open fails.
		_ = countryDB.Close()
		return nil, nil, err
	}
	if logger != nil {
		logger.Info("loaded GeoIP ASN database", "path", asnPath)
	}

	return countryDB, asnDB, nil
}

// openFirstMMDB walks candidates in priority order, returning the first DB
// that opens successfully along with its full path. On total failure the
// error lists every attempted path with its underlying reason.
func openFirstMMDB[T any](dir string, candidates []string, kind string, open func(string) (T, error)) (T, string, error) {
	var zero T
	attempts := make([]string, 0, len(candidates))
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		db, err := open(path)
		if err == nil {
			return db, path, nil
		}
		attempts = append(attempts, fmt.Sprintf("%q: %v", path, err))
	}
	return zero, "", fmt.Errorf("failed to open GeoIP %s database; tried %s",
		kind, strings.Join(attempts, ", "))
}
