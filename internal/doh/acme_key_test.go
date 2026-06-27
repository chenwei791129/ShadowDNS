package doh

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadOrCreateAccountKey_MissingGeneratesWith0600 covers case (a): a path
// that does not yet exist yields a freshly generated key persisted as an
// *ecdsa.PrivateKey with file mode 0600. The reuse-on-reload property is
// covered by TestLoadOrCreateAccountKey_SecondCallReturnsSameKey.
func TestLoadOrCreateAccountKey_MissingGeneratesWith0600(t *testing.T) {
	dir := t.TempDir()
	// Nested path so MkdirAll of the missing parent is exercised too.
	path := filepath.Join(dir, "acme", "account.key")

	key, err := loadOrCreateAccountKey(path)
	if err != nil {
		t.Fatalf("loadOrCreateAccountKey: %v", err)
	}
	if _, ok := key.(*ecdsa.PrivateKey); !ok {
		t.Fatalf("key type = %T, want *ecdsa.PrivateKey", key)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat persisted key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("persisted key file mode = %o, want 600", perm)
	}
}

// TestLoadOrCreateAccountKey_SecondCallReturnsSameKey covers case (b): a second
// call against the same path reloads and returns the identical key, compared
// with (*ecdsa.PrivateKey).Equal rather than == or reflect.DeepEqual.
func TestLoadOrCreateAccountKey_SecondCallReturnsSameKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.key")

	k1, err := loadOrCreateAccountKey(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	k2, err := loadOrCreateAccountKey(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	ec1, ok := k1.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("first key type = %T, want *ecdsa.PrivateKey", k1)
	}
	ec2, ok := k2.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("second key type = %T, want *ecdsa.PrivateKey", k2)
	}
	if !ec1.Equal(ec2) {
		t.Error("second call returned a different key; key was not reused")
	}
}

// TestLoadOrCreateAccountKey_CorruptFailsLoud covers case (c): an existing file
// that does not contain a parseable key yields a non-nil error naming the path,
// leaves the file unchanged, and leaves no temp file residue in the directory.
func TestLoadOrCreateAccountKey_CorruptFailsLoud(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "account.key")
	corrupt := []byte("this is not a PEM private key\n")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	_, err := loadOrCreateAccountKey(path)
	if err == nil {
		t.Fatal("loadOrCreateAccountKey succeeded on corrupt file, want error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the key path %q", err, path)
	}

	// The corrupt file must not be overwritten with a freshly minted key.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back corrupt file: %v", err)
	}
	if string(got) != string(corrupt) {
		t.Errorf("corrupt file was modified: got %q, want %q", got, corrupt)
	}

	// No sibling temp file should be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want 1 (no temp residue): %v", len(entries), entries)
	}
}

// TestLoadOrCreateAccountKey_DirectoryFailsLoud covers case (d): a path that
// points at an existing directory is classified as an error, not mistaken for a
// missing file (which would silently mint a new key).
func TestLoadOrCreateAccountKey_DirectoryFailsLoud(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keydir")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("make directory: %v", err)
	}

	_, err := loadOrCreateAccountKey(path)
	if err == nil {
		t.Fatal("loadOrCreateAccountKey succeeded on a directory path, want error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the key path %q", err, path)
	}
}

// TestLoadOrCreateAccountKey_TightensLoosePermsOnLoad asserts that loading a
// pre-existing key whose permissions are looser than 0600 re-tightens it to
// 0600 (defense in depth against an exposed secret key).
func TestLoadOrCreateAccountKey_TightensLoosePermsOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "account.key")

	// Seed a valid key, then loosen its permissions as a restore/umask might.
	if _, err := loadOrCreateAccountKey(path); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("loosen perms: %v", err)
	}

	if _, err := loadOrCreateAccountKey(path); err != nil {
		t.Fatalf("reload: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode after load = %o, want 600 (tightened)", perm)
	}
}

// TestLoadOrCreateAccountKey_EmptyPathFailsLoud asserts that an empty path is
// rejected rather than treated as "missing" (which would mint a key in the CWD).
func TestLoadOrCreateAccountKey_EmptyPathFailsLoud(t *testing.T) {
	if _, err := loadOrCreateAccountKey(""); err == nil {
		t.Fatal("loadOrCreateAccountKey(\"\") succeeded, want error")
	}
}

// TestLoadOrCreateAccountKey_NonECDSAKeyRejected asserts that a valid PKCS#8 PEM
// holding a non-ECDSA key (RSA) is rejected, since the account key is always an
// ECDSA key.
func TestLoadOrCreateAccountKey_NonECDSAKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "account.key")

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatalf("marshal rsa key: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, pemData, 0o600); err != nil {
		t.Fatalf("write rsa key: %v", err)
	}

	if _, err := loadOrCreateAccountKey(path); err == nil {
		t.Fatal("loadOrCreateAccountKey accepted a non-ECDSA key, want error")
	}
}
