package prunebackup

import (
	"fmt"
	"path/filepath"

	"github.com/miekg/dns"
)

// loadedFile pairs a physical file path with its raw bytes, pre-split
// lines, and ordered lexemes. Keeping Raw and Lines here lets the rewrite
// stage skip a second ReadFile AND a second splitLines pass.
type loadedFile struct {
	Path    string
	Raw     []byte
	Lines   []string
	Lexemes []lexeme
}

// sourcedRR is one RR annotated with its originating file and 1-based line
// range, so the line-based writer can map "delete this RR" to "delete lines
// X-Y in file F".
type sourcedRR struct {
	RR        dns.RR
	File      string
	StartLine int
	EndLine   int
}

// loadZoneTree loads mainPath plus every file transitively pulled in by its
// `$INCLUDE` directives and returns both the per-file data and a flat,
// merged list of sourcedRR in traversal order.
//
// Relative `$INCLUDE` paths are resolved against the directory of the file
// containing the directive, matching miekg/dns's zone parser and therefore
// the server's runtime loader. baseDir is used as the fallback when
// mainPath itself is relative. Include cycles abort with an error naming
// the file chain.
func loadZoneTree(mainPath, origin, baseDir string, initialTTL uint32) ([]loadedFile, []sourcedRR, error) {
	var files []loadedFile
	var merged []sourcedRR
	visited := map[string]bool{}

	var visit func(path string, stack []string) error
	visit = func(path string, stack []string) error {
		if !filepath.IsAbs(path) && baseDir != "" {
			path = filepath.Join(baseDir, path)
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("prunebackup: resolve %q: %w", path, err)
		}
		for _, s := range stack {
			if s == absPath {
				return fmt.Errorf("prunebackup: $INCLUDE cycle detected: %v -> %s", stack, absPath)
			}
		}
		if visited[absPath] {
			// Re-including the same file is legal in BIND but would yield
			// duplicate RRs. Skip the repeat so the merged list stays set-like.
			return nil
		}
		visited[absPath] = true

		raw, lines, lexemes, err := lexFile(absPath, origin, initialTTL)
		if err != nil {
			return err
		}
		files = append(files, loadedFile{Path: absPath, Raw: raw, Lines: lines, Lexemes: lexemes})

		parentDir := filepath.Dir(absPath)
		for _, lx := range lexemes {
			switch lx.Kind {
			case kindRR:
				merged = append(merged, sourcedRR{
					RR:        lx.RR,
					File:      absPath,
					StartLine: lx.StartLine,
					EndLine:   lx.EndLine,
				})
			case kindDirective:
				if lx.DirectiveName == directiveInclude {
					target := lx.DirectiveArg
					if !filepath.IsAbs(target) {
						target = filepath.Join(parentDir, target)
					}
					if err := visit(target, append(stack, absPath)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	if err := visit(mainPath, nil); err != nil {
		return nil, nil, err
	}
	return files, merged, nil
}
