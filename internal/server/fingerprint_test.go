package server

import (
	"os"
	"path/filepath"
	"testing"
)

// testFP is a helper that computes a fingerprint and fails the test on error.
func testFP(t *testing.T, path string, mode VerifyMode) zoneFingerprint {
	t.Helper()
	fp, err := computeFingerprint(path, mode)
	if err != nil {
		t.Fatalf("computeFingerprint(%q, %v): %v", path, mode, err)
	}
	return fp
}

func TestComputeFingerprint_TableDriven(t *testing.T) {
	dir := t.TempDir()

	// contentA and contentB must have exactly the same byte length to simulate
	// the rsync -avc --inplace scenario where only content changes, not size.
	contentA := []byte("$ORIGIN example.com.\n; version=1 serial=1\nexample.com. 300 IN SOA ns1 admin 1 3600 900 604800 300\n")
	contentB := []byte("$ORIGIN example.com.\n; version=2 serial=2\nexample.com. 300 IN SOA ns1 admin 2 3600 900 604800 300\n")
	contentC := []byte("$ORIGIN example.com.\n; extra-line\n; version=1 serial=1\nexample.com. 300 IN SOA ns1 admin 1 3600 900 604800 300\n")

	if len(contentA) != len(contentB) {
		t.Fatalf("test setup error: contentA (%d bytes) and contentB (%d bytes) must be the same length for the rsync scenario", len(contentA), len(contentB))
	}
	if len(contentA) == len(contentC) {
		t.Fatal("test setup error: contentC must differ in size from contentA")
	}

	// Write the three zone files.
	pathA := filepath.Join(dir, "zone_a.txt")
	pathB := filepath.Join(dir, "zone_b.txt")
	pathC := filepath.Join(dir, "zone_c.txt")
	for _, tc := range []struct {
		path    string
		content []byte
	}{
		{pathA, contentA},
		{pathB, contentB},
		{pathC, contentC},
	} {
		if err := os.WriteFile(tc.path, tc.content, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", tc.path, err)
		}
	}

	// ---------------------------------------------------------------------------
	// Scenario: unchanged file (size + hash match → reuse)
	// ---------------------------------------------------------------------------
	t.Run("unchanged_file_hash_mode", func(t *testing.T) {
		fp1 := testFP(t, pathA, VerifyModeHash)
		fp2 := testFP(t, pathA, VerifyModeHash)
		if fp1.changed(fp2, VerifyModeHash) {
			t.Error("identical file should not be detected as changed in hash mode")
		}
		if fp2.changed(fp1, VerifyModeHash) {
			t.Error("identical file should not be detected as changed in hash mode (reversed)")
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario: rsync -avc --inplace — same size, different content
	// hash mode must detect the change; size mode must miss it (negative control)
	// ---------------------------------------------------------------------------
	t.Run("rsync_inplace_hash_detects_change", func(t *testing.T) {
		rsyncFile := filepath.Join(dir, "rsync_zone.txt")
		if err := os.WriteFile(rsyncFile, contentA, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// Capture mtime before overwrite.
		info, err := os.Stat(rsyncFile)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		origMtime := info.ModTime()

		fpBefore := testFP(t, rsyncFile, VerifyModeHash)
		fpBeforeSize := testFP(t, rsyncFile, VerifyModeSize)

		// Overwrite with same-size different content (simulating rsync --inplace).
		if err := os.WriteFile(rsyncFile, contentB, 0o644); err != nil {
			t.Fatalf("overwrite: %v", err)
		}
		// Restore mtime to simulate rsync preserving it with -t.
		if err := os.Chtimes(rsyncFile, origMtime, origMtime); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}

		fpAfter := testFP(t, rsyncFile, VerifyModeHash)
		fpAfterSize := testFP(t, rsyncFile, VerifyModeSize)

		// hash mode: must detect the content change.
		if !fpAfter.changed(fpBefore, VerifyModeHash) {
			t.Error("hash mode should detect same-size different-content as changed")
		}
		// size mode: must NOT detect the change (negative control — rsync preserves mtime).
		if fpAfterSize.changed(fpBeforeSize, VerifyModeSize) {
			t.Error("size mode should NOT detect change when mtime and size are both preserved (negative control)")
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario: different size — early exit without computing hash
	// ---------------------------------------------------------------------------
	t.Run("different_size_early_exit", func(t *testing.T) {
		fpA := testFP(t, pathA, VerifyModeHash)
		fpC := testFP(t, pathC, VerifyModeHash)
		if !fpC.changed(fpA, VerifyModeHash) {
			t.Error("different-size files should be detected as changed")
		}

		// Verify that changed() returns true purely on size, independent of hash.
		// Construct synthetic fingerprints: different size but same hash field —
		// this is physically impossible but verifies the early-exit code path.
		syntheticPrev := zoneFingerprint{size: 100, mtime: 0, hash: 0xdeadbeef}
		syntheticCur := zoneFingerprint{size: 200, mtime: 0, hash: 0xdeadbeef}
		if !syntheticCur.changed(syntheticPrev, VerifyModeHash) {
			t.Error("fingerprints with different sizes but same hash should still report changed")
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario: VerifyModeNone always reports changed (full rebuild)
	// ---------------------------------------------------------------------------
	t.Run("none_mode_always_changed", func(t *testing.T) {
		fpA := testFP(t, pathA, VerifyModeNone)
		fpA2 := testFP(t, pathA, VerifyModeNone)
		if !fpA.changed(fpA2, VerifyModeNone) {
			t.Error("none mode should always report changed regardless of fingerprint values")
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario: VerifyModeSize unchanged — size and mtime both match
	// ---------------------------------------------------------------------------
	t.Run("size_mode_unchanged", func(t *testing.T) {
		fp1 := testFP(t, pathA, VerifyModeSize)
		fp2 := testFP(t, pathA, VerifyModeSize)
		if fp1.changed(fp2, VerifyModeSize) {
			t.Error("identical file should not be detected as changed in size mode")
		}
	})
}
