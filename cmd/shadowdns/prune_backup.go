package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/logging"
	"github.com/chenwei791129/ShadowDNS/internal/prunebackup"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
)

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

No DNS traffic is exchanged and the running server is not signalled.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := logging.New(logging.Options{NoColor: noColor, Level: zapcore.InfoLevel})
			defer func() { _ = logger.Sync() }()
			return runPruneBackup(cmd.OutOrStdout(), namedConfPath, configPath, applyWrites, logger)
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

// runPruneBackup executes the prune pipeline. It is extracted from RunE so
// unit tests can exercise it without cobra argument parsing.
func runPruneBackup(out io.Writer, namedConfPath, configPath string, applyWrites bool, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

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
		_, _ = fmt.Fprintln(out, "no redundant records found")
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

	// Deterministic order for stable dry-run output.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].view != pairs[j].view {
			return pairs[i].view < pairs[j].view
		}
		return pairs[i].backupOrig < pairs[j].backupOrig
	})

	var allDeletions []prunebackup.Deletion
	mergedFiles := map[string][]byte{}
	for _, p := range pairs {
		plan, err := prunebackup.PlanPair(
			p.backupFile, p.rootFile,
			p.view, p.backupOrig, p.rootOrig,
			baseDir, logger,
		)
		if err != nil {
			return err
		}
		allDeletions = append(allDeletions, plan.Deletions...)
		for f, content := range plan.Files {
			mergedFiles[f] = content
		}
	}

	if len(allDeletions) == 0 {
		_, _ = fmt.Fprintln(out, "no redundant records found")
		return nil
	}

	// Stable print order: by file path, then start line.
	sort.Slice(allDeletions, func(i, j int) bool {
		a, b := allDeletions[i], allDeletions[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.StartLine < b.StartLine
	})

	for _, d := range allDeletions {
		_, _ = fmt.Fprintf(out, "%s:%d-%d %s %s %s\n",
			d.File, d.StartLine, d.EndLine, d.Owner, d.Type, d.Rdata)
	}

	if !applyWrites {
		return nil
	}
	return prunebackup.ApplyAll(mergedFiles, logger)
}
