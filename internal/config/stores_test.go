package config

import (
	"strings"
	"testing"
	"time"
)

const (
	testDelay = 2500 * time.Millisecond
	testMax   = 6000
)

func parse(t *testing.T, doc string) ([]Store, error) {
	t.Helper()
	cfg := Config{
		StoresFile:         "stores.toml",
		DefaultDelay:       int(testDelay / time.Millisecond),
		DefaultMaxProducts: testMax,
	}
	return cfg.ParseStores(strings.NewReader(doc))
}

// @spec CFG-STORES-005, CFG-STORES-003
func TestParseStoresReadsEveryStoreTable(t *testing.T) {
	got, err := parse(t, `
[[store]]
url = "https://a.test"
webhooks = ["https://hook.test/a"]

[[store]]
url = "https://b.test"
webhooks = ["https://hook.test/b1", "https://hook.test/b2"]
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d stores, want 2", len(got))
	}

	if got[0].URL != "https://a.test" {
		t.Errorf("first store url = %q", got[0].URL)
	}
	if len(got[0].WebhookURLs) != 1 || got[0].WebhookURLs[0] != "https://hook.test/a" {
		t.Errorf("first store webhooks = %v, want one entry", got[0].WebhookURLs)
	}
	if len(got[1].WebhookURLs) != 2 {
		t.Errorf("second store webhooks = %v, want both entries", got[1].WebhookURLs)
	}

	// Neither store sets a delay or a cap, so both take the global defaults.
	for i, s := range got {
		if s.Delay != testDelay {
			t.Errorf("store %d delay = %s, want the default %s", i, s.Delay, testDelay)
		}
		if s.MaxProducts != testMax {
			t.Errorf("store %d max_products = %d, want the default %d", i, s.MaxProducts, testMax)
		}
	}
}

// @spec CFG-STORES-003
func TestParseStoresReadsPerStoreOverrides(t *testing.T) {
	got, err := parse(t, `
[[store]]
url = "https://a.test"
webhooks = ["https://hook.test/a"]
delay = 60000
max_products = 200
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].Delay != 60*time.Second {
		t.Errorf("delay = %s, want 60s", got[0].Delay)
	}
	if got[0].MaxProducts != 200 {
		t.Errorf("max_products = %d, want 200", got[0].MaxProducts)
	}
}

// Pausing a store is a routine act, and being able to keep it in the file
// rather than delete it is a reason this format was chosen.
//
// @spec CFG-STORES-005
func TestParseStoresIgnoresACommentedOutStore(t *testing.T) {
	got, err := parse(t, `
[[store]]
url = "https://a.test"
webhooks = ["https://hook.test/a"]

# Paused for the season.
# [[store]]
# url = "https://b.test"
# webhooks = ["https://hook.test/b"]
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("read %d stores, want only the one that is not commented out", len(got))
	}
}

// Zero means the whole catalogue, so it has to be distinguishable from a field
// the operator never set. Decoding into a plain int would make every omission
// read as an explicit zero.
//
// @spec CFG-VALID-010, CFG-STORES-003
func TestParseStoresTellsAnExplicitZeroFromAnOmittedField(t *testing.T) {
	got, err := parse(t, `
[[store]]
url = "https://explicit.test"
webhooks = ["https://hook.test/a"]
max_products = 0

[[store]]
url = "https://omitted.test"
webhooks = ["https://hook.test/b"]
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].MaxProducts != 0 {
		t.Errorf("an explicit max_products of 0 became %d; zero means the whole catalogue", got[0].MaxProducts)
	}
	if got[1].MaxProducts != testMax {
		t.Errorf("an omitted max_products became %d, want the default %d", got[1].MaxProducts, testMax)
	}
}

