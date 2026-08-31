package main

import (
	"strings"
	"testing"
	"time"
)

const (
	testDelay = 2500 * time.Millisecond
	testMax   = 6000
)

func parse(t *testing.T, csv string) ([]storeConfig, error) {
	t.Helper()
	return parseStores(strings.NewReader(csv), "websites.csv", testDelay, testMax)
}

// @spec CFG-STORES-001, CFG-STORES-003
func TestParseStoresLocatesColumnsByName(t *testing.T) {
	// Deliberately reordered, with the optional columns absent.
	got, err := parse(t, "webhook,url\nhttps://hook.test/a,https://store.test\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d stores, want 1", len(got))
	}
	if got[0].URL != "https://store.test" || got[0].WebhookURL != "https://hook.test/a" {
		t.Errorf("columns resolved to url=%q webhook=%q", got[0].URL, got[0].WebhookURL)
	}
	if got[0].Delay != testDelay || got[0].MaxProducts != testMax {
		t.Errorf("absent optional columns should take the defaults, got delay=%v max=%d", got[0].Delay, got[0].MaxProducts)
	}
}

// @spec CFG-STORES-004
func TestParseStoresBlankOptionalCellTakesTheDefault(t *testing.T) {
	got, err := parse(t, "url,webhook,delay,max_products\nhttps://store.test,https://hook.test/a,,\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].Delay != testDelay || got[0].MaxProducts != testMax {
		t.Errorf("blank cells should take the defaults, got delay=%v max=%d", got[0].Delay, got[0].MaxProducts)
	}
}

func TestParseStoresReadsOverrides(t *testing.T) {
	got, err := parse(t, "url,webhook,delay,max_products\nhttps://store.test,https://hook.test/a,60000,200\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].Delay != 60*time.Second {
		t.Errorf("delay = %v, want 60s", got[0].Delay)
	}
	if got[0].MaxProducts != 200 {
		t.Errorf("max_products = %d, want 200", got[0].MaxProducts)
	}
}

// A store that finds restocks and has nowhere to report them looks exactly like
// a healthy one, so it must not be allowed to start.
//
// @spec CFG-STORES-002, CFG-VALID-001, CFG-VALID-002, CFG-VALID-003, CFG-VALID-004, CFG-VALID-005, CFG-VALID-006, CFG-VALID-007
func TestParseStoresRefusesUnworkableConfiguration(t *testing.T) {
	tests := []struct {
		name string
		csv  string
		want string
	}{
		{"no url column", "webhook\nhttps://hook.test/a\n", "url"},
		{"no webhook column", "url\nhttps://store.test\n", "webhook"},
		{"blank url", "url,webhook\n,https://hook.test/a\n", "url"},
		{"blank webhook", "url,webhook\nhttps://store.test,\n", "webhook"},
		{"url with no scheme", "url,webhook\nstore.test,https://hook.test/a\n", "url"},
		{"webhook with no scheme", "url,webhook\nhttps://store.test,hook.test/a\n", "webhook"},
		{"url with a non-http scheme", "url,webhook\nftp://store.test,https://hook.test/a\n", "url"},
		{"delay not a number", "url,webhook,delay\nhttps://store.test,https://hook.test/a,fast\n", "delay"},
		{"delay zero", "url,webhook,delay\nhttps://store.test,https://hook.test/a,0\n", "delay"},
		{"delay negative", "url,webhook,delay\nhttps://store.test,https://hook.test/a,-1\n", "delay"},
		{"max_products not a number", "url,webhook,max_products\nhttps://store.test,https://hook.test/a,lots\n", "max_products"},
		{"max_products negative", "url,webhook,max_products\nhttps://store.test,https://hook.test/a,-5\n", "max_products"},
		{"no store rows", "url,webhook\n", "nothing to monitor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, tt.csv)
			if err == nil {
				t.Fatal("expected this configuration to be refused")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// @spec CFG-VALID-008
func TestParseStoresReportsTheEditorsLineNumber(t *testing.T) {
	csv := "url,webhook\n" +
		"https://a.test,https://hook.test/a\n" +
		"https://b.test,https://hook.test/b\n" +
		"https://c.test,\n" // line 4 in an editor
	_, err := parse(t, csv)
	if err == nil {
		t.Fatal("expected the blank webhook to be refused")
	}
	if !strings.Contains(err.Error(), "line 4") {
		t.Errorf("error %q should name line 4", err)
	}
}
