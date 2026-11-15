package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Store represents one monitored shopify store. Per-store settings are
// resolved, and fallback values are used from global defaults.
type Store struct {
	URL         string
	WebhookURLs []string
	Delay       time.Duration
	MaxProducts int

	// Line is the line in the stores file on which this store's table begins.
	// Validation that cannot happen while parsing — recognising a webhook's
	// destination needs the segment that delivers to it — still has to tell the
	// operator which part of the file to fix.
	Line int
}

// storesFile is the shape of the stores file.
//
// Delay and MaxProducts are pointers so that a field the operator omitted is
// distinguishable from one they set to zero. The distinction is load-bearing in
// both directions: zero means the whole catalogue for max_products, and is
// refused outright for delay, while omission means neither.
type storesFile struct {
	Store []storeEntry `toml:"store"`
}

type storeEntry struct {
	URL         string   `toml:"url"`
	Webhooks    []string `toml:"webhooks"`
	Delay       *int     `toml:"delay"`
	MaxProducts *int     `toml:"max_products"`
}

// ParseStores reads the stores file, resolving each store's settings against
// this Config's global defaults.
//
// Anything that cannot possibly work is refused here rather than at the point
// of use. A store with no destination for its alerts polls, diffs, and finds
// restocks exactly like a healthy one — it just reports them nowhere — so the
// only moment that failure is visible is before it starts.
//
// Validation stays shallow on purpose: shape is checkable offline, reachability
// is not, and a store being briefly down should not stop the monitor booting.
//
// The order of the checks below is deliberate, because several of them match
// the same file and the first to fire is the one the operator is told about.
//
// @spec CFG-STORES-003, CFG-STORES-005, CFG-STORES-006, CFG-STORES-007, CFG-VALID-001, CFG-VALID-002, CFG-VALID-003, CFG-VALID-004, CFG-VALID-005, CFG-VALID-006, CFG-VALID-007, CFG-VALID-008, CFG-VALID-009, CFG-VALID-010, CFG-VALID-011, CFG-VALID-012
func (c Config) ParseStores(r io.Reader) ([]Store, error) {
	path := c.StoresFile

	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	content := string(raw)

	// Before parsing: a file in the retired format is not a malformed TOML
	// file, it is a different file, and saying so is the whole point.
	if looksLikeRetiredFormat(content) {
		return nil, fmt.Errorf("%s: this is the retired comma-separated format; the stores file is now TOML, with a [[store]] table per store", path)
	}

	var parsed storesFile
	md, err := toml.Decode(content, &parsed)
	if err != nil {
		// A file that will not parse at all is reported as that. A file that
		// parses but carries a value of the wrong type is reported against the
		// field that holds it, which the decoder names.
		var parseErr toml.ParseError
		if errors.As(err, &parseErr) {
			return nil, fmt.Errorf("%s is not valid TOML: %w", path, err)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	// Before drawing any conclusion from what is absent: a name the decoder did
	// not recognise is more likely a typo than an intention, and reporting it is
	// the difference between naming the cause and describing the symptom. A
	// mistyped [[stores]] is the case this ordering exists for — checked later,
	// it would be reported as a file with no stores in it.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("%s: unrecognised %s; expected a [[store]] table with url, webhooks, delay, and max_products", path, undecoded[0])
	}

	lines := storeLines(content)

	stores := make([]Store, 0, len(parsed.Store))
	for i, entry := range parsed.Store {
		line := 0
		if i < len(lines) {
			line = lines[i]
		}

		store := Store{
			URL:         strings.TrimSpace(entry.URL),
			WebhookURLs: entry.Webhooks,
			Delay:       time.Duration(c.DefaultDelay) * time.Millisecond,
			MaxProducts: c.DefaultMaxProducts,
			Line:        line,
		}

		if err := requireHTTPURL(store.URL, "url", path, line); err != nil {
			return nil, err
		}

		if len(store.WebhookURLs) == 0 {
			return nil, fmt.Errorf("%s line %d: webhooks is empty; a store with nowhere to report its alerts looks healthy while reporting nothing", path, line)
		}
		// Refused even where the store's other entries are usable: keeping it on
		// those would alert fewer places than asked for, with nothing to notice.
		for _, hook := range store.WebhookURLs {
			if err := requireHTTPURL(strings.TrimSpace(hook), "webhooks entry", path, line); err != nil {
				return nil, err
			}
		}

		if entry.Delay != nil {
			if *entry.Delay <= 0 {
				return nil, fmt.Errorf("%s line %d: delay %d must be a positive whole number of milliseconds", path, line, *entry.Delay)
			}
			store.Delay = time.Duration(*entry.Delay) * time.Millisecond
		}

		if entry.MaxProducts != nil {
			if *entry.MaxProducts < 0 {
				return nil, fmt.Errorf("%s line %d: max_products %d must be zero or a positive whole number", path, line, *entry.MaxProducts)
			}
			store.MaxProducts = *entry.MaxProducts
		}

		stores = append(stores, store)
	}

	// Last, so that a file which failed for any nameable reason has already said
	// so. Starting with no stores means exiting successfully and immediately,
	// which under a restart policy is indistinguishable from a crash loop.
	if len(stores) == 0 {
		return nil, fmt.Errorf("%s: nothing to monitor — the file has no [[store]] tables", path)
	}

	return stores, nil
}

// looksLikeRetiredFormat reports whether the file's first meaningful line is a
// comma-separated header rather than TOML.
//
// Recognising the retired format is not supporting it: the point is to answer
// "this is the old comma-separated file" instead of complaining about an
// unexpected character on line 1.
//
// @spec CFG-VALID-012
func looksLikeRetiredFormat(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// A TOML line may well hold commas — inside an array — but it also
		// holds an assignment or a table header, and a bare header row does
		// not.
		if strings.ContainsAny(line, "=[") || !strings.Contains(line, ",") {
			return false
		}

		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			return false
		}
		for _, f := range fields {
			if !isFieldName(strings.TrimSpace(f)) {
				return false
			}
		}
		return true
	}

	return false
}

// isFieldName reports whether s is shaped like a column name, so that a line of
// prose containing commas is not mistaken for a header row.
func isFieldName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' {
			return false
		}
	}
	return true
}

// storeLines maps each [[store]] table to the line it begins on, so a store
// refused after decoding can still name a line an editor agrees with.
//
// The decoder reports positions for syntax faults but not for the tables it
// successfully decoded, so the positions are found by scanning.
//
// @spec CFG-STORES-006, CFG-VALID-008
func storeLines(content string) []int {
	var lines []int

	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[[") || !strings.HasSuffix(line, "]]") {
			continue
		}
		if strings.TrimSpace(strings.Trim(line, "[]")) != "store" {
			continue
		}
		// Counted from one, so the number matches what an editor shows.
		lines = append(lines, i+1)
	}

	return lines
}

// requireHTTPURL rejects anything that cannot be requested. A bare host is the
// common mistake: it parses without error but has no scheme, and every request
// built from it fails at the transport with "unsupported protocol scheme".
func requireHTTPURL(value, field, path string, line int) error {
	if value == "" {
		return fmt.Errorf("%s line %d: %s is blank", path, line, field)
	}

	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s line %d: %s %q is not a valid URL: %w", path, line, field, value, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s line %d: %s %q must be an absolute http or https URL", path, line, field, value)
	}
	if u.Host == "" {
		return fmt.Errorf("%s line %d: %s %q has no host", path, line, field, value)
	}

	return nil
}
