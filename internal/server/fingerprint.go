package server

import (
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
)

// VerifyMode controls how zone file changes are detected during SIGHUP reload.
// The value is set once at startup via the -reload-verify CLI flag and remains
// fixed for the process lifetime.
type VerifyMode int

const (
	// VerifyModeHash uses file size as a pre-filter and xxhash64 content hash
	// as the authoritative comparison. This is the safe default: it correctly
	// detects rsync -avc --inplace rewrites where mtime is preserved but
	// contents change.
	VerifyModeHash VerifyMode = iota

	// VerifyModeSize compares only (mtime, size) without reading file contents.
	// Faster than hash mode but cannot detect same-size content changes when
	// the source preserves mtime (e.g. rsync with -t). Use only when the
	// release pipeline guarantees mtime will differ on every rewrite.
	VerifyModeSize

	// VerifyModeNone disables fingerprinting entirely. Every reload re-parses
	// all zone files regardless of any change detection. This is the escape
	// hatch to restore pre-optimization behavior without a binary redeploy.
	VerifyModeNone
)

// String returns the canonical name of the mode for use in log fields and
// CLI error messages.
func (m VerifyMode) String() string {
	switch m {
	case VerifyModeHash:
		return "hash"
	case VerifyModeSize:
		return "size"
	case VerifyModeNone:
		return "none"
	default:
		return "unknown"
	}
}

// zoneFingerprint is a point-in-time snapshot of a zone file's identity.
// Which fields participate in the comparison depends on the VerifyMode used:
// changed() ignores mtime under VerifyModeHash and ignores hash under
// VerifyModeSize.
type zoneFingerprint struct {
	size  int64  // file size in bytes (populated for all non-None modes)
	mtime int64  // modification time in nanoseconds (populated for all non-None modes; compared only in VerifyModeSize)
	hash  uint64 // xxhash64 of full file contents (populated and compared only in VerifyModeHash)
}

// computeFingerprint computes and returns the fingerprint for the file at path
// under the given mode.
//
// For VerifyModeNone a zero-value fingerprint is returned without any I/O.
// For VerifyModeSize only os.Stat is called; the file is not opened.
// For VerifyModeHash os.Stat is called and the full file is read to compute
// the xxhash64.
//
// Note: a small TOCTOU window exists between os.Stat and os.Open under
// VerifyModeHash — an atomic rename between the two calls will pair the new
// file's size with a hash of whichever file os.Open resolved. In the worst
// case this yields a fingerprint that does not correspond to any single file
// version; the next reload will compute a fresh fingerprint that does not
// match, triggering the re-parse. A stale reuse is not possible because the
// comparison happens against whatever is on disk at the next reload.
func computeFingerprint(path string, mode VerifyMode) (zoneFingerprint, error) {
	if mode == VerifyModeNone {
		return zoneFingerprint{}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return zoneFingerprint{}, err
	}

	fp := zoneFingerprint{
		size:  info.Size(),
		mtime: info.ModTime().UnixNano(),
	}

	if mode == VerifyModeHash {
		f, err := os.Open(path)
		if err != nil {
			return zoneFingerprint{}, err
		}
		defer func() { _ = f.Close() }()

		h := xxhash.New()
		if _, err := io.Copy(h, f); err != nil {
			return zoneFingerprint{}, err
		}
		fp.hash = h.Sum64()
	}

	return fp, nil
}

// changed reports whether fp represents a changed file compared to prev under
// the given mode. Returns true when the zone file should be re-parsed.
//
// VerifyModeNone: always returns true (full rebuild every reload).
// VerifyModeSize: returns true if either mtime or size differs.
// VerifyModeHash: returns true if size differs (early exit) or hash differs.
func (fp zoneFingerprint) changed(prev zoneFingerprint, mode VerifyMode) bool {
	switch mode {
	case VerifyModeNone:
		return true
	case VerifyModeSize:
		return fp.size != prev.size || fp.mtime != prev.mtime
	default: // VerifyModeHash
		if fp.size != prev.size {
			return true // early exit: different size means definitely changed
		}
		return fp.hash != prev.hash
	}
}
