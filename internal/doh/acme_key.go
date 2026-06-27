package doh

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// accountKeyFileMode and accountKeyDirMode are the fixed permissions for a
// persisted ACME account key and its parent directory. They are constants, not
// parameters, so the secure values cannot be downgraded or accidentally swapped
// by a caller: the account key is a long-lived secret that must never be
// group/world readable.
const (
	accountKeyFileMode fs.FileMode = 0o600
	accountKeyDirMode  fs.FileMode = 0o700
)

// loadOrCreateAccountKey returns the ACME account private key stored at path,
// generating and persisting a new one when the file does not exist.
//
// Precondition: path is a non-empty absolute path guaranteed by the caller (in
// production by shadowdnscfg.buildDoHACME; in tests by passing a t.TempDir()
// path). The branches are:
//
//   - path does not exist (errors.Is(err, fs.ErrNotExist), never a value
//     comparison, so a wrapped error is still recognized) → generate a new P256
//     key, create any missing parent directory (0700), and persist the key as
//     PKCS#8 PEM with mode 0600 via an atomic write, then return it.
//   - path exists but cannot be read (a directory yields EISDIR, permission
//     denied, ...) or cannot be parsed (corrupt contents) → return a non-nil
//     error naming path. The existing file is never overwritten and no
//     replacement key is minted (fail loudly so a transient problem cannot be
//     mistaken for "missing" and silently churn ACME accounts).
//   - path exists and parses → return that key.
//
// A single os.ReadFile distinguishes all three cases (missing via fs.ErrNotExist,
// directory/permission via a non-ErrNotExist read error, corrupt via a parse
// error) without a separate stat, avoiding a TOCTOU window.
//
// newLazyLegoObtainer only caches the obtainer on success, so this runs on
// every obtain retry; a corrupt key therefore re-surfaces the same explicit
// error on each retry rather than being swallowed once at startup.
func loadOrCreateAccountKey(path string) (crypto.PrivateKey, error) {
	// Guard the empty path explicitly: os.ReadFile("") returns an
	// fs.ErrNotExist, which would otherwise take the generate branch and
	// silently write a key into the process CWD (filepath.Dir("") == ".")
	// instead of failing. Production validates this in buildDoHACME, but a
	// future caller that bypasses validation must not mint a misplaced key.
	if path == "" {
		return nil, errors.New("doh acme: account key path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return generateAndPersistAccountKey(path)
		}
		return nil, fmt.Errorf("doh acme: read account key %q: %w", path, err)
	}
	key, err := parseAccountKey(data)
	if err != nil {
		return nil, fmt.Errorf("doh acme: account key %q is not a valid PKCS#8 PEM key file: %w", path, err)
	}
	// Defense in depth: a key placed by an operator, a backup restore, or a
	// permissive umask may be group/world readable. Tighten it to 0600 so the
	// long-lived secret is not left exposed, preserving any already-stricter
	// mode (e.g. 0400). The code only ever creates the file at 0600, so this
	// matters solely for pre-existing files it did not write.
	if err := tightenKeyFilePerm(path); err != nil {
		return nil, err
	}
	return key, nil
}

// tightenKeyFilePerm chmods path to 0600 when its current permissions grant any
// group or other access. A mode already at or stricter than 0600 (e.g. 0400) is
// left untouched.
func tightenKeyFilePerm(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("doh acme: stat account key %q: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, accountKeyFileMode); err != nil {
			return fmt.Errorf("doh acme: tighten account key %q permissions: %w", path, err)
		}
	}
	return nil
}

// parseAccountKey decodes a PKCS#8 PEM-encoded ECDSA private key. A valid PKCS#8
// key of another type (RSA, Ed25519) is rejected: the account key is always
// generated as P256 ECDSA, so a non-ECDSA key at the path is a misconfiguration
// that must fail loudly rather than be passed to lego and surface as an opaque
// registration error later.
func parseAccountKey(pemData []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected an ECDSA private key, got %T", key)
	}
	return ecKey, nil
}

// generateAndPersistAccountKey mints a new P256 ECDSA account key, encodes it as
// PKCS#8 PEM, and writes it atomically to path before returning it.
func generateAndPersistAccountKey(path string) (crypto.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("doh acme: generate account key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("doh acme: marshal account key: %w", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := atomicWriteAccountKey(path, pemData); err != nil {
		return nil, fmt.Errorf("doh acme: persist account key %q: %w", path, err)
	}
	return key, nil
}

// atomicWriteAccountKey writes data to path atomically: it creates the parent
// directory (0700) if missing, writes a sibling temp file, fsyncs it (so a
// crash/power-loss cannot leave a zero-length key), chmods it to 0600, and
// renames it onto path. Every error branch removes the temp file. This mirrors
// the durability dance in internal/prunebackup.applyFile (fsync before rename,
// temp cleanup on every failure) without that function's .bak / must-exist
// semantics, which do not apply to first-time key creation. Permissions are
// fixed constants rather than parameters so they cannot be downgraded.
func atomicWriteAccountKey(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, accountKeyDirMode); err != nil {
		return fmt.Errorf("create parent dir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write tmp %q: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("fsync tmp %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close tmp %q: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, accountKeyFileMode); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %q -> %q: %w", tmpName, path, err)
	}
	return nil
}
