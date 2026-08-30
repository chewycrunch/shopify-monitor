package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
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

	// Columns are located by header name rather than position, so an existing
	// two-column file keeps working when a third is added and the order of
	// columns in the file does not matter.
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read websites header: %w", err)
	}

	col := make(map[string]int, len(header))
	for i, name := range header {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}

	urlCol, ok := col["url"]
	if !ok {
		return fmt.Errorf("%s: no 'url' column in header", cfg.WebsitesFile)
	}
	webhookCol, ok := col["webhook"]
	if !ok {
		return fmt.Errorf("%s: no 'webhook' column in header", cfg.WebsitesFile)
	}
	// Optional: absent means every store uses the global default.
	delayCol, hasDelay := col["delay"]
	maxProductsCol, hasMaxProducts := col["max_products"]

	// Process each website. line counts the header, so it matches what an
	// editor shows.
	for line := 2; ; line++ {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read websites record: %w", err)
		}

		if urlCol >= len(record) || webhookCol >= len(record) {
			return fmt.Errorf("%s line %d: missing url or webhook", cfg.WebsitesFile, line)
		}
		websiteURL := record[urlCol]
		webhookURL := record[webhookCol]

		// Per-store override. Stores that restock fast are worth polling hard;
		// quiet ones are not worth the proxy bandwidth of a full crawl every
		// few seconds. Blank falls back to the global default.
		delay := time.Duration(cfg.Delay) * time.Millisecond
		if hasDelay && delayCol < len(record) {
			if raw := strings.TrimSpace(record[delayCol]); raw != "" {
				ms, err := strconv.Atoi(raw)
				if err != nil || ms <= 0 {
					return fmt.Errorf("%s line %d: delay %q must be a positive number of milliseconds", cfg.WebsitesFile, line, raw)
				}
				delay = time.Duration(ms) * time.Millisecond
			}
		}

		// Per-store crawl depth. Live inventory sits at the front of a
		// catalogue, so a large store is read only as deep as it is worth
		// reading; 0 means the whole reachable catalogue.
		maxProducts := cfg.MaxProducts
		if hasMaxProducts && maxProductsCol < len(record) {
			if raw := strings.TrimSpace(record[maxProductsCol]); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 0 {
					return fmt.Errorf("%s line %d: max_products %q must be zero or a positive number of products", cfg.WebsitesFile, line, raw)
				}
				maxProducts = n
			}
		}

		// Add to the wait group
		wg.Add(1)

		// Start a goroutine for each website
		go func() {
			defer wg.Done()
			m := monitor.NewMonitor(websiteURL, webhookURL, proxyManager, cfg.PageWorkers, maxProducts)

			// Retry rather than give up: the baseline fetch goes through the
			// same rotating proxies as every later one, so a single timeout
			// here would otherwise drop this site for the life of the process.
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
				case <-time.After(delay):
				}
			}

			if err := m.StartWatching(ctx, delay); err != nil {
				slog.Error("stopped watching", "site", m.Url, "err", err)
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

	if err := startMonitorService(context.Background(), &wg, cfg, shopifyProxyBroker); err != nil {
		return err
	}

	wg.Wait()

	return nil
}
