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
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/api"
	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/ephemeral"
	"github.com/chenwei791129/ShadowDNS/internal/logging"
	"github.com/chenwei791129/ShadowDNS/internal/metrics"
	"github.com/chenwei791129/ShadowDNS/internal/querylog"
	"github.com/chenwei791129/ShadowDNS/internal/ratelimit"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/shadowdnscfg"
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

	// On validation failure the early return leaves the running state —
	// and the ephemeral store — untouched.
	shadowCfg, err := shadowdnscfg.Load(opts.ConfigPath, logger)
	if err != nil {
		return fmt.Errorf("loading shadowdns config: %w", err)
	}

	prev := srv.CurrentState()
	state, summary, err := server.BuildState(cfg, shadowCfg.Aliases, shadowCfg.AliasFlags, shadowCfg.BackupOriginalCase, prev, opts.ReloadVerify, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	srv.SwapState(state)
	if srv.EphemeralStore != nil {
		srv.EphemeralStore.Clear()
	}
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
	resolved, resolveErr := server.ResolveListenAddresses(opts.ListenAddr, cfg.Options.ListenOn, cfg.Options.ListenOnV6, logger)
	switch {
	case resolveErr != nil:
		logger.Sugar().Warnw("reload: could not resolve listen addresses from new config; keeping current listeners",
			"err", resolveErr)
	case !server.AddrSetEqual(currentBound, resolved):
		logger.Sugar().Infow(
			"reload: listen-address set differs from bound set; restart to apply (cause: listen-on/listen-on-v6 change and/or interface change since startup)",
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
	ConfigPath    string
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
	// LogReopener, when non-nil, is the file-backed sink driving zap; the
	// serve loop installs SIGUSR1 only when this is set so subcommands
	// running stderr-only do not inherit a signal handler with no sink.
	LogReopener *logging.ReopenSink
	// ReadyCh is optional; if non-nil, run() closes it once the SIGHUP handler
	// is installed. Production callers leave it nil; test callers use it as an
	// explicit happens-before sync point instead of sleeping.
	ReadyCh chan<- struct{}
}

// parseVerifyMode converts the string value of --reload-verify to a VerifyMode.
// Returns an error if the value is not one of "hash", "size", or "none".
// queryLogSummary renders the query_log field of the dry-run summary: the
// enabled state with the resolved path and effective print option values, or
// the disable reason covering all five disable conditions (task 4.4).
func queryLogSummary(cfg *config.Config) string {
	switch {
	case cfg.QueryLog != nil:
		return fmt.Sprintf("enabled path=%s print-time=%s print-category=%v print-severity=%v",
			cfg.QueryLog.FilePath,
			cfg.QueryLog.PrintTime,
			cfg.QueryLog.PrintCategory,
			cfg.QueryLog.PrintSeverity,
		)
	case cfg.QueryLogDisabledReason != "":
		// logging{} block was present but explicitly disabled by configuration.
		return "disabled reason=" + cfg.QueryLogDisabledReason
	default:
		// No logging{} block at all.
		return "disabled reason=no logging{} block in named.conf"
	}
}

// rateLimitSummary renders the rate_limit field of the dry-run summary: the
// effective per-second / window / slip values when a rate-limit block is
// present, or "disabled" when unconfigured.
func rateLimitSummary(cfg *config.Config) string {
	rl := cfg.Options.RateLimit
	if rl == nil {
		return "disabled reason=no rate-limit block in named.conf options"
	}
	return fmt.Sprintf("enabled responses-per-second=%d nxdomains-per-second=%d errors-per-second=%d all-per-second=%d window=%d slip=%d log-only=%v",
		rl.ResponsesPerSecond, rl.NxdomainsPerSecond, rl.ErrorsPerSecond, rl.AllPerSecond, rl.Window, rl.Slip, rl.LogOnly)
}

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
		logFile         string
	)

	cmd := &cobra.Command{
		Use:   "shadowdns",
		Short: "Authoritative DNS server",
		Long: `shadowdns is an authoritative DNS server that reads zone data from a
BIND-compatible named.conf and serves queries with view-based GeoIP routing.

All flags are parsed once at startup. SIGHUP re-reads named.conf, the
unified ShadowDNS config (--config), and zone files from the paths
recorded at startup, but does not re-parse flags — restart the process
to change flag values.`,
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

			logger, reopener, lerr := logging.New(logging.Options{
				NoColor: opts.NoColor,
				Level:   zapcore.InfoLevel,
				LogFile: logFile,
			})
			if lerr != nil {
				// Surface the open error on stderr regardless of sink
				// configuration so a misconfigured --log-file is visible.
				fmt.Fprintf(os.Stderr, "shadowdns: cannot open log file %q: %v\n", logFile, lerr)
				return lerr
			}
			opts.Logger = logger
			opts.LogReopener = reopener
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
	f.StringVar(&opts.ConfigPath, "config", "", "path to unified ShadowDNS YAML config (required)")
	f.StringVar(&opts.ListenAddr, "listen", ":53",
		"UDP/TCP listen address. Forms with a host component (e.g. \"127.0.0.1:53\", or an IPv6 bracket literal "+
			"\"[::1]:53\") override named.conf's listen-on/listen-on-v6 and bind exactly that single address. "+
			"Forms without a host (\":PORT\") apply the port to the union of named.conf's listen-on (IPv4) and "+
			"listen-on-v6 (IPv6) addresses; when listen-on is absent all IPv4 interface addresses are used, while "+
			"listen-on-v6 is opt-in (absent means no IPv6 listener).")
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
	f.StringVar(&logFile, "log-file", "",
		"write output to this file (O_APPEND|O_CREATE, mode 0640) instead of stderr; "+
			"send SIGUSR1 to make the daemon reopen the file (used by logrotate postrotate). "+
			"Empty string keeps stderr (default).")
	f.BoolVarP(&showVersion, "version", "v", false, "print version and exit")

	cmd.AddCommand(newReloadCmd())
	cmd.AddCommand(newPruneBackupCmd())
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
	if opts.ConfigPath == "" {
		return errors.New("--config is required")
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
		"config", opts.ConfigPath,
		"listen", opts.ListenAddr,
	)

	cfg, err := config.LoadNamedConf(opts.NamedConfPath, logger)
	if err != nil {
		return fmt.Errorf("loading named.conf: %w", err)
	}

	if cfg.Options.GeoIPDirectory == "" {
		return errors.New("geoip-directory not set in named.conf options")
	}

	shadowCfg, err := shadowdnscfg.Load(opts.ConfigPath, logger)
	if err != nil {
		return fmt.Errorf("loading shadowdns config: %w", err)
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

	state, _, err := server.BuildState(cfg, shadowCfg.Aliases, shadowCfg.AliasFlags, shadowCfg.BackupOriginalCase, nil, opts.ReloadVerify, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	// Build the response rate limiter from the options-block rate-limit config
	// (nil when unconfigured). Constructed before the dry-run check so an
	// invalid exempt-clients entry fails fast even under --dry-run. It is built
	// once at startup and is not rebuilt on SIGHUP reload (v1 rate-limit config
	// takes effect at startup only).
	rateLimiter, err := ratelimit.NewLimiter(cfg.Options.RateLimit)
	if err != nil {
		return fmt.Errorf("building rate limiter: %w", err)
	}

	// Open the query log sink when configured. This is done before the dry-run
	// check so that (a) a bad path is caught early even in dry-run, and (b) the
	// rotation warning (task 4.3) is always emitted.
	//
	// On success, qlLogger is injected into the server and qlReopener is added
	// to the SIGUSR1 handler alongside LogReopener. On failure the error names
	// the path so operators can act without reading source code.
	var qlLogger *querylog.Logger
	var qlReopener *logging.ReopenSink
	if cfg.QueryLog != nil {
		// Warn once (task 4.3) — must appear before both the dry-run exit and
		// the normal startup path, so it is always emitted exactly once.
		if cfg.QueryLog.RotationIgnored {
			logger.Sugar().Warnw(
				"query log: BIND rotation parameters (versions/size) are ignored — "+
					"use an external log rotation tool (e.g. logrotate + SIGUSR1) instead",
				"path", cfg.QueryLog.FilePath,
			)
		}

		qlLogger, qlReopener, err = querylog.New(cfg.QueryLog.FilePath, querylog.Config{
			PrintTime:     cfg.QueryLog.PrintTime,
			PrintCategory: cfg.QueryLog.PrintCategory,
			PrintSeverity: cfg.QueryLog.PrintSeverity,
		})
		if err != nil {
			return fmt.Errorf("opening query log %q: %w", cfg.QueryLog.FilePath, err)
		}
	}

	// Resolve the listen-address set up front so --dry-run can report exactly
	// what would be bound (IPv4 from listen-on plus IPv6 from listen-on-v6,
	// emitted in bracket form) without starting any listener. The non-dry-run
	// path below reuses this result, so resolution happens exactly once per
	// startup. Precedence is described in design.md: an explicit host in
	// --listen overrides everything; otherwise named.conf's listen-on /
	// listen-on-v6 drive the host list with the port from --listen; otherwise
	// all IPv4 interface addresses are used.
	listenAddrs, err := server.ResolveListenAddresses(opts.ListenAddr, cfg.Options.ListenOn, cfg.Options.ListenOnV6, logger)
	if err != nil {
		return fmt.Errorf("resolving listen addresses: %w", err)
	}

	// --dry-run: load and validate the unified config (aliases + ephemeral_api),
	// build zone state, log a summary, then exit without starting listeners or
	// the API server. Validation errors from shadowdnscfg.Load above already
	// caused an early return, so reaching here means the config is valid.
	if opts.DryRun {
		logger.Sugar().Infow("dry-run: configuration loaded successfully",
			"views", len(cfg.Views),
			"zones", state.ZoneCount(),
			"listen_addrs", listenAddrs,
			"ephemeral_api", shadowCfg.EphemeralAPI != nil,
			"query_log", queryLogSummary(cfg),
			"rate_limit", rateLimitSummary(cfg),
		)
		// The query log sink was opened above (to fail loudly on a bad path
		// even in dry-run); close it before exiting so dry-run leaks no fd
		// when run() is invoked in-process (tests, future callers).
		if qlReopener != nil {
			if cerr := qlReopener.Close(); cerr != nil {
				logger.Sugar().Warnw("closing query log after dry-run", "err", cerr)
			}
		}
		return nil
	}

	// Ephemeral store lives for the process lifetime and is shared between
	// the DNS handler and the API server. Keeping it outside ServerState
	// means SIGHUP reload does not wipe it passively — the reload handler
	// clears it explicitly only after a successful atomic swap.
	ephemeralStore := ephemeral.NewStore()
	go ephemeralStore.GC(ctx, ephemeral.DefaultGCInterval)

	srv := server.NewServer(state, logger)
	srv.EphemeralStore = ephemeralStore
	// Inject the query log (nil when not configured — server handles nil gracefully).
	srv.QueryLog = qlLogger
	// Inject the rate limiter (nil when no rate-limit block — the server skips
	// installing the wrapper, keeping the response path zero-cost).
	srv.RateLimiter = rateLimiter

	// Ephemeral TXT HTTP API (optional — only started when the ephemeral_api
	// section is present in the unified config).
	if shadowCfg.EphemeralAPI != nil {
		// Read the atomic state snapshot on every call so zones added or
		// removed by a SIGHUP reload take effect on the next PUT without
		// restarting the API server.
		zoneLister := func() []string {
			st := srv.CurrentState()
			if st == nil {
				return nil
			}
			return st.AllOrigins()
		}
		apiSrv := api.NewServer(shadowCfg.EphemeralAPI, ephemeralStore, zoneLister, logger)
		go func() {
			logger.Sugar().Infow("ephemeral API server starting", "listen", shadowCfg.EphemeralAPI.Listen)
			if err := apiSrv.Run(ctx); err != nil {
				logger.Sugar().Errorw("ephemeral API server exited with error", "err", err)
			}
		}()
	}

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
		// Route rate-limit action counts to the metrics registry.
		rateLimiter.SetRecorder(m)
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

	// Bind listeners before writing the PID file so the port is guaranteed
	// to be available when the PID file appears on disk. listenAddrs was
	// resolved once above (shared with the --dry-run preview).
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

	// Listen for SIGUSR1 to reopen file-backed log sinks. Install the handler
	// when either the main log sink or the query log sink is file-backed.
	// Stderr-only runs with no query log have nothing to reopen and do not
	// inherit an unused handler.
	//
	// Both sinks are reopened independently: a failure on one side keeps its
	// old fd open and logs an error, but does not prevent the other side from
	// reopening (task 4.2).
	type sinkReopener struct {
		sink  *logging.ReopenSink
		label string
	}
	var reopeners []sinkReopener
	if opts.LogReopener != nil {
		reopeners = append(reopeners, sinkReopener{opts.LogReopener, "log file"})
	}
	if qlReopener != nil {
		reopeners = append(reopeners, sinkReopener{qlReopener, "query log file"})
	}
	var sigusr1Ch chan os.Signal
	if len(reopeners) > 0 {
		sigusr1Ch = make(chan os.Signal, 1)
		signal.Notify(sigusr1Ch, syscall.SIGUSR1)
		defer signal.Stop(sigusr1Ch)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-sigusr1Ch:
					for _, r := range reopeners {
						if rerr := r.sink.Reopen(); rerr != nil {
							// Either the new path could not be opened
							// (previous fd preserved) or the swap completed
							// but the old fd's Close reported an error
							// (e.g. ENOSPC flush on NFS). In both cases
							// subsequent writes go through a working sink,
							// so this error itself lands.
							logger.Sugar().Errorw(r.label+" reopen reported error",
								"path", r.sink.Path(),
								"err", rerr,
							)
						} else {
							logger.Sugar().Infow(r.label+" reopened",
								"path", r.sink.Path(),
							)
						}
					}

					// Drain a second SIGUSR1 that arrived during the
					// reopen so we do not immediately reopen again.
					select {
					case <-sigusr1Ch:
					default:
					}
				}
			}
		}()
	}

	// Signal test callers that the SIGHUP handler is now attached. Closing
	// this channel here — after signal.Notify but before the dispatch
	// goroutine starts — gives tests an explicit happens-before sync point
	// so they can avoid sleeping to wait for startup.
	if opts.ReadyCh != nil {
		close(opts.ReadyCh)
	}

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

// notifySendFn is the dispatch-layer indirection to transfer.SendNOTIFY.
// Production code never replaces it; tests substitute a fast in-memory stub
// to assert dispatch decisions (one goroutine per IP, no goroutine for
// no-glue targets, cross-view dedup) without real UDP exchanges.
var notifySendFn = transfer.SendNOTIFY

// dispatchNotifies sends NOTIFY messages for every loaded root zone in the
// background. Each (zone, NS host, in-zone glue IP) triple becomes its own
// goroutine; all are fire-and-forget with results logged. NS targets that
// have no in-zone A/AAAA glue are skipped — no goroutine is spawned and no
// system resolver is consulted (RFC 1996 §3.3 best-effort semantics).
func dispatchNotifies(
	ctx context.Context,
	rootZones map[string]map[string]*zone.Zone,
	logger *zap.Logger,
) {
	// De-duplicate (origin, host, ip) triples across views — the same zone in
	// multiple views still has the same NS records, no need to NOTIFY twice
	// to the same IP. netip.Addr is a comparable value type and serves as a
	// map key directly, avoiding per-entry string conversion.
	type key struct {
		origin, host string
		ip           netip.Addr
	}
	seen := make(map[key]bool)

	// source="glue" is invariant for every goroutine spawned by this call;
	// build the attempt logger once and capture it.
	glueLogger := logger.With(zap.String("source", "glue"))

	for _, zonesInView := range rootZones {
		for origin, z := range zonesInView {
			for _, target := range transfer.NotifyTargets(z) {
				if len(target.IPs) == 0 {
					// Out-of-bailiwick NS or missing glue. Record once at debug
					// severity per (origin, host) so the operator can see why
					// no NOTIFY was sent, and spawn no goroutine.
					logger.Sugar().Debugw("NOTIFY skipped: no in-zone glue",
						"zone", origin,
						"target", target.Host,
						"source", "skipped-no-glue",
					)
					continue
				}
				for _, ip := range target.IPs {
					k := key{origin, target.Host, ip}
					if seen[k] {
						continue
					}
					seen[k] = true
					go func(origin, host string, ip netip.Addr) {
						// Bound each NOTIFY attempt so a hung NS target cannot
						// leak a goroutine for the lifetime of the server.
						notifyCtx, cancel := context.WithTimeout(ctx, notifyDeadline)
						defer cancel()
						ipStr := ip.String()
						// Attach target+ip to the logger so both the per-attempt
						// warns inside sendNotifyWithBackoff and the final warn
						// below carry the NS hostname and IP without duplicating
						// keys at every emission site.
						hostLogger := glueLogger.With(
							zap.String("target", host),
							zap.String("ip", ipStr),
						)
						addr := net.JoinHostPort(ipStr, "53")
						if err := notifySendFn(notifyCtx, origin, addr, hostLogger); err != nil {
							hostLogger.Sugar().Warnw("NOTIFY failed",
								"zone", origin,
								"err", err,
							)
						}
					}(origin, target.Host, ip)
				}
			}
		}
	}
}
