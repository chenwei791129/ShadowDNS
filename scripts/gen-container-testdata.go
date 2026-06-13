// Command gen-container-testdata prepares a ready-to-use ShadowDNS config
// directory for container testing. It copies the integration test fixtures,
// generates mock GeoIP mmdb files, and patches all paths to use the target
// directory.
//
// Usage:
//
//	go run scripts/gen-container-testdata.go -out /path/to/output
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func main() {
	outDir := flag.String("out", "", "output directory (required)")
	targetDir := flag.String("target", "", "target path inside container (defaults to -out value)")
	flag.Parse()

	if *outDir == "" {
		log.Fatal("-out is required")
	}
	if *targetDir == "" {
		*targetDir = *outDir
	}

	fixtureDir := filepath.Join("testdata", "integration")

	// Create output directories. The Debian layout keeps zone files flat
	// beside the config; only the nested cnames/ $INCLUDE fragment dir remains.
	for _, d := range []string{"geoip", "cnames"} {
		if err := os.MkdirAll(filepath.Join(*outDir, d), 0o755); err != nil {
			log.Fatalf("mkdir: %v", err)
		}
	}

	// Copy the flat db.* zone files (and the db.*.overrides $INCLUDE fragment)
	// from the fixture top level, plus the nested cnames/ fragments.
	fixtureEntries, err := os.ReadDir(fixtureDir)
	if err != nil {
		log.Fatalf("readdir fixtures: %v", err)
	}
	for _, e := range fixtureEntries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "db.") {
			copyFile(
				filepath.Join(fixtureDir, e.Name()),
				filepath.Join(*outDir, e.Name()),
			)
		}
	}
	cnamesDir := filepath.Join(fixtureDir, "cnames")
	cnameEntries, err := os.ReadDir(cnamesDir)
	if err != nil {
		log.Fatalf("readdir cnames: %v", err)
	}
	for _, e := range cnameEntries {
		if !e.IsDir() {
			copyFile(
				filepath.Join(cnamesDir, e.Name()),
				filepath.Join(*outDir, "cnames", e.Name()),
			)
		}
	}

	// Copy and patch named.conf (only the two includes; rewrite to absolute
	// target paths — no TESTDATA_DIR_PLACEHOLDER lives here, it's in
	// named.conf.options).
	data, err := os.ReadFile(filepath.Join(fixtureDir, "named.conf"))
	if err != nil {
		log.Fatalf("read named.conf: %v", err)
	}
	patched := string(data)
	for _, inc := range []string{"named.conf.options", "named.conf.local"} {
		patched = strings.ReplaceAll(patched,
			`include "`+inc+`";`,
			`include "`+filepath.Join(*targetDir, inc)+`";`)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "named.conf"), []byte(patched), 0o644); err != nil {
		log.Fatalf("write named.conf: %v", err)
	}

	// Copy and patch named.conf.options (placeholder → target for the options{}
	// directory and geoip-directory).
	data, err = os.ReadFile(filepath.Join(fixtureDir, "named.conf.options"))
	if err != nil {
		log.Fatalf("read named.conf.options: %v", err)
	}
	patched = strings.ReplaceAll(string(data), "TESTDATA_DIR_PLACEHOLDER", *targetDir)
	if err := os.WriteFile(filepath.Join(*outDir, "named.conf.options"), []byte(patched), 0o644); err != nil {
		log.Fatalf("write named.conf.options: %v", err)
	}

	// Copy named.conf.local. Zone files use relative `file "db.*"` names that
	// the parser resolves against the options{} directory (the target path), so
	// no path rewrite is needed.
	copyFile(
		filepath.Join(fixtureDir, "named.conf.local"),
		filepath.Join(*outDir, "named.conf.local"),
	)

	// Copy shadowdns.yaml (unified config with aliases section).
	copyFile(
		filepath.Join(fixtureDir, "shadowdns.yaml"),
		filepath.Join(*outDir, "shadowdns.yaml"),
	)

	// Generate mock GeoIP mmdb files.
	buildCountryMMDB(filepath.Join(*outDir, "geoip", "GeoLite2-Country.mmdb"))
	buildASNMMDB(filepath.Join(*outDir, "geoip", "GeoLite2-ASN.mmdb"))

	fmt.Printf("testdata ready at %s (target: %s)\n", *outDir, *targetDir)
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		log.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", dst, err)
	}
}

func buildCountryMMDB(path string) {
	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		log.Fatalf("create country mmdb writer: %v", err)
	}

	insert(w, "0.0.0.0/0", mmdbtype.Map{})
	insert(w, "192.0.2.0/24", mmdbtype.Map{
		"country": mmdbtype.Map{"iso_code": mmdbtype.String("TH")},
	})
	insert(w, "198.51.100.0/24", mmdbtype.Map{
		"country": mmdbtype.Map{"iso_code": mmdbtype.String("JP")},
	})

	writeMMDB(w, path)
}

func buildASNMMDB(path string) {
	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-ASN",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		log.Fatalf("create ASN mmdb writer: %v", err)
	}

	insert(w, "0.0.0.0/0", mmdbtype.Map{})
	insert(w, "203.0.113.0/24", mmdbtype.Map{
		"autonomous_system_number":       mmdbtype.Uint32(64500),
		"autonomous_system_organization": mmdbtype.String("AS64500 Test ASN"),
	})

	writeMMDB(w, path)
}

func insert(w *mmdbwriter.Tree, cidr string, record mmdbtype.Map) {
	_, ipnet, _ := net.ParseCIDR(cidr)
	if err := w.Insert(ipnet, record); err != nil {
		log.Fatalf("insert %s: %v", cidr, err)
	}
}

func writeMMDB(w *mmdbwriter.Tree, path string) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Fatalf("close %s: %v", path, cerr)
		}
	}()
	if _, err := w.WriteTo(f); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
