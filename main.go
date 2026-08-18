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

	"github.com/chewycrunch/shopify-monitor/internal/config"
	"github.com/chewycrunch/shopify-monitor/internal/monitor"
	"github.com/chewycrunch/shopify-monitor/internal/proxy"
)

// Fetch variants and load them into the map

// Compare variants to the map, see if any availability has changed, or new variants exist
// If so, send a webhook to the webhook URL
var wg sync.WaitGroup

// Spawns one monitor goroutine per website in config/websites.csv and returns;
// the goroutines it starts are tracked on wg.
func startMonitorService(ctx context.Context, wg *sync.WaitGroup, cfg config.Config, proxyManager *proxy.ProxyManager) error {
	file, err := os.Open("config/websites.csv")
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

// Read config/websites.csv
func main() {
	// Set before anything else logs. Packages that build loggers in their own
	// init() would capture the pre-default handler, so they call slog directly.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	// Kept free of defers so the exit path below is the only one: gocritic's
	// exitAfterDefer catches an exit that would skip a deferred Close.
	if err := run(); err != nil {
		slog.Error("shutting down", "err", err)
		os.Exit(1)
	}
}

func run() error {
	slog.Info("welcome to the shopify monitor")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Initialize the proxy manager once and share it
	proxyFile, err := os.Open("config/proxies.txt")
	if err != nil {
		return fmt.Errorf("open proxy file: %w", err)
	}
	defer proxyFile.Close()

	shopifyProxyBroker := proxy.NewProxyManager(50)
	if err := shopifyProxyBroker.LoadProxiesFromFile(proxyFile); err != nil {
		return fmt.Errorf("load proxies: %w", err)
	}

	if err := startMonitorService(context.Background(), &wg, cfg, shopifyProxyBroker); err != nil {
		return err
	}

	wg.Wait()

	return nil
}
