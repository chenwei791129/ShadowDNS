// Command shadowdns is the entry point for the ShadowDNS authoritative server.
//
// It parses command-line flags, initializes structured logging, installs
// signal handlers for graceful shutdown, loads configuration and zone data,
// and starts the DNS listeners.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/logging"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// version is set at build time via -ldflags="-X main.version=<tag>".
// It defaults to "dev" for local development builds.
var version = "dev"

// reload re-reads configuration and zone data, then atomically swaps the
// server state and dispatches NOTIFY messages. GeoIP databases are reused
// from startup. On any error the old state is preserved and the error is
// returned.
func reload(
	ctx context.Context,
	opts runOptions,
	srv *server.Server,
	country *view.CountryDB,
	asn *view.ASNDB,
	logger *zap.Logger,
) error {
	logger.Info("reload initiated")

	cfg, err := config.LoadNamedConf(opts.NamedConfPath, logger)
	if err != nil {
		return fmt.Errorf("loading named.conf: %w", err)
	}

	aliases, err := config.LoadAliases(opts.AliasesPath, logger)
	if err != nil {
		return fmt.Errorf("loading aliases: %w", err)
	}

	prev := srv.CurrentState()
	state, summary, err := server.BuildState(cfg, aliases, prev, opts.ReloadVerify, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	srv.SwapState(state)
	maybeDispatchNotifies(ctx, opts, cfg.Options.Notify, state.RootZones, logger)

	// Detect listen-address drift between what is currently bound and what
	// the newly reloaded config would resolve to. We deliberately do NOT
	// rebind listeners here — that would risk downtime and is considered
	// an explicit opt-in operation requiring a process restart.
	//
	// Drift may come from two sources: (a) the operator edited listen-on
	// in named.conf, or (b) the set of local IPv4 interfaces changed
	// since startup (new NIC, IP alias added/removed). Both show up as a
	// non-empty diff against the originally bound set, and neither is
	// applied at reload time.
	currentBound := srv.BoundAddrStrings()
	resolved, resolveErr := server.ResolveListenAddresses(opts.ListenAddr, cfg.Options.ListenOn, logger)
	switch {
	case resolveErr != nil:
		logger.Sugar().Warnw("reload: could not resolve listen addresses from new config; keeping current listeners",
			"err", resolveErr)
	case !server.AddrSetEqual(currentBound, resolved):
		logger.Sugar().Infow(
			"reload: listen-address set differs from bound set; restart to apply (cause: listen-on change and/or interface change since startup)",
			"current_bound", currentBound,
			"new_resolved", resolved,
		)
	}

	logger.Sugar().Infow("reload complete",
		"views", len(cfg.Views),
		"zones", state.ZoneCount(),
		"verify_mode", opts.ReloadVerify.String(),
		"reused", summary.Reused,
		"reparsed", summary.Reparsed,
	)
	return nil
}

// notifyDeadline caps the total time a single NOTIFY goroutine can run.
// The backoff chain (1s+2s+4s) plus per-attempt exchange timeouts can
// exceed this, so later retries may be cut short — that is intentional.
const notifyDeadline = 10 * time.Second

// runOptions captures everything run() needs from the environment.
// Keeping these in a struct makes run() unit-testable without touching globals.
type runOptions struct {
	NamedConfPath string
	AliasesPath   string
	ListenAddr    string
	MetricsAddr   string
	PProfEnable   bool
	DryRun        bool
	// NoNotifyExplicit records whether --no-notify was explicitly passed on
	// the command line (detected via cobra's Flags().Changed()). This is
	// process-lifetime sticky: a SIGHUP reload never re-reads the CLI, so
	// this value remains constant after startup and guarantees "flag >
	// config" precedence even after an operator edits named.conf mid-run.
	NoNotifyExplicit bool
	// ReloadVerify controls how zone file changes are detected on SIGHUP.
	// Set once at startup from --reload-verify; sticky across reloads.
	// Zero value is VerifyModeHash (the safe default).
	ReloadVerify server.VerifyMode
	NoColor      bool
	Logger       *zap.Logger
}

// parseVerifyMode converts the string value of --reload-verify to a VerifyMode.
// Returns an error if the value is not one of "hash", "size", or "none".
func parseVerifyMode(s string) (server.VerifyMode, error) {
	switch s {
	case "hash":
		return server.VerifyModeHash, nil
	case "size":
		return server.VerifyModeSize, nil
	case "none":
		return server.VerifyModeNone, nil
	default:
		return server.VerifyModeHash, fmt.Errorf("invalid --reload-verify value %q: must be one of hash, size, none", s)
	}
}

// resolveNotifyEnabled implements the precedence rule for NOTIFY dispatch:
// an explicit --no-notify CLI flag disables NOTIFY regardless of config;
// otherwise the options.notify directive from named.conf applies; otherwise
// NOTIFY defaults to enabled (preserving pre-change behavior).
//
// Returns the resolved enable state and the source that decided it:
// "flag" | "config" | "default". The source is emitted in the startup/reload
// INFO log so operators can see which input took effect.
func resolveNotifyEnabled(noNotifyExplicit bool, configNotify *bool) (enabled bool, source string) {
	if noNotifyExplicit {
		return false, "flag"
	}
	if configNotify != nil {
		return *configNotify, "config"
	}
	return true, "default"
}

// maybeDispatchNotifies resolves the effective notify state, logs it, and
// dispatches NOTIFY only when enabled. Shared by the startup and reload
// paths so the log format and guard stay in lock-step.
func maybeDispatchNotifies(
	ctx context.Context,
	opts runOptions,
	cfgNotify *bool,
	rootZones map[string]map[string]*zone.Zone,
	logger *zap.Logger,
) {
	enabled, source := resolveNotifyEnabled(opts.NoNotifyExplicit, cfgNotify)
	logger.Sugar().Infow("notify state resolved", "enabled", enabled, "source", source)
	if enabled {
		dispatchNotifies(ctx, rootZones, logger)
	}
}

// newRootCmd constructs the cobra root command that serves authoritative DNS
// when invoked without a subcommand. All server flags are registered on this
// command; the reload subcommand carries its own independent flag set so
// operators cannot pass server-only flags to `shadowdns reload`.
func newRootCmd() *cobra.Command {
	var (
		opts            runOptions
		reloadVerifyStr string
		showVersion     bool
	)

	cmd := &cobra.Command{
		Use:   "shadowdns",
		Short: "Authoritative DNS server",
		Long: `shadowdns is an authoritative DNS server that reads zone data from a
BIND-compatible named.conf and serves queries with view-based GeoIP routing.

All flags are parsed once at startup. SIGHUP re-reads named.conf,
aliases.yaml, and zone files from the paths recorded at startup, but
does not re-parse flags — restart the process to change flag values.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				fmt.Println(version)
				return nil
			}

			verifyMode, err := parseVerifyMode(reloadVerifyStr)
			if err != nil {
				return err
			}
			opts.ReloadVerify = verifyMode

			// Distinguishing "flag not set" from "flag set to false" requires
			// Flags().Changed(); the --no-notify flag's runtime value is
			// intentionally never read (it's registered below without binding
			// to a variable), which is why flag > config precedence holds.
			opts.NoNotifyExplicit = cmd.Flags().Changed("no-notify")

			opts.Logger = logging.New(logging.Options{NoColor: opts.NoColor, Level: zapcore.InfoLevel})
			defer func() { _ = opts.Logger.Sync() }()

			// SIGINT and SIGTERM both trigger graceful shutdown.
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if err := run(ctx, opts); err != nil && !errors.Is(err, context.Canceled) {
				opts.Logger.Sugar().Errorw("shadowdns exited with error", "err", err)
				return err
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.NamedConfPath, "named-conf", "", "path to named.conf (required)")
	f.StringVar(&opts.AliasesPath, "aliases", "", "path to aliases.yaml (optional; missing file is tolerated)")
	f.StringVar(&opts.ListenAddr, "listen", ":53",
		"UDP/TCP listen address. Forms with a host component (e.g. \"127.0.0.1:53\") override named.conf's listen-on. "+
			"Forms without a host (\":PORT\") use the port from --listen but take listen-on addresses from named.conf; "+
			"when listen-on is absent, all IPv4 interface addresses are used.")
	f.StringVar(&opts.MetricsAddr, "metrics-addr", ":9153", "Prometheus /metrics HTTP listen address (empty string disables)")
	f.BoolVar(&opts.PProfEnable, "pprof-enable", false, "Expose Go pprof profiling endpoints under /debug/pprof/ on the metrics HTTP server; requires --metrics-addr to be non-empty")
	f.BoolVar(&opts.DryRun, "dry-run", false, "load configuration and zones, log a summary, then exit without starting listeners")
	// --no-notify is registered without a variable binding: its runtime value
	// is intentionally never read. Explicit-supply detection uses
	// cmd.Flags().Changed("no-notify") in RunE instead, which is the only way
	// to preserve flag > config precedence across cobra's value-or-default model.
	f.Bool("no-notify", false, "disable NOTIFY dispatch (overrides named.conf options.notify)")
	f.StringVar(&reloadVerifyStr, "reload-verify", "hash",
		"zone file change detection strategy on SIGHUP reload: hash (default, safe for rsync -avc --inplace), size (mtime+size only, no file read), none (always full rebuild)")
	f.BoolVar(&opts.NoColor, "no-color", false, "disable colored log output")
	f.BoolVarP(&showVersion, "version", "v", false, "print version and exit")

	cmd.AddCommand(newReloadCmd())
	return cmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// run is the testable core of the server-startup path. It loads configuration
// and zone data, builds a server, starts listeners, and blocks until ctx is
// cancelled.
func run(ctx context.Context, opts runOptions) error {
	if opts.NamedConfPath == "" {
		return errors.New("--named-conf is required")
	}
	if opts.PProfEnable && opts.MetricsAddr == "" {
		return errors.New("--pprof-enable requires --metrics-addr to be non-empty (pprof handlers mount on the metrics HTTP server)")
	}
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Sugar().Infow("shadowdns starting",
		"version", version,
		"named_conf", opts.NamedConfPath,
		"aliases", opts.AliasesPath,
		"listen", opts.ListenAddr,
	)

	cfg, err := config.LoadNamedConf(opts.NamedConfPath, logger)
	if err != nil {
		return fmt.Errorf("loading named.conf: %w", err)
	}

	if cfg.Options.GeoIPDirectory == "" {
		return errors.New("geoip-directory not set in named.conf options")
	}

	aliases, err := config.LoadAliases(opts.AliasesPath, logger)
	if err != nil {
		return fmt.Errorf("loading aliases: %w", err)
	}

	country, asn, err := view.LoadGeoIP(cfg.Options.GeoIPDirectory, logger)
	if err != nil {
		return fmt.Errorf("loading GeoIP: %w", err)
	}
	defer func() {
		if cerr := country.Close(); cerr != nil {
			logger.Sugar().Warnw("closing country mmdb", "err", cerr)
		}
		if cerr := asn.Close(); cerr != nil {
			logger.Sugar().Warnw("closing ASN mmdb", "err", cerr)
		}
	}()

	state, _, err := server.BuildState(cfg, aliases, nil, opts.ReloadVerify, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	// --dry-run: count loaded zones, log summary, and exit without listening.
	if opts.DryRun {
		logger.Sugar().Infow("dry-run: configuration loaded successfully",
			"views", len(cfg.Views),
			"zones", state.ZoneCount(),
		)
		return nil
	}

	srv := server.NewServer(state, logger)

	// Prometheus metrics (disabled when --metrics-addr is empty).
	if opts.MetricsAddr != "" {
		m := metrics.New()
		m.SetBuildInfo(version, runtime.Version())
		// GeoIP metadata is set once at startup; databases are not reloaded
		// on SIGHUP, so these values remain stable for the server lifetime.
		m.SetGeoIPInfo(map[string]uint{
			"country": country.Metadata().BuildEpoch,
			"asn":     asn.Metadata().BuildEpoch,
		})
		srv.Metrics = m
		// Trigger initial zone gauge update. NewServer can't do this
		// because Metrics is assigned after construction.
		rootCounts := make(map[string]int, len(state.RootZones))
		for v, zones := range state.RootZones {
			rootCounts[v] = len(zones)
		}
		backupCounts := make(map[string]int, len(state.BackupZones))
		for v, zones := range state.BackupZones {
			backupCounts[v] = len(zones)
		}
		m.SetZoneCounts(rootCounts, backupCounts)

		mux := http.NewServeMux()
		mux.Handle("/metrics", m.Handler())
		if opts.PProfEnable {
			registerPProfHandlers(mux)
			logger.Sugar().Infow("pprof endpoints enabled", "path", "/debug/pprof/")
		}
		metricsSrv := &http.Server{Addr: opts.MetricsAddr, Handler: mux}

		go func() {
			logger.Sugar().Infow("metrics server starting", "addr", opts.MetricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Sugar().Errorw("metrics server failed", "err", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
				logger.Sugar().Warnw("metrics server shutdown error", "err", err)
			}
		}()
	}

	// Resolve the listen-address set using the precedence described in
	// design.md: explicit host in --listen overrides everything; otherwise
	// named.conf's listen-on drives the host list with the port from
	// --listen; otherwise fall back to all IPv4 interface addresses.
	listenAddrs, err := server.ResolveListenAddresses(opts.ListenAddr, cfg.Options.ListenOn, logger)
	if err != nil {
		return fmt.Errorf("resolving listen addresses: %w", err)
	}

	// Bind listeners before writing the PID file so the port is guaranteed
	// to be available when the PID file appears on disk.
	if err := srv.BindMany(listenAddrs); err != nil {
		return err
	}

	// Fire NOTIFY messages for every loaded root zone in background goroutines.
	// These are best-effort; failures are logged but do not block startup.
	// NOTIFY may be suppressed by --no-notify or options.notify=no; when
	// suppressed, no goroutines, no retries, no network I/O occur.
	maybeDispatchNotifies(ctx, opts, cfg.Options.Notify, state.RootZones, logger)

	// Write PID file if configured. Failure is non-fatal — log a warning and
	// continue so the server still starts even if the directory is missing.
	if pidPath := cfg.Options.PidFile; pidPath != "" {
		if werr := os.WriteFile(pidPath, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644); werr != nil {
			logger.Sugar().Warnw("failed to write PID file", "path", pidPath, "err", werr)
		} else {
			defer func() {
				if rerr := os.Remove(pidPath); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
					logger.Sugar().Warnw("failed to remove PID file", "path", pidPath, "err", rerr)
				}
			}()
		}
	}

	// Listen for SIGHUP to trigger graceful reload.
	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)
	defer signal.Stop(sighupCh)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sighupCh:
				if err := reload(ctx, opts, srv, country, asn, logger); err != nil {
					logger.Sugar().Errorw("reload failed", "err", err)
				}
				// Drain the channel (capacity 1) so that a second SIGHUP
				// received during this reload does not trigger an
				// immediate redundant re-reload.
				select {
				case <-sighupCh:
				default:
				}
			}
		}
	}()

	bound := srv.BoundAddrStrings()
	logger.Sugar().Infow("shadowdns ready",
		"views", len(cfg.Views),
		"bound_addrs", bound,
		"bound_count", len(bound),
	)
	return srv.Serve(ctx)
}

// dispatchNotifies sends NOTIFY messages for every loaded root zone in the
// background. Each zone × NS-target pair becomes its own goroutine; all are
// fire-and-forget with results logged.
func dispatchNotifies(
	ctx context.Context,
	rootZones map[string]map[string]*zone.Zone,
	logger *zap.Logger,
) {
	// De-duplicate (origin, target) pairs across views — the same zone in
	// multiple views still has the same NS records, no need to NOTIFY twice.
	type key struct{ origin, target string }
	seen := make(map[key]bool)

	for _, zonesInView := range rootZones {
		for origin, z := range zonesInView {
			for _, target := range transfer.NotifyTargets(z) {
				k := key{origin, target}
				if seen[k] {
					continue
				}
				seen[k] = true
				go func(origin, target string) {
					// Bound each NOTIFY attempt so a hung NS target cannot leak a
					// goroutine for the lifetime of the server.
					notifyCtx, cancel := context.WithTimeout(ctx, notifyDeadline)
					defer cancel()
					addr := target + ":53"
					if err := transfer.SendNOTIFY(notifyCtx, origin, addr, logger); err != nil {
						logger.Sugar().Warnw("NOTIFY failed",
							"origin", origin,
							"target", target,
							"err", err,
						)
					}
				}(origin, target)
			}
		}
	}
}
