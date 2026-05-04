package prunebackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return full
}

func TestLoadZoneTree_NestedIncludesMergeAndAnnotate(t *testing.T) {
	dir := t.TempDir()

	mainPath := writeFile(t, dir, "main.fwd", `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
$include "frag1.fwd"
@ IN TXT "main-level"
`)

	writeFile(t, dir, "frag1.fwd", `host1 IN TXT "from-frag1"
$include "frag2.fwd"
`)

	writeFile(t, dir, "frag2.fwd", `host2 IN TXT "from-frag2"
`)

	files, srrs, err := loadZoneTree(mainPath, "backup.example.", dir, 0)
	if err != nil {
		t.Fatalf("loadZoneTree: %v", err)
	}

	if got, want := len(files), 3; got != want {
		t.Errorf("loaded files = %d, want %d", got, want)
	}

	// Expect merged RR list: SOA, TXT host1, TXT host2, TXT main-level — in file order.
	var txtOrder []string
	for _, s := range srrs {
		if txt, ok := s.RR.(*dns.TXT); ok {
			txtOrder = append(txtOrder, txt.Txt[0])
		}
	}
	want := []string{"from-frag1", "from-frag2", "main-level"}
	if len(txtOrder) != len(want) {
		t.Fatalf("txt order = %v, want %v", txtOrder, want)
	}
	for i, w := range want {
		if txtOrder[i] != w {
			t.Errorf("txt[%d] = %q, want %q", i, txtOrder[i], w)
		}
	}

	// Annotations: frag2's host2 TXT must point at frag2.fwd.
	var host2 *sourcedRR
	for i := range srrs {
		if txt, ok := srrs[i].RR.(*dns.TXT); ok && txt.Txt[0] == "from-frag2" {
			host2 = &srrs[i]
			break
		}
	}
	if host2 == nil {
		t.Fatalf("host2 not found in merged list")
	}
	if filepath.Base(host2.File) != "frag2.fwd" {
		t.Errorf("host2 source file = %q, want frag2.fwd", host2.File)
	}
	if host2.StartLine != 1 {
		t.Errorf("host2 start line = %d, want 1", host2.StartLine)
	}
}

func TestLoadZoneTree_RelativePathUsesBaseDir(t *testing.T) {
	base := t.TempDir()
	// Main file lives in zones/, fragment lives in zones/sub/. Include uses
	// path "sub/frag.fwd" which must resolve against base, not against main's
	// directory — matching named.conf `directory` semantics.
	zonesDir := filepath.Join(base, "zones")
	mainPath := writeFile(t, zonesDir, "main.fwd", `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
$include "sub/frag.fwd"
`)
	writeFile(t, zonesDir, "sub/frag.fwd", `host IN TXT "from-sub"
`)

	files, srrs, err := loadZoneTree(mainPath, "backup.example.", zonesDir, 0)
	if err != nil {
		t.Fatalf("loadZoneTree: %v", err)
	}
	if got, want := len(files), 2; got != want {
		t.Errorf("loaded files = %d, want %d", got, want)
	}
	var found bool
	for _, s := range srrs {
		if txt, ok := s.RR.(*dns.TXT); ok && txt.Txt[0] == "from-sub" {
			found = true
			if !strings.HasSuffix(s.File, filepath.Join("sub", "frag.fwd")) {
				t.Errorf("TXT source file = %q, want .../sub/frag.fwd", s.File)
			}
		}
	}
	if !found {
		t.Fatalf("TXT from sub fragment not found")
	}
}

func TestLoadZoneTree_QuotedAndBareIncludeBothWork(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.fwd", `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
$include "quoted.fwd"
$include bare.fwd
`)
	writeFile(t, dir, "quoted.fwd", `q IN TXT "quoted-token"
`)
	writeFile(t, dir, "bare.fwd", `b IN TXT "bare-token"
`)

	_, srrs, err := loadZoneTree(mainPath, "backup.example.", dir, 0)
	if err != nil {
		t.Fatalf("loadZoneTree: %v", err)
	}
	found := map[string]bool{}
	for _, s := range srrs {
		if txt, ok := s.RR.(*dns.TXT); ok {
			found[txt.Txt[0]] = true
		}
	}
	if !found["quoted-token"] {
		t.Errorf("quoted include not merged")
	}
	if !found["bare-token"] {
		t.Errorf("bare include not merged")
	}
}

func TestLoadZoneTree_IncludeCycleIsRejected(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "a.fwd", `$TTL 300
$ORIGIN backup.example.
@ IN SOA ns1.backup.example. hostmaster.backup.example. 1 300 120 604800 300
$include "b.fwd"
`)
	writeFile(t, dir, "b.fwd", `$include "a.fwd"
`)

	_, _, err := loadZoneTree(mainPath, "backup.example.", dir, 0)
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q should mention cycle", err.Error())
	}
}
