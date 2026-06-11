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
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
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

// errGeoIPDirectoryUnset is shared by the startup and reload validations so
// the two error messages cannot drift.
var errGeoIPDirectoryUnset = errors.New("geoip-directory not set in named.conf options")

// warnClose closes c and logs a warning when the close fails; label names the
// handle in the log line.
func warnClose(c io.Closer, label string, logger *zap.Logger) {
	if err := c.Close(); err != nil {
		logger.Sugar().Warnw("closing "+label, "err", err)
	}
}

// openQueryLog emits the rotation-ignored warning when applicable and opens
// the query-log sink for qlCfg, mapping the parsed named.conf options onto
// querylog.Config. Shared by the startup and reload paths so the warning text
// and the field mapping cannot drift between them.
func openQueryLog(qlCfg *config.QueryLogConfig, logger *zap.Logger) (*querylog.Logger, *logging.ReopenSink, error) {
	if qlCfg.RotationIgnored {
		logger.Sugar().Warnw(
			"query log: BIND rotation parameters (versions/size) are ignored — "+
				"use an external log rotation tool (e.g. logrotate + SIGUSR1) instead",
			"path", qlCfg.FilePath,
		)
	}
	lg, sink, err := querylog.New(qlCfg.FilePath, querylog.Config{
		PrintTime:     qlCfg.PrintTime,
		PrintCategory: qlCfg.PrintCategory,
		PrintSeverity: qlCfg.PrintSeverity,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("opening query log %q: %w", qlCfg.FilePath, err)
	}
	return lg, sink, nil
}

// queryLogState tracks the currently active query-log configuration and its
// file sink. querylog.Logger retains neither, so reload comparison and sink
// close/reopen go through this record. Shared between run(), the SIGHUP
// reload goroutine, and the SIGUSR1 goroutine via an atomic.Pointer; only the
// SIGHUP goroutine writes after startup.
type queryLogState struct {
	cfg  *config.QueryLogConfig // nil when query logging is disabled
	sink *logging.ReopenSink    // nil when query logging is disabled
}

// queryLogConfigEqual reports whether the active and reloaded query-log
// configurations are identical. Whole-value equality covers every field of
// config.QueryLogConfig (FilePath, the three print options, RotationIgnored),
// so a field added in the future cannot be silently excluded from change
// detection.
func queryLogConfigEqual(a, b *config.QueryLogConfig) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// geoipRuntime owns the live GeoIP handles plus the generation swapped out by
// the most recent reload. The swapped-out generation is closed at the start
// of the next reload (or at shutdown), never immediately after the state swap
// — in-flight queries may still resolve views against the previous state, and
// closing an mmdb unmaps its memory (use-after-munmap is a fatal,
// unrecoverable crash). No locking: the writers are the startup path (before
// any goroutine starts) and the SIGHUP goroutine, and shutdown reads only
// after that goroutine is joined.
type geoipRuntime struct {
	country *view.CountryDB
	asn     *view.ASNDB
	// prevCountry/prevASN hold the generation deferred for close; nil before
	// the first reload.
	prevCountry *view.CountryDB
	prevASN     *view.ASNDB
}

// closePrev closes and clears the deferred-close generation. Called at the
// start of a reload — a full reload interval after that generation was
// swapped out, when no in-flight query can still reference it.
func (g *geoipRuntime) closePrev(logger *zap.Logger) {
	if g.prevCountry != nil {
		warnClose(g.prevCountry, "superseded country mmdb", logger)
		g.prevCountry = nil
	}
	if g.prevASN != nil {
		warnClose(g.prevASN, "superseded ASN mmdb", logger)
		g.prevASN = nil
	}
}

// closeAll closes every live handle (current and deferred) and clears the
// fields so a repeated call is a no-op. Used by run()'s shutdown defer, which
// executes after the signal goroutines are joined, and by tests.
func (g *geoipRuntime) closeAll(logger *zap.Logger) {
	g.closePrev(logger)
	if g.country != nil {
		warnClose(g.country, "country mmdb", logger)
		g.country = nil
	}
	if g.asn != nil {
		warnClose(g.asn, "ASN mmdb", logger)
		g.asn = nil
	}
}

// reload re-reads configuration and zone data, re-opens the GeoIP mmdb files,
// rebuilds the rate limiter, re-applies the query-log configuration, then
// atomically swaps the server state and dispatches NOTIFY messages. Every
// fallible step runs before the state swap; on any error the old state (zones,
// GeoIP handles, limiter, query log) is preserved in full and the error is
// returned. The GeoIP handles superseded by a successful reload are not closed
// here — they are parked in geo's deferred-close slot and released at the
// start of the next reload or at shutdown (deferred-by-one-generation close).
func reload(
	ctx context.Context,
	opts runOptions,
	srv *server.Server,
	geo *geoipRuntime,
	qlState *atomic.Pointer[queryLogState],
	logger *zap.Logger,
) (err error) {
	logger.Info("reload initiated")

	// Record the outcome exactly once per attempt, whichever step fails.
	// All three metrics methods are nil-receiver safe, so a disabled-metrics
	// configuration (srv.Metrics == nil) needs no special case.
	defer func() {
		if err != nil {
			srv.Metrics.RecordReload("failure")
		} else {
			srv.Metrics.RecordReload("success")
			srv.Metrics.SetLastReloadSuccess(time.Now())
		}
	}()

	// Step 0: release the GeoIP generation parked by the previous reload. A
	// full reload interval has passed since it was swapped out, so no
	// in-flight query can still hold a state snapshot that references it.
	geo.closePrev(logger)

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

	// Mirror the startup validation: an empty geoip-directory must surface as
	// an explicit configuration error, not a confusing relative-path open error.
	if cfg.Options.GeoIPDirectory == "" {
		return errGeoIPDirectoryUnset
	}
	newCountry, newASN, err := view.LoadGeoIP(cfg.Options.GeoIPDirectory, logger)
	if err != nil {
		return fmt.Errorf("reloading GeoIP: %w", err)
	}
	// Release the freshly opened handles when a later fallible step aborts
	// the reload; the running state keeps the old handles. On success (err ==
	// nil) the handles have been installed into geo and must stay open.
	defer func() {
		if err != nil {
			warnClose(newCountry, "new country mmdb after failed reload", logger)
			warnClose(newASN, "new ASN mmdb after failed reload", logger)
		}
	}()

	prev := srv.CurrentState()
	state, summary, err := server.BuildState(cfg, shadowCfg.Aliases, shadowCfg.AliasFlags, shadowCfg.BackupOriginalCase, prev, opts.ReloadVerify, newCountry, newASN, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	// Rebuild the rate limiter from the reloaded config (nil when the
	// rate-limit block is absent). The credit table is deliberately reset.
	newLimiter, err := ratelimit.NewLimiter(cfg.Options.RateLimit)
	if err != nil {
		return fmt.Errorf("rebuilding rate limiter: %w", err)
	}
	// Guard against the typed-nil trap: storing a nil *metrics.Metrics in the
	// Recorder interface would pass the limiter's own nil check and panic on
	// the first RRL decision.
	if srv.Metrics != nil {
		newLimiter.SetRecorder(srv.Metrics)
	}

	// Re-apply the query-log configuration. An unchanged config (whole-value
	// equality, including RotationIgnored) keeps the existing logger and sink
	// with no file operations; any difference opens a new sink here (fallible)
	// and installs it after the swap.
	curQL := qlState.Load()
	newQLCfg := cfg.QueryLog
	qlChanged := !queryLogConfigEqual(curQL.cfg, newQLCfg)
	var newQLLogger *querylog.Logger
	var newQLSink *logging.ReopenSink
	if qlChanged && newQLCfg != nil {
		// openQueryLog re-emits the rotation warning on this change path
		// only; the reuse path performs no file operations and no re-warning.
		newQLLogger, newQLSink, err = openQueryLog(newQLCfg, logger)
		if err != nil {
			return err
		}
	}

	// All fallible steps are done. Everything below is infallible installation.
	srv.SwapState(state)

	srv.RateLimiter.Store(newLimiter)
	if qlChanged {
		srv.QueryLog.Store(newQLLogger)
		qlState.Store(&queryLogState{cfg: newQLCfg, sink: newQLSink})
		// Close the superseded sink last so the SIGUSR1 goroutine's racy
		// window only ever touches a still-open sink; a Reopen that loses the
		// race gets os.ErrClosed (close is terminal) and logs it.
		if curQL.sink != nil {
			if cerr := curQL.sink.Close(); cerr != nil {
				logger.Sugar().Warnw("closing superseded query log sink", "err", cerr)
			}
		}
	}

	// Rotate the superseded GeoIP generation into the deferred-close slot —
	// never close it here; in-flight queries may still resolve views against
	// the pre-swap state snapshot.
	geo.prevCountry, geo.prevASN = geo.country, geo.asn
	geo.country, geo.asn = newCountry, newASN
	srv.Metrics.SetGeoIPInfo(map[string]uint{
		"country": newCountry.Metadata().BuildEpoch,
		"asn":     newASN.Metadata().BuildEpoch,
	})

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
	// ECSEnable turns on RFC 7871 EDNS Client Subnet processing in the DNS
	// handler. Process-lifetime sticky: a SIGHUP reload never re-reads the
	// CLI, so the value set at startup stays in effect until restart.
	ECSEnable bool
	DryRun    bool
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
	f.BoolVar(&opts.ECSEnable, "ecs-enable", false, "enable RFC 7871 EDNS Client Subnet processing: a valid ECS address in a query drives GeoIP view selection (country/ASN rules only; IP/CIDR ACL rules always use the real source IP) and responses echo the ECS option. Disabled by default — queries' ECS options are ignored and responses never carry one, matching BIND")
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
		return errGeoIPDirectoryUnset
	}

	shadowCfg, err := shadowdnscfg.Load(opts.ConfigPath, logger)
	if err != nil {
		return fmt.Errorf("loading shadowdns config: %w", err)
	}

	country, asn, err := view.LoadGeoIP(cfg.Options.GeoIPDirectory, logger)
	if err != nil {
		return fmt.Errorf("loading GeoIP: %w", err)
	}
	// geo owns every live GeoIP handle from here on; reloads rotate new
	// generations through it. The single defer covers both the normal
	// shutdown path (it runs after the signal goroutines are joined in the
	// function body) and every early-return path below.
	geo := &geoipRuntime{country: country, asn: asn}
	defer geo.closeAll(logger)

	state, _, err := server.BuildState(cfg, shadowCfg.Aliases, shadowCfg.AliasFlags, shadowCfg.BackupOriginalCase, nil, opts.ReloadVerify, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	// Build the response rate limiter from the options-block rate-limit config
	// (nil when unconfigured). Constructed before the dry-run check so an
	// invalid exempt-clients entry fails fast even under --dry-run. A SIGHUP
	// reload rebuilds it from the reloaded config and installs the
	// replacement atomically.
	rateLimiter, err := ratelimit.NewLimiter(cfg.Options.RateLimit)
	if err != nil {
		return fmt.Errorf("building rate limiter: %w", err)
	}

	// Open the query log sink when configured. This is done before the dry-run
	// check so that (a) a bad path is caught early even in dry-run, and (b)
	// openQueryLog's rotation warning is always emitted exactly once.
	//
	// On success, qlLogger is injected into the server and qlReopener becomes
	// the initial qlState sink consumed by the SIGUSR1 handler. On failure the
	// error names the path so operators can act without reading source code.
	var qlLogger *querylog.Logger
	var qlReopener *logging.ReopenSink
	if cfg.QueryLog != nil {
		qlLogger, qlReopener, err = openQueryLog(cfg.QueryLog, logger)
		if err != nil {
			return err
		}
	}

	// qlState is the single source of truth for the active query-log config
	// and sink: querylog.Logger retains neither, and a SIGHUP reload may
	// replace both. Shared with the SIGHUP and SIGUSR1 goroutines.
	var qlState atomic.Pointer[queryLogState]
	qlState.Store(&queryLogState{cfg: cfg.QueryLog, sink: qlReopener})
	// Close whichever query-log sink is active when run() returns — possibly
	// one opened by a later reload. The defer runs after the shutdown join
	// sequence in the function body, so it cannot race an in-flight reload,
	// and it also covers every early-return path (dry-run, bind failures).
	defer func() {
		if st := qlState.Load(); st.sink != nil {
			if cerr := st.sink.Close(); cerr != nil {
				logger.Sugar().Warnw("closing query log sink", "err", cerr)
			}
		}
	}()

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

	// ECS state is logged in both flag states, before the dry-run early exit,
	// so dry-run output also reports it (spec: startup log states ECS state).
	logger.Sugar().Infow("EDNS Client Subnet (ECS) processing",
		"enabled", opts.ECSEnable,
	)

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
		// The query log sink opened above (to fail loudly on a bad path even
		// in dry-run) is closed by the qlState defer.
		return nil
	}

	// Ephemeral store lives for the process lifetime and is shared between
	// the DNS handler and the API server. Keeping it outside ServerState
	// means SIGHUP reload does not wipe it passively — the reload handler
	// clears it explicitly only after a successful atomic swap.
	ephemeralStore := ephemeral.NewStore()
	go ephemeralStore.GC(ctx, ephemeral.DefaultGCInterval)

	srv := server.NewServer(state, logger)
	srv.ECSEnabled = opts.ECSEnable
	srv.EphemeralStore = ephemeralStore
	// Inject the query log (nil when not configured — server handles nil gracefully).
	srv.QueryLog.Store(qlLogger)
	// Inject the rate limiter (nil when no rate-limit block — the server skips
	// installing the wrapper, keeping the response path zero-cost).
	srv.RateLimiter.Store(rateLimiter)

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
		// GeoIP metadata from the startup load; a successful SIGHUP reload
		// re-opens the mmdb files and updates this gauge to the new build
		// epochs (deleting the stale build_time series).
		m.SetGeoIPInfo(map[string]uint{
			"country": country.Metadata().BuildEpoch,
			"asn":     asn.Metadata().BuildEpoch,
		})
		// The initial configuration load succeeded, so initialise the
		// last-reload-success gauge to the startup time (mirroring
		// Prometheus's own behaviour) — `time() - <gauge>` staleness alerts
		// must not fire on servers that never reload. The metrics HTTP
		// listener starts below, so the registration-time 0 is never
		// externally observable.
		m.SetLastReloadSuccess(time.Now())
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

	// Install the SIGHUP and SIGUSR1 handlers and start their dispatch
	// goroutines. SIGUSR1 is registered unconditionally in daemon mode: a
	// SIGHUP reload can introduce a query log at any time, so registration
	// must not depend on a file sink existing at startup.
	shutdownSignals := runSignalHandlers(ctx, opts, srv, geo, &qlState, logger)

	// Signal test callers that the signal handlers are now attached, giving
	// them an explicit happens-before sync point instead of sleeping.
	if opts.ReadyCh != nil {
		close(opts.ReadyCh)
	}

	bound := srv.BoundAddrStrings()
	logger.Sugar().Infow("shadowdns ready",
		"views", len(cfg.Views),
		"bound_addrs", bound,
		"bound_count", len(bound),
	)
	serveErr := srv.Serve(ctx)

	// Shutdown-order contract: Serve has returned (whether because ctx was
	// cancelled or because a listener died — the latter leaves ctx alive, so
	// this sequence must not rely on it). Joining the signal goroutines here,
	// before the deferred sink/GeoIP closes run, gives shutdown a
	// happens-before edge over any in-flight reload's writes.
	shutdownSignals()
	return serveErr
}

// runSignalHandlers registers the SIGHUP (reload) and SIGUSR1 (log reopen)
// handlers and starts one dispatch goroutine for each. Both goroutines listen
// on a child context derived from ctx — srv.Serve can return on a listener
// error while the parent ctx is still alive, and the join below must not
// deadlock on that path. reload() receives the child context too, so NOTIFY
// goroutines spawned by a last-moment reload are cancelled with the shutdown
// sequence.
//
// The returned function implements the shutdown-order contract: stop signal
// delivery, cancel the child context, then join both goroutines (waiting out
// an in-flight reload). The caller invokes it after srv.Serve returns and
// before its deferred query-log-sink and GeoIP closes execute.
func runSignalHandlers(
	ctx context.Context,
	opts runOptions,
	srv *server.Server,
	geo *geoipRuntime,
	qlState *atomic.Pointer[queryLogState],
	logger *zap.Logger,
) (shutdown func()) {
	sigCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)
	sigusr1Ch := make(chan os.Signal, 1)
	signal.Notify(sigusr1Ch, syscall.SIGUSR1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sigCtx.Done():
				return
			case <-sighupCh:
				if err := reload(sigCtx, opts, srv, geo, qlState, logger); err != nil {
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
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sigCtx.Done():
				return
			case <-sigusr1Ch:
				reopenLogSinks(opts.LogReopener, qlState, logger)
				// Drain a second SIGUSR1 that arrived during the
				// reopen so we do not immediately reopen again.
				select {
				case <-sigusr1Ch:
				default:
				}
			}
		}
	}()

	return func() {
		// Stop new signal injection first so repeated signals during
		// shutdown cannot prolong it indefinitely, then unblock the
		// goroutines and wait for them — including a reload in progress.
		signal.Stop(sighupCh)
		signal.Stop(sigusr1Ch)
		cancel()
		wg.Wait()
	}
}