// @spec CFG-STORES-006
func TestParseStoresRecordsTheLineEachStoreBeginsOn(t *testing.T) {
	doc := "[[store]]\n" + // 1
		"url = \"https://a.test\"\n" + // 2
		"webhooks = [\"https://hook.test/a\"]\n" + // 3
		"\n" + // 4
		"[[store]]\n" + // 5
		"url = \"https://b.test\"\n" + // 6
		"webhooks = [\"https://hook.test/b\"]\n" // 7

	got, err := parse(t, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, want := range []int{1, 5} {
		if got[i].Line != want {
			t.Errorf("store %d recorded line %d, want %d", i, got[i].Line, want)
		}
	}
}

// @spec CFG-VALID-001, CFG-VALID-002, CFG-VALID-003, CFG-VALID-005, CFG-VALID-006, CFG-VALID-007, CFG-VALID-011, CFG-STORES-007
func TestParseStoresRefusesUnworkableConfiguration(t *testing.T) {
	const store = "[[store]]\nurl = \"https://store.test\"\nwebhooks = [\"https://hook.test/a\"]\n"

	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"no url", "[[store]]\nwebhooks = [\"https://hook.test/a\"]\n", "url"},
		{"empty url", "[[store]]\nurl = \"\"\nwebhooks = [\"https://hook.test/a\"]\n", "url"},
		{"url with no scheme", "[[store]]\nurl = \"store.test\"\nwebhooks = [\"https://hook.test/a\"]\n", "url"},
		{"url with a non-http scheme", "[[store]]\nurl = \"ftp://store.test\"\nwebhooks = [\"https://hook.test/a\"]\n", "url"},
		{"no webhooks", "[[store]]\nurl = \"https://store.test\"\n", "webhooks"},
		{"empty webhooks list", "[[store]]\nurl = \"https://store.test\"\nwebhooks = []\n", "webhooks"},
		{"delay of the wrong type", store + "delay = \"fast\"\n", "delay"},
		{"delay zero", store + "delay = 0\n", "delay"},
		{"delay negative", store + "delay = -1\n", "delay"},
		{"max_products of the wrong type", store + "max_products = \"lots\"\n", "max_products"},
		{"max_products negative", store + "max_products = -5\n", "max_products"},
		{"no stores at all", "# nothing here yet\n", "nothing to monitor"},
		{"an unrecognised field", store + "delayy = 500\n", "delayy"},
		{"a mistyped table name", "[[stores]]\nurl = \"https://store.test\"\nwebhooks = [\"https://hook.test/a\"]\n", "stores"},
		{"not TOML at all", "this is not a config file\n", "stores.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, tt.doc)
			if err == nil {
				t.Fatal("expected this configuration to be refused")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// Keeping the store on its valid entries would mean alerting fewer places than
// the operator asked for, with a log line as the only record.
//
// @spec CFG-VALID-004
func TestParseStoresRefusesAStoreWithOneUnusableWebhook(t *testing.T) {
	_, err := parse(t, `
[[store]]
url = "https://store.test"
webhooks = ["https://hook.test/good", "hook.test/bad"]
`)
	if err == nil {
		t.Fatal("a store with one unusable webhook was accepted")
	}
	if !strings.Contains(err.Error(), "hook.test/bad") {
		t.Errorf("error %q does not name the offending entry", err)
	}
}

// @spec CFG-VALID-008
func TestParseStoresReportsTheEditorsLineNumber(t *testing.T) {
	doc := "[[store]]\n" + // 1
		"url = \"https://a.test\"\n" + // 2
		"webhooks = [\"https://hook.test/a\"]\n" + // 3
		"\n" + // 4
		"[[store]]\n" + // 5
		"url = \"https://b.test\"\n" + // 6
		"webhooks = [\"nope\"]\n" // 7

	_, err := parse(t, doc)
	if err == nil {
		t.Fatal("expected the unusable webhook to be refused")
	}
	if !strings.Contains(err.Error(), "line 5") {
		t.Errorf("error %q should name line 5, where the offending store's table begins", err)
	}
}

// The retired format has to be named for what it is. An operator converting
// their file will point the monitor at the old one once, and a complaint about
// an unexpected character on line 1 does not tell them what happened.
//
// @spec CFG-VALID-012
func TestParseStoresNamesTheRetiredCommaSeparatedFormat(t *testing.T) {
	doc := "url,webhook,delay,max_products\n" +
		"https://kith.com,https://discord.com/api/webhooks/1/x,300000,6000\n"

	_, err := parse(t, doc)
	if err == nil {
		t.Fatal("the retired comma-separated format was accepted")
	}
	// Reported as the format it is, in preference to a parse fault.
	if !strings.Contains(err.Error(), "comma-separated") {
		t.Errorf("error %q should name the retired comma-separated format", err)
	}
}

// Shape is checkable offline; reachability is not, and a store being briefly
// down must not stop the monitor booting.
//
// @spec CFG-VALID-009
func TestParseStoresContactsNothing(t *testing.T) {
	// Port 1 has nothing listening on it, so any attempt to reach either URL
	// would fail or hang rather than pass.
	got, err := parse(t, `
[[store]]
url = "https://127.0.0.1:1"
webhooks = ["https://127.0.0.1:1/hook"]
`)
	if err != nil {
		t.Fatalf("validation reached for the network: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("read %d stores, want 1", len(got))
	}
}

// An unrecognised MONITOR_* variable is ignored, so a former name would
// otherwise leave the setting at its built-in value while looking configured.
func TestLoadRefusesRenamedSettings(t *testing.T) {
	for old, current := range aliases {
		t.Run(old, func(t *testing.T) {
			t.Setenv(old, "1")

			_, err := Load()
			if err == nil {
				t.Fatalf("%s should be refused, not ignored", old)
			}
			if !strings.Contains(err.Error(), current) {
				t.Errorf("error %q should name the current setting %q", err, current)
			}
		})
	}
}
