package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ardanlabs/conf/v3"
	"github.com/chewycrunch/shopify-monitor/internal/config"
	"github.com/chewycrunch/shopify-monitor/internal/monitor"
	"github.com/chewycrunch/shopify-monitor/internal/proxy"
)

// Fetch variants and load them into the map

// Compare variants to the map, see if any availability has changed, or new variants exist
// If so, send a webhook to the webhook URL
var wg sync.WaitGroup

// Spawns one monitor goroutine per website in cfg.WebsitesFile and returns;
// the goroutines it starts are tracked on wg.
func startMonitorService(ctx context.Context, wg *sync.WaitGroup, cfg config.Config, proxyManager *proxy.ProxyManager) error {
	file, err := os.Open(cfg.WebsitesFile)
	if err != nil {
		return fmt.Errorf("open websites file: %w", err)
	}
	defer file.Close()

	// Parse the CSV file
	reader := csv.NewReader(file)
	_, err = reader.Read() // Disregard the header line
	if err != nil {
		return fmt.Errorf("read websites header: %w", err)
	}

	// Process each website
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read websites record: %w", err)
		}

		websiteURL := record[0]
		webhookURL := record[1]

		// Add to the wait group
		wg.Add(1)

		// Start a goroutine for each website
		go func() {
			defer wg.Done()
			m := monitor.NewMonitor(websiteURL, webhookURL, proxyManager)
			if err := m.InitializeVariants(ctx); err != nil {
				slog.Error("failed to initialize variants", "site", websiteURL, "err", err)
				return
			}
			if err := m.StartWatching(ctx, time.Duration(cfg.Delay)*time.Millisecond); err != nil {
				slog.Error("stopped watching", "site", websiteURL, "err", err)
			}
		}()

	}

	return nil
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
func newLogHandler(w io.Writer, format string) (slog.Handler, error) {
	switch format {
	case "text":
		return slog.NewTextHandler(w, nil), nil
	case "json":
		return slog.NewJSONHandler(w, nil), nil
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
	handler, err := newLogHandler(os.Stderr, cfg.LogFormat)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(handler))

	slog.Info("welcome to the shopify monitor", "build", config.Build)

	// Initialize the proxy manager once and share it. Proxies are optional: an
	// empty manager makes rotateClient fall back to a direct connection, so a
	// missing file is a first-run default rather than an error.
	shopifyProxyBroker, err := loadProxies(cfg.ProxiesFile)
	if err != nil {
		return err
	}

	if err := startMonitorService(context.Background(), &wg, cfg, shopifyProxyBroker); err != nil {
		return err
	}

	wg.Wait()

	return nil
}
