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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/config"
	"github.com/chenwei791129/ShadowDNS/internal/server"
	"github.com/chenwei791129/ShadowDNS/internal/transfer"
	"github.com/chenwei791129/ShadowDNS/internal/view"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// notifyDeadline caps the total time a single NOTIFY goroutine can block,
// including the 1s/2s/4s backoff chain. Anything longer would leak a
// goroutine behind a hung peer for the lifetime of the server.
const notifyDeadline = 10 * time.Second

// runOptions captures everything run() needs from the environment.
// Keeping these in a struct makes run() unit-testable without touching globals.
type runOptions struct {
	NamedConfPath string
	AliasesPath   string
	ListenAddr    string
	DryRun        bool
	Logger        *slog.Logger
}

func main() {
	var opts runOptions
	flag.StringVar(&opts.NamedConfPath, "named-conf", "", "path to named.conf (required)")
	flag.StringVar(&opts.AliasesPath, "aliases", "", "path to aliases.yaml (optional; missing file is tolerated)")
	flag.StringVar(&opts.ListenAddr, "listen", ":53", "UDP/TCP listen address")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "load configuration and zones, log a summary, then exit without starting listeners")
	flag.Parse()

	opts.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

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
		return errors.New("--named-conf is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("shadowdns starting",
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

	// --dry-run: count loaded zones, log summary, and exit without listening.
	if opts.DryRun {
		zoneCount := 0
		for _, zones := range state.RootZones {
			zoneCount += len(zones)
		}
		for _, zones := range state.BackupZones {
			zoneCount += len(zones)
		}
		logger.Info("dry-run: configuration loaded successfully",
			"views", len(cfg.Views),
			"zones", zoneCount,
		)
		return nil
	}

	srv := server.NewServer(state, logger)

	// Fire NOTIFY messages for every loaded root zone in background goroutines.
	// These are best-effort; failures are logged but do not block startup.
	dispatchNotifies(ctx, state.RootZones, logger)

	logger.Info("shadowdns ready", "views", len(cfg.Views), "listen", opts.ListenAddr)
	return srv.Start(ctx, opts.ListenAddr)
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
