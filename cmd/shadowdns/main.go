// Command shadowdns is the entry point for the ShadowDNS authoritative server.
//
// It parses command-line flags, initializes structured logging, installs
// signal handlers for graceful shutdown, loads configuration and zone data,
// and starts the DNS listeners.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/config"
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
	logger *slog.Logger,
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

	state, err := server.BuildState(cfg, aliases, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	srv.SwapState(state)
	dispatchNotifies(ctx, state.RootZones, logger)

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
		logger.Warn("reload: could not resolve listen addresses from new config; keeping current listeners",
			"err", resolveErr)
	case !server.AddrSetEqual(currentBound, resolved):
		logger.Info(
			"reload: listen-address set differs from bound set; restart to apply (cause: listen-on change and/or interface change since startup)",
			"current_bound", currentBound,
			"new_resolved", resolved,
		)
	}

	logger.Info("reload complete", "views", len(cfg.Views), "zones", state.ZoneCount())
	return nil
}

// runReload sends SIGHUP to a running ShadowDNS instance by reading the PID
// from the pid-file configured in named.conf.
func runReload(opts runOptions) error {
	if opts.NamedConfPath == "" {
		return fmt.Errorf("-named-conf is required for -reload")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	cfg, err := config.LoadNamedConf(opts.NamedConfPath, logger)
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
	DryRun        bool
	Logger        *slog.Logger
}

func main() {
	var opts runOptions
	flag.StringVar(&opts.NamedConfPath, "named-conf", "", "path to named.conf (required)")
	flag.StringVar(&opts.AliasesPath, "aliases", "", "path to aliases.yaml (optional; missing file is tolerated)")
	flag.StringVar(&opts.ListenAddr, "listen", ":53",
		"UDP/TCP listen address. Forms with a host component (e.g. \"127.0.0.1:53\") override named.conf's listen-on. "+
			"Forms without a host (\":PORT\") use the port from -listen but take listen-on addresses from named.conf; "+
			"when listen-on is absent, all IPv4 interface addresses are used.")
	flag.StringVar(&opts.MetricsAddr, "metrics-addr", ":9153", "Prometheus /metrics HTTP listen address (empty string disables)")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "load configuration and zones, log a summary, then exit without starting listeners")

	var reloadFlag bool
	flag.BoolVar(&reloadFlag, "reload", false, "send SIGHUP to a running server (requires -named-conf)")

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")

	flag.Parse()

	if showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	opts.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if reloadFlag {
		if err := runReload(opts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// SIGINT and SIGTERM both trigger graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, opts); err != nil && !errors.Is(err, context.Canceled) {
		opts.Logger.Error("shadowdns exited with error", "err", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run is the testable core of main(). It loads configuration and zone data,
// builds a server, starts listeners, and blocks until ctx is cancelled.
func run(ctx context.Context, opts runOptions) error {
	if opts.NamedConfPath == "" {
		return errors.New("-named-conf is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("shadowdns starting",
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
			logger.Warn("closing country mmdb", "err", cerr)
		}
		if cerr := asn.Close(); cerr != nil {
			logger.Warn("closing ASN mmdb", "err", cerr)
		}
	}()

	state, err := server.BuildState(cfg, aliases, country, asn, logger)
	if err != nil {
		return fmt.Errorf("building server state: %w", err)
	}

	// -dry-run: count loaded zones, log summary, and exit without listening.
	if opts.DryRun {
		logger.Info("dry-run: configuration loaded successfully",
			"views", len(cfg.Views),
			"zones", state.ZoneCount(),
		)
		return nil
	}

	srv := server.NewServer(state, logger)

	// Prometheus metrics (disabled when -metrics-addr is empty).
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
		metricsSrv := &http.Server{Addr: opts.MetricsAddr, Handler: mux}

		go func() {
			logger.Info("metrics server starting", "addr", opts.MetricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server failed", "err", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
				logger.Warn("metrics server shutdown error", "err", err)
			}
		}()
	}

	// Resolve the listen-address set using the precedence described in
	// design.md: explicit host in -listen overrides everything; otherwise
	// named.conf's listen-on drives the host list with the port from
	// -listen; otherwise fall back to all IPv4 interface addresses.
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
	dispatchNotifies(ctx, state.RootZones, logger)

	// Write PID file if configured. Failure is non-fatal — log a warning and
	// continue so the server still starts even if the directory is missing.
	if pidPath := cfg.Options.PidFile; pidPath != "" {
		if werr := os.WriteFile(pidPath, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644); werr != nil {
			logger.Warn("failed to write PID file", "path", pidPath, "err", werr)
		} else {
			defer func() {
				if rerr := os.Remove(pidPath); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
					logger.Warn("failed to remove PID file", "path", pidPath, "err", rerr)
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
					logger.Error("reload failed", "err", err)
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
	logger.Info("shadowdns ready",
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
	logger *slog.Logger,
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
						logger.Warn("NOTIFY failed",
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
