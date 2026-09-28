package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ardanlabs/conf/v3"
	"github.com/chewycrunch/shopify-monitor/internal/config"
	"github.com/chewycrunch/shopify-monitor/internal/monitor"
	"github.com/chewycrunch/shopify-monitor/internal/proxy"
	"github.com/chewycrunch/shopify-monitor/internal/webhook"
)

// Fetch variants and load them into the map

// Compare variants to the map, see if any availability has changed, or new variants exist
// If so, send a webhook to the webhook URL
var wg sync.WaitGroup

// shutdownDrain bounds how long a graceful shutdown spends delivering the
// alerts it is already holding.
const shutdownDrain = 5 * time.Second

// Spawns one monitor goroutine per store in cfg.WebsitesFile and returns; the
// goroutines it starts are tracked on wg.
func startMonitorService(ctx context.Context, wg *sync.WaitGroup, cfg config.Config, proxyManager *proxy.ProxyManager, destinations *webhook.Registry) error {
	file, err := os.Open(cfg.WebsitesFile)
	if err != nil {
		return fmt.Errorf("open websites file: %w", err)
	}
	defer file.Close()

	stores, err := cfg.ParseStores(file)
	if err != nil {
		return err
	}

	// Resolved before any goroutine starts: a webhook URL nothing can deliver to
	// is a configuration that cannot work, and the moment to say so is before
	// the monitor is polling rather than during the drop it was deployed for.
	for _, store := range stores {
		dest, err := destinationFor(cfg, destinations, store)
		if err != nil {
			return err
		}

		wg.Add(1)

		go func() {
			defer wg.Done()
			m := monitor.NewMonitor(store.URL, dest, proxyManager, cfg.PageWorkers, store.MaxProducts)

			// Retry rather than give up: the baseline fetch goes through the
			// same rotating proxies as every later one, so a single timeout
			// here would otherwise drop this store for the life of the process.
			for attempt := 1; ; attempt++ {
				err := m.InitializeVariants(ctx)
				if err == nil {
					break
				}
				if ctx.Err() != nil {
					return
				}
				slog.Warn("initialize failed, retrying",
					"site", m.Url, "attempt", attempt, "err", err)

				select {
				case <-ctx.Done():
					return
				case <-time.After(store.Delay):
				}
			}

			// A cancelled context is the shutdown working, not a fault.
			if err := m.StartWatching(ctx, store.Delay); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("stopped watching", "site", m.Url, "err", err)
			}
		}()
	}

	return nil
}

// destinationFor resolves where one store's alerts go, naming the row in the
// stores file when a webhook URL matches no destination this program can reach.
func destinationFor(cfg config.Config, destinations *webhook.Registry, store config.Store) (webhook.Destination, error) {
	urls := []string{store.WebhookURL}

	dests := make([]webhook.Destination, 0, len(urls))
	for _, u := range urls {
		d, err := destinations.For(u)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", cfg.WebsitesFile, store.Line, err)
		}
		dests = append(dests, d)
	}

	return webhook.Fanout(dests...)
}

// loadProxies reads path into a ProxyManager. A missing file yields an empty
// manager rather than an error — monitors then run over direct connections.
func loadProxies(path string) (*proxy.ProxyManager, error) {
	pm := proxy.NewProxyManager(50)

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("no proxy file, using direct connections", "path", path)
			return pm, nil
		}
		return nil, fmt.Errorf("open proxy file: %w", err)
	}
	defer file.Close()

	if err := pm.LoadProxiesFromFile(file); err != nil {
		return nil, fmt.Errorf("load proxies: %w", err)
	}

	return pm, nil
}

// newLogHandler builds the slog handler for MONITOR_LOG_FORMAT.
//
// Text is the default because both places this runs by hand want it: a terminal
// during development, and journalctl on an LXC, where JSON would land as one
// opaque MESSAGE field and duplicate the timestamp and priority journald
// already records. Set json where something parses the logs.
func newLogHandler(w io.Writer, format, level string) (slog.Handler, error) {
	var lvl slog.Level
	// UnmarshalText takes the same names slog prints, and is case-insensitive.
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("unknown log level %q: want debug, info, warn, or error", level)
	}

	opts := &slog.HandlerOptions{Level: lvl}

	switch format {
	case "text":
		return slog.NewTextHandler(w, opts), nil
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	default:
		return nil, fmt.Errorf("unknown log format %q: want text or json", format)
	}
}

func main() {
	// Set before anything else logs. Packages that build loggers in their own
	// init() would capture the pre-default handler, so they call slog directly.
	// This is the default format rather than the configured one: config.Load's
	// own failures have to be loggable, and run() swaps in the configured
	// handler as soon as it knows.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Kept free of defers so the exit path below is the only one: gocritic's
	// exitAfterDefer catches an exit that would skip a deferred Close.
	if err := run(); err != nil {
		// --help already printed usage; it is a successful exit, not a crash.
		if errors.Is(err, conf.ErrHelpWanted) {
			return
		}
		slog.Error("shutting down", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Swap the bootstrap handler for the configured one before anything routine
	// is logged, so a json consumer never has to skip a stray text line.
	handler, err := newLogHandler(os.Stderr, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(handler))

	slog.Info("welcome to the shopify monitor", "build", config.Build)

	// Initialize the proxy manager once and share it. Proxies are optional: an
	// empty manager makes nextClient fall back to a direct connection, so a
	// missing file is a first-run default rather than an error.
	shopifyProxyBroker, err := loadProxies(cfg.ProxiesFile)
	if err != nil {
		return err
	}

	// One registry for the process: a webhook's limits are imposed per webhook,
	// so two stores naming the same one have to share the queue and the pacing
	// that keeps them inside it.
	destinations := webhook.NewRegistry(webhook.Options{
		MinInterval: time.Duration(cfg.WebhookMinInterval) * time.Millisecond,
		Logger:      slog.Default(),
	})

	// Without this the process could only ever be killed, and alerts detected in
	// the final cycle would go out nowhere.
	ctx, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()

	if err := startMonitorService(ctx, &wg, cfg, shopifyProxyBroker, destinations); err != nil {
		return err
	}

	wg.Wait()

	// The events still queued are the most recently detected ones, so they are
	// worth a bounded wait. Bounded because an unresponsive webhook must not be
	// able to keep the process alive.
	drain, cancelDrain := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancelDrain()

	if err := destinations.Shutdown(drain); err != nil {
		slog.Warn("gave up delivering some alerts before exit", "err", err)
	}

	return nil
}
