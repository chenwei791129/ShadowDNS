package view

import (
	"fmt"
	"log/slog"
	"path/filepath"
)

const (
	countryMMDBFilename = "GeoLite2-Country.mmdb"
	asnMMDBFilename     = "GeoLite2-ASN.mmdb"
)

// LoadGeoIP opens GeoLite2-Country.mmdb and GeoLite2-ASN.mmdb from dir.
// If either file is missing or fails mmdb validation, it returns an error
// whose message names the offending path. The caller (main) should Fatal on it.
//
// On success, logger receives info messages for each successfully opened file.
// MUST NOT panic.
func LoadGeoIP(dir string, logger *slog.Logger) (country *CountryDB, asn *ASNDB, err error) {
	countryPath := filepath.Join(dir, countryMMDBFilename)
	countryDB, err := OpenCountryDB(countryPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open GeoIP country database %q: %w", countryPath, err)
	}
	if logger != nil {
		logger.Info("loaded GeoIP country database", "path", countryPath)
	}

	asnPath := filepath.Join(dir, asnMMDBFilename)
	asnDB, err := OpenASNDB(asnPath)
	if err != nil {
		// Close the already-opened country DB to avoid leaking the file handle.
		_ = countryDB.Close()
		return nil, nil, fmt.Errorf("failed to open GeoIP ASN database %q: %w", asnPath, err)
	}
	if logger != nil {
		logger.Info("loaded GeoIP ASN database", "path", asnPath)
	}

	return countryDB, asnDB, nil
}
