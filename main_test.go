package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogHandler(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		level   string
		wantErr bool
	}{
		{name: "text info", format: "text", level: "info"},
		{name: "json debug", format: "json", level: "debug"},
		{name: "level is case insensitive", format: "text", level: "DEBUG"},
		{name: "warn", format: "text", level: "warn"},
		{name: "error", format: "text", level: "error"},
		{name: "bad format", format: "yaml", level: "info", wantErr: true},
		{name: "bad level", format: "text", level: "verbose", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := newLogHandler(&bytes.Buffer{}, tt.format, tt.level)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h == nil {
				t.Fatal("handler is nil")
			}
		})
	}
}

// The point of moving the per-poll line to Debug: at the default level it must
// not reach the log at all, or the noise reduction is imaginary.
func TestLevelFiltersDebug(t *testing.T) {
	tests := []struct {
		level     string
		wantDebug bool
	}{
		{"info", false},
		{"debug", true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			h, err := newLogHandler(&buf, "text", tt.level)
			if err != nil {
				t.Fatal(err)
			}
			log := slog.New(h)

			log.Debug("refreshing")
			log.Info("new variant")

			out := buf.String()
			if got := strings.Contains(out, "refreshing"); got != tt.wantDebug {
				t.Errorf("debug line present = %v, want %v (output: %q)", got, tt.wantDebug, out)
			}
			if !strings.Contains(out, "new variant") {
				t.Errorf("info line missing at level %q: %q", tt.level, out)
			}
		})
	}
}
