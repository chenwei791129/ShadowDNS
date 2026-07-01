package config

import (
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// TestIncludeCycle_SelfInclude verifies that a named.conf which includes itself
// is rejected with a descriptive cycle error instead of recursing without bound
// until the Go runtime aborts the process with a stack overflow.
func TestIncludeCycle_SelfInclude(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "named.conf")
	// A file that includes itself: following the include without cycle detection
	// recurses forever.
	writeFile(t, confPath, `include "named.conf";`+"\n")

	_, err := LoadNamedConf(confPath, zap.NewNop())
	if err == nil {
		t.Fatalf("expected a cycle error loading a self-including file, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention a cycle, got: %v", err)
	}
	if !strings.Contains(err.Error(), confPath) {
		t.Errorf("error should name the offending path %q, got: %v", confPath, err)
	}
}

// TestIncludeCycle_MutualInclude verifies that two files which include each
// other (A includes B, B includes A) are rejected with a cycle error rather
// than crashing the process.
func TestIncludeCycle_MutualInclude(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.conf")
	bPath := filepath.Join(dir, "b.conf")
	writeFile(t, aPath, `include "b.conf";`+"\n")
	writeFile(t, bPath, `include "a.conf";`+"\n")

	_, err := LoadNamedConf(aPath, zap.NewNop())
	if err == nil {
		t.Fatalf("expected a cycle error loading mutually-including files, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention a cycle, got: %v", err)
	}
	// The error must name one of the files on the cycle so an operator can locate
	// the offending include, matching the rigor of the self-include case.
	if !strings.Contains(err.Error(), aPath) && !strings.Contains(err.Error(), bPath) {
		t.Errorf("error should name an offending path (%q or %q), got: %v", aPath, bPath, err)
	}
}

// TestIncludeCycle_AcyclicDiamondLoads verifies that the SAME file legitimately
// included from two separate branches of an acyclic include tree is NOT falsely
// flagged as a cycle. The tree is a diamond: root includes branch1 and branch2,
// and both branches include the same shared leaf. The leaf appears twice in the
// include tree but never on a single active include chain, so it is valid.
func TestIncludeCycle_AcyclicDiamondLoads(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "named.conf")
	branch1Path := filepath.Join(dir, "branch1.conf")
	branch2Path := filepath.Join(dir, "branch2.conf")
	leafPath := filepath.Join(dir, "leaf.conf")

	writeFile(t, rootPath, `include "branch1.conf";`+"\n"+`include "branch2.conf";`+"\n")
	writeFile(t, branch1Path, `include "leaf.conf";`+"\n")
	writeFile(t, branch2Path, `include "leaf.conf";`+"\n")
	// The shared leaf declares a valid view so the acyclic tree loads cleanly.
	writeFile(t, leafPath, `view "internal" { match-clients { any; }; };`+"\n")

	cfg, err := LoadNamedConf(rootPath, zap.NewNop())
	if err != nil {
		t.Fatalf("acyclic diamond include tree should load without error, got: %v", err)
	}
	// The leaf is included from both branches; its single view declaration is
	// processed exactly once per inclusion (twice total), confirming the same
	// file may legitimately appear on two distinct branches.
	if len(cfg.Views) != 2 {
		t.Fatalf("expected 2 views (leaf view included from both branches), got %d", len(cfg.Views))
	}
	// Both views must be the leaf's "internal" view, proving each inclusion
	// processed the shared leaf rather than the count being inflated some other way.
	for i, v := range cfg.Views {
		if v.Name != "internal" {
			t.Errorf("view %d: expected leaf view %q, got %q", i, "internal", v.Name)
		}
	}
}
