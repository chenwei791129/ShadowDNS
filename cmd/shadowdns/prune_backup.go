package main

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/logging"
	"github.com/chenwei791129/ShadowDNS/internal/prunebackup"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

// outBufferSize is the buffered-writer capacity wrapped around stdout. At
// roughly 80–200 bytes per dry-run line this coalesces several hundred
// lines per syscall, dropping per-line write traffic from millions of
// syscalls to thousands at production scale.
const outBufferSize = 64 * 1024

const msgNoRedundant = "no redundant records found"

// newPruneBackupCmd constructs the `shadowdns prune-backup` subcommand. It
// reads named.conf + the unified config, diffs every (view, backup) pair
// against its aliased root zone, and either reports (dry-run) or writes
// pruned zone files (`--apply`). No network sockets are opened.
func newPruneBackupCmd() *cobra.Command {
	var (
		namedConfPath string
		configPath    string
		applyWrites   bool
		noColor       bool
	)

	cmd := &cobra.Command{
		Use:   "prune-backup",
		Short: "Remove redundant records from backup zone files (offline)",
		Long: `prune-backup scans backup zone files declared in named.conf, compares each
against its aliased root zone, and either reports (dry-run, default) or
removes (--apply) records that the running server would never serve from a
backup zone or whose RRSet is byte-identical to the root zone.

Recommended workflow: run once without --apply to inspect candidates, then
re-run with --apply. Dry-run performs the full parse for every pair and
acts as the parse-time pre-check; per-pair apply does not pre-validate
later pairs before writing earlier ones.

No DNS traffic is exchanged and the running server is not signalled.`,
		SilenceUsage: true,
		// os.Exit is confined to this RunE wrapper so unit tests calling
		// runPruneBackup directly still observe its return value. After a
		// successful run we sync the logger and exit immediately to skip
		// the runtime's final GC sweep over now-released structures.
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := logging.New(logging.Options{NoColor: noColor, Level: zapcore.InfoLevel})
			if err := runPruneBackup(cmd.OutOrStdout(), namedConfPath, configPath, applyWrites, logger); err != nil {
				_ = logger.Sync()
				return err
			}
			_ = logger.Sync()
			os.Exit(0)
			return nil // unreachable: os.Exit does not return
		},
	}

	f := cmd.Flags()
	f.StringVar(&namedConfPath, "named-conf", "", "path to named.conf (required)")
	f.StringVar(&configPath, "config", "", "path to unified ShadowDNS YAML config (required)")
	f.BoolVar(&applyWrites, "apply", false, "write pruned content to disk; without this flag only dry-run output is printed")
	f.BoolVar(&noColor, "no-color", false, "disable colored log output")
	_ = cmd.MarkFlagRequired("named-conf")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

// runPruneBackup executes the prune pipeline as a per-pair streaming
// pipeline: each (view, backup origin) pair is planned, its dry-run lines
// emitted, and (under --apply) its pruned files written before the next
// pair starts. Intermediate plan structures are released between pairs so
// peak memory tracks the largest single pair, not the union of all pairs.
//
// This function returns error/nil so unit tests can assert on its return
// value and on the captured output buffer; os.Exit lives only in the
// cobra RunE wrapper above.
func runPruneBackup(out io.Writer, namedConfPath, configPath string, applyWrites bool, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	bw := bufio.NewWriterSize(out, outBufferSize)
	// Defer is the safety net for every return path (success and error);
	// successful paths additionally Flush explicitly before returning so a
	// later refactor that drops the defer cannot silently swallow output.
	defer func() { _ = bw.Flush() }()

	cfg, err := config.LoadNamedConf(namedConfPath, logger)
	if err != nil {
		return fmt.Errorf("loading named.conf: %w", err)
	}
	shadowCfg, err := shadowdnscfg.Load(configPath, logger)
	if err != nil {
		return fmt.Errorf("loading shadowdns config: %w", err)
	}

	aliases := shadowCfg.Aliases
	if len(aliases) == 0 {
		_, _ = fmt.Fprintln(bw, msgNoRedundant)
		_ = bw.Flush()
		return nil
	}

	baseDir := cfg.Options.Directory

	// Per view, gather (backup, root) file pairs from named.conf + aliases.
	type pair struct {
		view       string
		backupOrig string
		rootOrig   string
		backupFile string
		rootFile   string
	}
	var pairs []pair

	for _, v := range cfg.Views {
		byOrigin := make(map[string]string, len(v.Zones))
		for _, z := range v.Zones {
			byOrigin[z.Name+"."] = z.File
		}
		for backup, root := range aliases {
			backupFile, hasBackup := byOrigin[backup]
			if !hasBackup {
				continue
			}
			rootFile, hasRoot := byOrigin[root]
			if !hasRoot {
				logger.Sugar().Warnw("skipping backup zone: root zone not declared in same view",
					"view", v.Name,
					"backup", backup,
					"root", root,
				)
				continue
			}
			pairs = append(pairs, pair{
				view:       v.Name,
				backupOrig: backup,
				rootOrig:   root,
				backupFile: backupFile,
				rootFile:   rootFile,
			})
		}
	}

	// Deterministic outer-loop order; pair P's full output is emitted
	// before pair P+1 starts, so this also fixes the cross-pair order.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].view != pairs[j].view {
			return pairs[i].view < pairs[j].view
		}
		return pairs[i].backupOrig < pairs[j].backupOrig
	})

	anyDeletion := false
	for _, p := range pairs {
		plan, err := prunebackup.PlanPair(
			p.backupFile, p.rootFile,
			p.view, p.backupOrig, p.rootOrig,
			baseDir, logger,
		)
		if err != nil {
			return err
		}

		if len(plan.Deletions) == 0 {
			continue
		}
		anyDeletion = true

		// Pair-local sort: every (file, line) tuple appears in exactly
		// one pair (each backup zone belongs to one (view, origin)),
		// so per-pair sort produces the same per-line ordering as a
		// global sort while keeping memory bounded to one pair.
		slices.SortFunc(plan.Deletions, func(a, b prunebackup.Deletion) int {
			if c := cmp.Compare(a.File, b.File); c != 0 {
				return c
			}
			return cmp.Compare(a.StartLine, b.StartLine)
		})

		for _, d := range plan.Deletions {
			_, _ = fmt.Fprintf(bw, "%s:%d-%d %s %s %s\n",
				d.File, d.StartLine, d.EndLine, d.Owner, d.Type, d.Rdata)
		}

		if applyWrites {
			if err := prunebackup.ApplyAll(plan.Files, logger); err != nil {
				return err
			}
		}
	}

	if !anyDeletion {
		_, _ = fmt.Fprintln(bw, msgNoRedundant)
	}
	_ = bw.Flush()
	return nil
}
