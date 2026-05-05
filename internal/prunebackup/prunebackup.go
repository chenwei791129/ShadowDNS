package prunebackup

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/alias"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// Deletion describes one RR flagged for removal by the diff engine.
// Owner/Type/Rdata are in presentation form so the dry-run printer can emit
// them directly; File + StartLine + EndLine drive the line-based writer.
type Deletion struct {
	File      string
	StartLine int
	EndLine   int
	Owner     string
	Type      string
	Rdata     string
}

// Plan captures the full outcome of analysing one (view, backup origin) pair.
// Files holds only the backup zone physical files whose contents actually
// change; files with zero deletions are absent so the apply stage skips them.
type Plan struct {
	View      string
	Deletions []Deletion
	Files     map[string][]byte
}

// PlanPair computes the prune plan for one backup-root pair. It reads the
// backup tree (with $include expansion), reads the root tree, diffs RRSet by
// RRSet under the rewriting rule, and produces per-file pruned contents for
// any file that actually changes.
//
// baseDir is the named.conf `directory` option used for resolving relative
// paths at the top-level mainPath entry only; nested $INCLUDE paths resolve
// against the directory of the file containing them, matching the runtime.
//
// rootFile may be empty to signal root-less mode: PlanPair skips loading any
// root zone and drives every (owner, rtype) RRSet through classifyWithoutRoot.
// In root-less mode, non-overridable types still plan for deletion, but
// overridable types (TXT/MX/SRV) are unconditionally retained because
// byte-equality against root cannot be evaluated. Use this when the caller
// has no root zone for this view (a topology-driven choice, not a config
// error). The caller is responsible for emitting the user-facing INFO log
// announcing root-less mode; PlanPair itself stays log-quiet for root-less.
func PlanPair(
	backupFile, rootFile string,
	viewName, backupOrigin, rootOrigin, baseDir string,
	logger *zap.Logger,
) (*Plan, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	canonBackup := dnsutil.LookupKey(backupOrigin)

	backupFiles, backupMerged, err := loadZoneTree(backupFile, canonBackup, baseDir, 0)
	if err != nil {
		return nil, fmt.Errorf("loading backup zone %q: %w", backupFile, err)
	}

	rootless := rootFile == ""
	var (
		canonRoot string
		rootIdx   rrsetIndex
	)
	if !rootless {
		canonRoot = dnsutil.LookupKey(rootOrigin)
		_, rootMerged, err := loadZoneTree(rootFile, canonRoot, baseDir, 0)
		if err != nil {
			return nil, fmt.Errorf("loading root zone %q: %w", rootFile, err)
		}
		rootRRs := make([]dns.RR, len(rootMerged))
		for i, s := range rootMerged {
			rootRRs[i] = s.RR
		}
		rootIdx = buildRRSetIndex(rootRRs)
	}

	type groupKey struct {
		Owner string
		Rtype uint16
	}
	groups := map[groupKey][]sourcedRR{}
	for _, s := range backupMerged {
		k := groupKey{
			Owner: dnsutil.LookupKey(s.RR.Header().Name),
			Rtype: s.RR.Header().Rrtype,
		}
		groups[k] = append(groups[k], s)
	}

	plan := &Plan{
		View:  viewName,
		Files: map[string][]byte{},
	}
	deleteByFile := map[string][]LineRange{}

	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b groupKey) int {
		if a.Owner != b.Owner {
			return strings.Compare(a.Owner, b.Owner)
		}
		return int(a.Rtype) - int(b.Rtype)
	})

	for _, k := range keys {
		srrs := groups[k]

		var d decision
		if rootless {
			d = classifyWithoutRoot(k.Owner, k.Rtype, canonBackup)
		} else {
			rrsetBackup := make([]dns.RR, len(srrs))
			for i, s := range srrs {
				rrsetBackup[i] = s.RR
			}
			rewrittenOwner := alias.RewriteQName(k.Owner, canonBackup, canonRoot)
			rootRRSet := rootIdx[rrsetKey{Owner: rewrittenOwner, Rtype: k.Rtype}]
			d = classify(rrsetBackup, rootRRSet, k.Owner, k.Rtype, canonBackup)
		}
		if d != decisionDelete {
			continue
		}
		for _, s := range srrs {
			plan.Deletions = append(plan.Deletions, Deletion{
				File:      s.File,
				StartLine: s.StartLine,
				EndLine:   s.EndLine,
				Owner:     s.RR.Header().Name,
				Type:      dns.TypeToString[s.RR.Header().Rrtype],
				Rdata:     strings.TrimPrefix(s.RR.String(), s.RR.Header().String()),
			})
			deleteByFile[s.File] = append(deleteByFile[s.File], LineRange{Start: s.StartLine, End: s.EndLine})
		}
	}

	// Walk every file (including zero-deletion ones) so pruneFile's
	// $GENERATE INFO log fires for operator review. Only files with at
	// least one RR deletion contribute to plan.Files — blank/comment-only
	// stripping is incidental to RR deletion, not a standalone reason to
	// rewrite a file.
	for _, f := range backupFiles {
		ranges := deleteByFile[f.Path]
		pruned := pruneFile(f.Lines, ranges, logger, f.Path)

		if len(ranges) == 0 {
			continue
		}
		newContent := joinLines(pruned, endsWithNewline(f.Raw))
		if !bytes.Equal(f.Raw, newContent) {
			plan.Files[f.Path] = newContent
		}
	}

	return plan, nil
}

func joinLines(lines []string, addTrailingNewline bool) []byte {
	var buf bytes.Buffer
	for i, l := range lines {
		buf.WriteString(l)
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
	}
	if addTrailingNewline && len(lines) > 0 {
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func endsWithNewline(data []byte) bool {
	return len(data) > 0 && data[len(data)-1] == '\n'
}
