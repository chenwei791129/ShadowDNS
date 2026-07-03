package main

// Regression test for the rapid-double-SIGHUP GeoIP use-after-munmap crash
// (GitHub issue #13). A DNS query holds a state snapshot — hence a country/ASN
// mmdb generation — for its whole duration. Two reloads firing in immediate
// succession must never unmap a generation an in-flight query is still reading.
//
// This test pins the startup generation via a retained reference and hammers it
// with concurrent lookups while two back-to-back reloads run. Under the pre-fix
// deferred-by-one-generation close, the second reload munmaps the startup
// generation while the lookup goroutines are still decoding it — a fatal
// use-after-munmap SIGSEGV (and a data race the -race detector flags). After the
// fix the reload path closes nothing, so the retained generation stays mapped
// and the test passes cleanly under `go test -race`.
//
// All fixture domains use RFC 2606 names; all paths live under t.TempDir().

import (
	"context"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestReloadGeoIP_RapidDoubleSIGHUP_NoUseAfterMunmap(t *testing.T) {
	dir := setupReloadTestDir(t)
	geoDir := filepath.Join(dir, "geoip")
	// Data-bearing mmdbs so a lookup against the retained generation actually
	// decodes records off the mapping (the operation that faults post-munmap).
	buildDataMMDBs(t, geoDir, "JP", 64500)

	srv, geo, qlState, opts := startReloadTestServer(t, dir)
	defer geo.closeAll(zap.NewNop())

	// Retain the startup generation. Holding these references is what an
	// in-flight query does implicitly via its state snapshot; here it keeps the
	// generation reachable so nothing other than the reload path could reclaim
	// it — isolating the reload path as the only possible closer.
	genCountry := geo.country
	genASN := geo.asn
	testIP := netip.MustParseAddr("203.0.113.50")
	if iso, ok := genCountry.Lookup(testIP); !ok || iso != "JP" {
		t.Fatalf("fixture country lookup = (%q, %v), want (JP, true)", iso, ok)
	}

	// Keep a decode in flight off the retained generation across both reloads,
	// so a reload that unmapped it would fault mid-decode (surfacing as a race
	// under -race or a SIGSEGV). A throttled handful of readers is enough to
	// span the reload window; a saturating busy-spin would swamp the -race
	// runtime's shadow state and slow unrelated tests in the same binary, so
	// each reader yields between lookups.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	const readers = 2
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					genCountry.Lookup(testIP)
					genASN.Lookup(testIP)
					time.Sleep(100 * time.Microsecond)
				}
			}
		}()
	}

	// Two reloads in immediate succession: N+1 then N+2. Under the pre-fix
	// code the second reload's step-0 closePrev munmaps the startup generation
	// (parked by the first reload) while the readers above are mid-decode.
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("first reload: %v", err)
	}
	if err := reload(context.Background(), opts, srv, geo, qlState, zap.NewNop()); err != nil {
		t.Fatalf("second reload: %v", err)
	}

	close(stop)
	wg.Wait()

	// The reload path must have closed nothing: the retained generation still
	// answers lookups off its still-mapped mmdb.
	if iso, ok := genCountry.Lookup(testIP); !ok || iso != "JP" {
		t.Errorf("retained country generation lookup after two reloads = (%q, %v), want (JP, true); the reload path must not close it", iso, ok)
	}
	if asn, ok := genASN.Lookup(testIP); !ok || asn != 64500 {
		t.Errorf("retained ASN generation lookup after two reloads = (%d, %v), want (64500, true); the reload path must not close it", asn, ok)
	}
}
