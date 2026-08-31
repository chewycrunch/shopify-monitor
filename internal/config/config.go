package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/ardanlabs/conf/v3"
)

const prefix = "MONITOR"

// Build identifies the running binary. Overwritten at link time by the
// Dockerfile's -X flag; "develop" is what a `go build` with no ldflags reports.
var Build = "develop"

// Config holds the runtime settings for the monitor.
type Config struct {
	// Per-store defaults. Can be overwritten by website
	DefaultDelay       int `conf:"default:5000,help:rest between crawls in milliseconds for stores that set no delay of their own"`
	DefaultMaxProducts int `conf:"default:6000,help:newest products crawled for stores that set no max_products of their own; 0 for the whole reachable catalogue"`

	// Data files
	WebsitesFile string `conf:"default:config/websites.csv,help:CSV of store URL and webhook URL pairs"`
	ProxiesFile  string `conf:"default:config/proxies.txt,help:one proxy per line with optional basic auth; direct connections if absent"`

	// Internal stuff
	LogFormat string `conf:"default:text,help:log output format: text or json"`
	LogLevel  string `conf:"default:info,help:debug info warn or error; debug adds a line per poll"`

	// Application performance
	PageWorkers int `conf:"default:5,help:catalog pages fetched at once; each goes through its own proxy"`
}

// aliases maps former environment variable names to current ones.
var aliases = map[string]string{
	"MONITOR_DELAY":        "MONITOR_DEFAULT_DELAY",
	"MONITOR_MAX_PRODUCTS": "MONITOR_DEFAULT_MAX_PRODUCTS",
}

// Load resolves the monitor configuration from environment variables and flags.
func Load() (Config, error) {
	for old, current := range aliases {
		if os.Getenv(old) != "" {
			return Config{}, fmt.Errorf("%s has been renamed to %s; the old name is no longer read", old, current)
		}
	}

	var cfg Config
	help, err := conf.Parse(prefix, &cfg)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			return Config{}, err
		}
		return Config{}, err
	}
	return cfg, nil
}
