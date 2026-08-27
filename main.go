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

func main() {
	// Set before anything else logs. Packages that build loggers in their own
	// init() would capture the pre-default handler, so they call slog directly.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

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
	slog.Info("welcome to the shopify monitor", "build", config.Build)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

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
