package config

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Store is one monitored store, with its per-store settings already resolved
// against the global defaults.
type Store struct {
	URL         string
	WebhookURL  string
	Delay       time.Duration
	MaxProducts int
}

// ParseStores reads the stores file named by this Config, resolving each
// store's settings against the Config's defaults.
//
// Anything that cannot possibly work is refused here rather than at the point
// of use. A store with no destination for its alerts polls, diffs, and finds
// restocks exactly like a healthy one — it just reports them nowhere — so the
// only moment that failure is visible is before it starts.
//
// Validation stays shallow on purpose: shape is checkable offline, reachability
// is not, and a store being briefly down should not stop the monitor booting.
//
// @spec CFG-STORES-001, CFG-STORES-002, CFG-STORES-003, CFG-STORES-004, CFG-VALID-001, CFG-VALID-002, CFG-VALID-003, CFG-VALID-004, CFG-VALID-005, CFG-VALID-006, CFG-VALID-007, CFG-VALID-008, CFG-VALID-009
func (c Config) ParseStores(r io.Reader) ([]Store, error) {
	path := c.WebsitesFile
	defaultDelay := time.Duration(c.Delay) * time.Millisecond
	defaultMaxProducts := c.MaxProducts

	reader := csv.NewReader(r)
	// Rows may be shorter than the header when trailing optional columns are
	// left off entirely.
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read %s header: %w", path, err)
	}

	col := make(map[string]int, len(header))
	for i, name := range header {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}

	urlCol, ok := col["url"]
	if !ok {
		return nil, fmt.Errorf("%s: header has no 'url' column", path)
	}
	webhookCol, ok := col["webhook"]
	if !ok {
		return nil, fmt.Errorf("%s: header has no 'webhook' column", path)
	}
	delayCol, hasDelay := col["delay"]
	maxProductsCol, hasMaxProducts := col["max_products"]

	// cell returns a trimmed value, or "" when the row stops short of it.
	cell := func(record []string, i int) string {
		if i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}

	var stores []Store

	// The header is line 1, so rows start at 2 and the number matches an editor.
	for line := 2; ; line++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}

		// A wholly blank line is padding, not a store.
		if isBlank(record) {
			continue
		}

		store := Store{
			URL:         cell(record, urlCol),
			WebhookURL:  cell(record, webhookCol),
			Delay:       defaultDelay,
			MaxProducts: defaultMaxProducts,
		}

		if err := requireHTTPURL(store.URL, "url", path, line); err != nil {
			return nil, err
		}
		if err := requireHTTPURL(store.WebhookURL, "webhook", path, line); err != nil {
			return nil, err
		}

		if hasDelay {
			if raw := cell(record, delayCol); raw != "" {
				ms, err := strconv.Atoi(raw)
				if err != nil || ms <= 0 {
					return nil, fmt.Errorf("%s line %d: delay %q must be a positive whole number of milliseconds", path, line, raw)
				}
				store.Delay = time.Duration(ms) * time.Millisecond
			}
		}

		if hasMaxProducts {
			if raw := cell(record, maxProductsCol); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("%s line %d: max_products %q must be zero or a positive whole number", path, line, raw)
				}
				store.MaxProducts = n
			}
		}

		stores = append(stores, store)
	}

	// Starting with no stores means exiting successfully and immediately, which
	// under a restart policy is indistinguishable from a crash loop.
	if len(stores) == 0 {
		return nil, fmt.Errorf("%s: nothing to monitor — the file has no store rows", path)
	}

	return stores, nil
}

func isBlank(record []string) bool {
	for _, f := range record {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
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