// reopenLogSinks reopens the file-backed log sinks in response to SIGUSR1.
// The list is assembled at signal time: the fixed main-log sink (decided at
// startup) plus the currently active query-log sink from qlState — so a query
// log introduced or replaced by a SIGHUP reload is always the one reopened.
//
// Sinks are reopened independently: a failure on one side keeps its old fd
// open and logs an error without affecting the other. A sink concurrently
// retired by a reload reports os.ErrClosed (close is terminal), which is
// logged and harmless — the newly installed sink is unaffected.
func reopenLogSinks(mainSink *logging.ReopenSink, qlState *atomic.Pointer[queryLogState], logger *zap.Logger) {
	reopen := func(sink *logging.ReopenSink, label string) {
		if rerr := sink.Reopen(); rerr != nil {
			// Either the new path could not be opened (previous fd
			// preserved), the swap completed but the old fd's Close reported
			// an error (e.g. ENOSPC flush on NFS), or the sink was retired by
			// a concurrent reload (os.ErrClosed). In every case subsequent
			// writes go through a working sink, so this error itself lands.
			logger.Sugar().Errorw(label+" reopen reported error",
				"path", sink.Path(),
				"err", rerr,
			)
		} else {
			logger.Sugar().Infow(label+" reopened",
				"path", sink.Path(),
			)
		}
	}
	if mainSink != nil {
		reopen(mainSink, "log file")
	}
	if st := qlState.Load(); st != nil && st.sink != nil {
		reopen(st.sink, "query log file")
	}
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
