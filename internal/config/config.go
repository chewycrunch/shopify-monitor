package config

import (
	"errors"
	"fmt"

	"github.com/ardanlabs/conf/v3"
)

const prefix = "MONITOR"

// Build identifies the running binary. Overwritten at link time by the
// Dockerfile's -X flag; "develop" is what a `go build` with no ldflags reports.
var Build = "develop"

// Config holds the runtime settings for the monitor.
//
// The file paths are deliberately relative. The Dockerfile's WORKDIR is /app,
// so mounting the host's config directory at /app/config makes these defaults
// resolve in a container exactly as they do for `go run main.go` from the repo
// root — one default, both environments, no override needed. Running the built
// binary from anywhere else is what the env vars are for.
type Config struct {
	Delay        int    `conf:"default:2500,help:pause between polling cycles in milliseconds"`
	WebsitesFile string `conf:"default:config/websites.csv,help:CSV of store URL and webhook URL pairs"`
	ProxiesFile  string `conf:"default:config/proxies.txt,help:one proxy per line with optional basic auth; direct connections if absent"`
	LogFormat    string `conf:"default:text,help:log output format: text or json"`
	LogLevel     string `conf:"default:info,help:debug info warn or error; debug adds a line per poll"`
	PageWorkers  int    `conf:"default:5,help:catalog pages fetched at once; each goes through its own proxy"`
}

// Load resolves the monitor configuration from environment variables and flags.
//
// ErrHelpWanted is not a failure: conf returns the rendered usage text alongside
// it, and printing that is the whole point of --help. Callers get it back
// unwrapped so they can exit zero rather than reporting it as a crash.
func Load() (Config, error) {
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
