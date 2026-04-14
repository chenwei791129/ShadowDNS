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

	// Create output directories.
	for _, d := range []string{"geoip", "master"} {
		if err := os.MkdirAll(filepath.Join(*outDir, d), 0o755); err != nil {
			log.Fatalf("mkdir: %v", err)
		}
	}

	// Copy zone files.
	masterFiles, _ := os.ReadDir(filepath.Join(fixtureDir, "master"))
	for _, f := range masterFiles {
		copyFile(
			filepath.Join(fixtureDir, "master", f.Name()),
			filepath.Join(*outDir, "master", f.Name()),
		)
	}

	// Copy and patch named.conf.
	data, err := os.ReadFile(filepath.Join(fixtureDir, "named.conf"))
	if err != nil {
		log.Fatalf("read named.conf: %v", err)
	}
	patched := strings.ReplaceAll(string(data), "TESTDATA_DIR_PLACEHOLDER", *targetDir)
	patched = strings.ReplaceAll(patched,
		`include "master.zones";`,
		`include "`+filepath.Join(*targetDir, "master.zones")+`";`)
	if err := os.WriteFile(filepath.Join(*outDir, "named.conf"), []byte(patched), 0o644); err != nil {
		log.Fatalf("write named.conf: %v", err)
	}

	// Copy and patch master.zones.
	data, err = os.ReadFile(filepath.Join(fixtureDir, "master.zones"))
	if err != nil {
		log.Fatalf("read master.zones: %v", err)
	}
	patched = strings.ReplaceAll(string(data),
		`file "master/`,
		`file "`+filepath.Join(*targetDir, "master")+`/`)
	if err := os.WriteFile(filepath.Join(*outDir, "master.zones"), []byte(patched), 0o644); err != nil {
		log.Fatalf("write master.zones: %v", err)
	}

	// Copy aliases.yaml.
	copyFile(
		filepath.Join(fixtureDir, "aliases.yaml"),
		filepath.Join(*outDir, "aliases.yaml"),
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
