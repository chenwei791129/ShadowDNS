package prunebackup

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestApplyFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := applyFile(path, []byte("pruned\n"), zap.NewNop()); err != nil {
		t.Fatalf("applyFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pruned: %v", err)
	}
	if string(got) != "pruned\n" {
		t.Errorf("post-apply content = %q, want %q", got, "pruned\n")
	}

	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bak) != "original\n" {
		t.Errorf(".bak content = %q, want %q", bak, "original\n")
	}
}

func TestApplyFile_OverwritesExistingBakWithInfoLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	bak := path + ".bak"

	if err := os.WriteFile(path, []byte("second-run-original\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(bak, []byte("first-run-bak\n"), 0o644); err != nil {
		t.Fatalf("seed bak: %v", err)
	}

	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	if err := applyFile(path, []byte("pruned2\n"), logger); err != nil {
		t.Fatalf("applyFile: %v", err)
	}

	got, _ := os.ReadFile(bak)
	if string(got) != "second-run-original\n" {
		t.Errorf(".bak content = %q; want %q (previous .bak overwritten)", got, "second-run-original\n")
	}

	entries := logs.FilterMessageSnippet("overwriting").All()
	if len(entries) != 1 {
		t.Fatalf("want 1 INFO log about overwriting, got %d: %v", len(entries), logs.All())
	}
	foundPath := false
	for _, f := range entries[0].Context {
		if f.Key == "path" && f.String == bak {
			foundPath = true
		}
	}
	if !foundPath {
		t.Errorf("log fields missing path=%q: %v", bak, entries[0].Context)
	}
}

func TestApplyFile_PreservesOriginalMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	const origMode os.FileMode = 0o640
	if err := os.WriteFile(path, []byte("original\n"), origMode); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := applyFile(path, []byte("pruned\n"), zap.NewNop()); err != nil {
		t.Fatalf("applyFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat post-apply: %v", err)
	}
	if got := info.Mode().Perm(); got != origMode {
		t.Errorf("post-apply mode = %#o, want %#o (would break daemon read access)", got, origMode)
	}
}

func TestApplyAll_SkipsFilesNotInChangeMap(t *testing.T) {
	dir := t.TempDir()
	touched := filepath.Join(dir, "a.fwd")
	untouched := filepath.Join(dir, "b.fwd")
	if err := os.WriteFile(touched, []byte("A orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untouched, []byte("B orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes := map[string][]byte{touched: []byte("A pruned\n")}
	if err := ApplyAll(changes, zap.NewNop()); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}

	// untouched must be byte-identical and have no .bak.
	got, _ := os.ReadFile(untouched)
	if string(got) != "B orig\n" {
		t.Errorf("untouched mutated: %q", got)
	}
	if _, err := os.Stat(untouched + ".bak"); !os.IsNotExist(err) {
		t.Errorf("untouched .bak created unexpectedly: err=%v", err)
	}

	// touched applied.
	got, _ = os.ReadFile(touched)
	if string(got) != "A pruned\n" {
		t.Errorf("touched content = %q, want %q", got, "A pruned\n")
	}
}

func TestApplyAll_FailStopLeavesEarlierWritesInPlace(t *testing.T) {
	dir := t.TempDir()
	firstOK := filepath.Join(dir, "a.fwd")
	// Path whose parent directory does not exist → second applyFile fails.
	secondFail := filepath.Join(dir, "nope", "b.fwd")

	if err := os.WriteFile(firstOK, []byte("A orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes := map[string][]byte{
		firstOK:    []byte("A pruned\n"),
		secondFail: []byte("B pruned\n"),
	}
	err := ApplyAll(changes, zap.NewNop())
	if err == nil {
		t.Fatalf("want error, got nil")
	}

	got, _ := os.ReadFile(firstOK)
	if string(got) != "A pruned\n" {
		t.Errorf("first file not applied: %q", got)
	}
	bak, _ := os.ReadFile(firstOK + ".bak")
	if string(bak) != "A orig\n" {
		t.Errorf("first file .bak missing or wrong: %q", bak)
	}
}
