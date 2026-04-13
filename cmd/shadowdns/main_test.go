package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func TestRunRequiresNamedConfPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: "",
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	err := run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error when NamedConfPath is empty")
	}
}

// TestRunLoadsAndShutsDownGracefully exercises the full run() pipeline:
// it builds a minimal but valid named.conf + zone file + GeoIP mmdbs in a
// temp dir, starts run() in a goroutine, then cancels ctx and verifies
// that run() returns within a reasonable timeout.
func TestRunLoadsAndShutsDownGracefully(t *testing.T) {
	dir := t.TempDir()
	geoIPDir := filepath.Join(dir, "geoip")
	if err := os.MkdirAll(geoIPDir, 0o755); err != nil {
		t.Fatalf("mkdir geoip: %v", err)
	}
	buildEmptyMMDBs(t, geoIPDir)

	zoneFile := filepath.Join(dir, "example.com.zone")
	if err := os.WriteFile(zoneFile, []byte(minimalZone), 0o644); err != nil {
		t.Fatalf("write zone: %v", err)
	}

	masterZones := filepath.Join(dir, "master.zones")
	masterZonesContent := `view "view-other" {
    match-clients { any; };
    recursion no;
    zone "example.com" {
        type master;
        file "` + zoneFile + `";
    };
};
`
	if err := os.WriteFile(masterZones, []byte(masterZonesContent), 0o644); err != nil {
		t.Fatalf("write master.zones: %v", err)
	}

	namedConf := filepath.Join(dir, "named.conf")
	namedConfContent := `options {
    directory "` + dir + `";
    geoip-directory "` + geoIPDir + `";
    listen-on { any; };
    recursion no;
};

include "` + masterZones + `";
`
	if err := os.WriteFile(namedConf, []byte(namedConfContent), 0o644); err != nil {
		t.Fatalf("write named.conf: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	opts := runOptions{
		NamedConfPath: namedConf,
		AliasesPath:   "",
		ListenAddr:    "127.0.0.1:0",
		Logger:        logger,
	}

	done := make(chan error, 1)
	go func() { done <- run(ctx, opts) }()

	// Give run() time to load and start the listener.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s after context cancellation")
	}
}

const minimalZone = `$TTL 300
@ IN SOA ns1.example.com. hostmaster.example.com. (
    2024010101  ; serial
    3600
    600
    604800
    300
)

@   IN NS    ns1.example.com.
@   IN A     203.0.113.10
ns1 IN A     203.0.113.1
`

// buildEmptyMMDBs creates two minimal valid mmdb files in dir so that
// view.LoadGeoIP succeeds. The DBs contain no records — every IP lookup
// returns no-match, which is fine for tests that don't exercise GeoIP rules.
func buildEmptyMMDBs(t *testing.T, dir string) {
	t.Helper()

	country, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create country mmdb writer: %v", err)
	}
	// Insert a no-op record so the writer produces a valid file.
	_, ipnet, _ := net.ParseCIDR("0.0.0.0/0")
	if err := country.Insert(ipnet, mmdbtype.Map{}); err != nil {
		t.Fatalf("insert country record: %v", err)
	}
	writeMMDB(t, country, filepath.Join(dir, "GeoLite2-Country.mmdb"))

	asn, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-ASN",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("create ASN mmdb writer: %v", err)
	}
	if err := asn.Insert(ipnet, mmdbtype.Map{}); err != nil {
		t.Fatalf("insert ASN record: %v", err)
	}
	writeMMDB(t, asn, filepath.Join(dir, "GeoLite2-ASN.mmdb"))
}

func writeMMDB(t *testing.T, tree *mmdbwriter.Tree, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := tree.WriteTo(f); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
