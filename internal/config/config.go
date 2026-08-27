package config

import "github.com/ardanlabs/conf/v3"

const prefix = "MONITOR"

// Build identifies the running binary. Overwritten at link time by the
// Dockerfile's -X flag; "develop" is what a `go build` with no ldflags reports.
var Build = "develop"

// Config holds the runtime settings for the monitor.
type Config struct {
	Delay int `conf:"default:2500,help:pause between polling cycles in milliseconds"`
}

// Load resolves the monitor configuration from environment variables and flags.
func Load() (Config, error) {
	var cfg Config
	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}
