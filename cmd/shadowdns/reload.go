package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

// newReloadCmd constructs the `shadowdns reload` subcommand. Reload parses
// named.conf to obtain the pid-file path, reads the PID, and sends SIGHUP
// to the running server. It accepts only --named-conf; server-startup flags
// such as --listen or --reload-verify are intentionally not registered here
// so operators cannot mistake them for runtime knobs.
func newReloadCmd() *cobra.Command {
	var namedConfPath string

	cmd := &cobra.Command{
		Use:          "reload",
		Short:        "Send SIGHUP to a running shadowdns instance",
		Long:         "Parse named.conf for the pid-file option, read the PID, and send SIGHUP to that process so it reloads zone data.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runReload(namedConfPath, nil)
		},
	}

	cmd.Flags().StringVar(&namedConfPath, "named-conf", "", "path to named.conf (required)")
	_ = cmd.MarkFlagRequired("named-conf")
	return cmd
}

// runReload sends SIGHUP to a running ShadowDNS instance by reading the PID
// from the pid-file configured in named.conf.
func runReload(namedConfPath string, logger *zap.Logger) error {
	// Direct unit-test call sites bypass cobra's MarkFlagRequired, so we still
	// need this guard even though reloadCmd would never pass an empty string.
	if namedConfPath == "" {
		return fmt.Errorf("--named-conf is required for reload")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	cfg, err := config.LoadNamedConf(namedConfPath, logger)
	if err != nil {
		return fmt.Errorf("loading named.conf: %w", err)
	}

	if cfg.Options.PidFile == "" {
		return fmt.Errorf("pid-file not configured in named.conf; cannot determine server PID")
	}

	data, err := os.ReadFile(cfg.Options.PidFile)
	if err != nil {
		return fmt.Errorf("reading pid-file %q: %w", cfg.Options.PidFile, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("parsing PID from %q: %w", cfg.Options.PidFile, err)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("sending SIGHUP to PID %d: %w", pid, err)
	}

	return nil
}
