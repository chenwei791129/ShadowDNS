package prunebackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "zone.fwd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLexer_ClassifiesKinds(t *testing.T) {
	content := `$TTL 300
$ORIGIN backup.example.
; stand-alone comment

@ IN SOA ns1.backup.example. hostmaster.backup.example. (
    1
    300
    120
    604800
    300
)

@   IN TXT "marker"
`

	path := writeTempFile(t, content)
	_, _, lexemes, err := lexFile(path, "backup.example.", 0)
	if err != nil {
		t.Fatalf("lexFile: %v", err)
	}

	kinds := []lexemeKind{}
	for _, lx := range lexemes {
		kinds = append(kinds, lx.Kind)
	}

	want := []lexemeKind{
		kindDirective,
		kindDirective,
		kindBlankOrComment,
		kindBlankOrComment,
		kindRR,
		kindBlankOrComment,
		kindRR,
	}
	if len(kinds) != len(want) {
		t.Fatalf("got %d lexemes, want %d: %#v", len(kinds), len(want), kinds)
	}
	for i, k := range kinds {
		if k != want[i] {
			t.Errorf("lexeme[%d] kind=%d, want %d", i, k, want[i])
		}
	}

	soa := lexemes[4]
	if soa.StartLine != 5 || soa.EndLine != 11 {
		t.Errorf("SOA line range = %d..%d, want 5..11", soa.StartLine, soa.EndLine)
	}
	if _, ok := soa.RR.(*dns.SOA); !ok {
		t.Errorf("SOA lexeme RR type = %T, want *dns.SOA", soa.RR)
	}

	txt := lexemes[6]
	if txt.StartLine != 13 || txt.EndLine != 13 {
		t.Errorf("TXT line range = %d..%d, want 13..13", txt.StartLine, txt.EndLine)
	}
	if _, ok := txt.RR.(*dns.TXT); !ok {
		t.Errorf("TXT lexeme RR type = %T, want *dns.TXT", txt.RR)
	}
}

func TestLexer_TracksActiveOriginAndTTL(t *testing.T) {
	// The active $ORIGIN/$TTL aren't stored on each lexeme, but they must be
	// applied at parse time so a relative owner like `host` gets expanded to
	// the current origin and an implicit TTL picks up the active $TTL value.
	content := `$TTL 600
@ IN TXT "at-default"
$TTL 120
$ORIGIN sub.backup.example.
host IN TXT "at-overridden"
`
	path := writeTempFile(t, content)
	_, _, lexemes, err := lexFile(path, "backup.example.", 0)
	if err != nil {
		t.Fatalf("lexFile: %v", err)
	}

	var firstRR, secondRR *lexeme
	for i := range lexemes {
		if lexemes[i].Kind != kindRR {
			continue
		}
		if firstRR == nil {
			firstRR = &lexemes[i]
		} else if secondRR == nil {
			secondRR = &lexemes[i]
		}
	}
	if firstRR == nil || secondRR == nil {
		t.Fatalf("expected two RR lexemes")
	}

	if firstRR.RR.Header().Name != "backup.example." {
		t.Errorf("firstRR owner = %q, want backup.example.", firstRR.RR.Header().Name)
	}
	if firstRR.RR.Header().Ttl != 600 {
		t.Errorf("firstRR TTL = %d, want 600", firstRR.RR.Header().Ttl)
	}
	if secondRR.RR.Header().Name != "host.sub.backup.example." {
		t.Errorf("secondRR owner = %q, want host.sub.backup.example.", secondRR.RR.Header().Name)
	}
	if secondRR.RR.Header().Ttl != 120 {
		t.Errorf("secondRR TTL = %d, want 120", secondRR.RR.Header().Ttl)
	}
}

func TestLexer_IncludeDirectiveQuotedAndBare(t *testing.T) {
	content := `$TTL 300
$include "sub/frag.zone"
$INCLUDE other.zone
`
	path := writeTempFile(t, content)
	_, _, lexemes, err := lexFile(path, "backup.example.", 0)
	if err != nil {
		t.Fatalf("lexFile: %v", err)
	}

	var includes []lexeme
	for _, lx := range lexemes {
		if lx.Kind == kindDirective && strings.EqualFold(lx.DirectiveName, directiveInclude) {
			includes = append(includes, lx)
		}
	}
	if len(includes) != 2 {
		t.Fatalf("got %d $INCLUDE directives, want 2", len(includes))
	}
	if includes[0].DirectiveArg != "sub/frag.zone" {
		t.Errorf("first $INCLUDE arg = %q, want sub/frag.zone", includes[0].DirectiveArg)
	}
	if includes[1].DirectiveArg != "other.zone" {
		t.Errorf("second $INCLUDE arg = %q, want other.zone", includes[1].DirectiveArg)
	}
}

func TestLexer_ParenInsideQuotedStringDoesNotExtendRange(t *testing.T) {
	content := `$TTL 300
@ IN TXT "has ( paren but single line"
`
	path := writeTempFile(t, content)
	_, _, lexemes, err := lexFile(path, "backup.example.", 0)
	if err != nil {
		t.Fatalf("lexFile: %v", err)
	}

	var rr *lexeme
	for i := range lexemes {
		if lexemes[i].Kind == kindRR {
			rr = &lexemes[i]
			break
		}
	}
	if rr == nil {
		t.Fatalf("no RR lexeme found")
	}
	if rr.StartLine != 2 || rr.EndLine != 2 {
		t.Errorf("TXT with quoted paren line range = %d..%d, want 2..2", rr.StartLine, rr.EndLine)
	}
}

func TestLexer_ParseFailureReportsFileLine(t *testing.T) {
	content := `$TTL 300
@ IN TOTALLY_BOGUS nonsense
`
	path := writeTempFile(t, content)
	_, _, _, err := lexFile(path, "backup.example.", 0)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q missing file path", err.Error())
	}
	if !strings.Contains(err.Error(), ":2") {
		t.Errorf("error %q missing line number :2", err.Error())
	}
}

func TestLexer_FixtureBackupViewTH(t *testing.T) {
	path := "../../testdata/integration/master/backup.example_view-th.fwd"
	_, _, lexemes, err := lexFile(path, "backup.example.", 0)
	if err != nil {
		t.Fatalf("lexFile: %v", err)
	}

	var soa *lexeme
	var rrCount int
	for i := range lexemes {
		if lexemes[i].Kind != kindRR {
			continue
		}
		rrCount++
		if _, ok := lexemes[i].RR.(*dns.SOA); ok {
			soa = &lexemes[i]
		}
	}
	if soa == nil {
		t.Fatalf("expected an SOA lexeme in fixture, got none")
	}
	if soa.StartLine != 2 || soa.EndLine != 8 {
		t.Errorf("SOA line range = %d..%d, want 2..8", soa.StartLine, soa.EndLine)
	}
	if rrCount != 3 {
		t.Errorf("RR count = %d, want 3", rrCount)
	}
}
