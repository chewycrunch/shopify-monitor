package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/proxy"
	"github.com/chewycrunch/shopify-monitor/internal/utils"
	"github.com/chewycrunch/shopify-monitor/internal/webhook"
)

type Monitor struct {
	Url        string
	WebhookUrl string
	VariantMap map[int64]bool

	proxyBroker *proxy.ProxyManager
	pageWorkers int
	maxProducts int
	log         *slog.Logger
}

// Every request carries a deadline: a hung proxy would otherwise park a crawl
// goroutine forever, and the poll loop behind it with it.
const requestTimeout = 20 * time.Second

// normalizeBaseURL trims whitespace and trailing slashes, which the request
// path is concatenated onto.
func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// Instanciates a new monitor given a store, webhook, and proxy manager instance
func NewMonitor(url string, webhookUrl string, pb *proxy.ProxyManager, pageWorkers, maxProducts int) *Monitor {
	url = normalizeBaseURL(url)

	// Bound once here so every line this monitor logs carries its site.
	log := slog.Default().With("site", url)
	log.Info("creating monitor")
	return &Monitor{Url: url, WebhookUrl: webhookUrl, VariantMap: make(map[int64]bool), proxyBroker: pb, pageWorkers: pageWorkers, maxProducts: maxProducts, log: log}
}

// Initialize variants for the monitor
func (m *Monitor) InitializeVariants(ctx context.Context) error {
	m.log.Info("initializing variants")
	// Fetch variants and load them into the map
	res, err := FetchAllProducts(ctx, m.Url, m.nextClient, m.pageWorkers, m.maxProducts)
	if err != nil {
		return err
	}

	m.log.Info("initialized variants", "count", m.recordBaseline(res))

	return nil
}

// sleepCtx waits for d and reports whether it completed. A false return means
// ctx was cancelled, which is the caller's signal to stop.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Start monitoring the site
func (m *Monitor) StartWatching(ctx context.Context, duration time.Duration) error {
	if !sleepCtx(ctx, duration) {
		return ctx.Err()
	}

	// A fetch failure is routine, not terminal: proxies time out, stores rate
	// limit, DNS blips. Returning here would retire this site for the life of
	// the process, and once every site has retired the program exits — so one
	// slow proxy used to take down the whole monitor. Each cycle rotates to the
	// next proxy, so retrying is usually enough to recover on its own.
	failures := 0

	for {
		// Debug, not info: this fires every cycle for every site, so at a 5s
		// delay it is the overwhelming majority of the log and says nothing
		// beyond "still alive". The events below are what is worth reading.
		m.log.Debug("refreshing")
		res, err := FetchAllProducts(ctx, m.Url, m.nextClient, m.pageWorkers, m.maxProducts)
		if err != nil {
			// Give up only if the context is done — that is a real shutdown.
			if ctx.Err() != nil {
				return ctx.Err()
			}

			failures++
			m.log.Warn("fetch failed, retrying next cycle",
				"err", err,
				"consecutive_failures", failures,
			)

			if !sleepCtx(ctx, duration) {
				return ctx.Err()
			}
			continue
		}

		if failures > 0 {
			m.log.Info("fetch recovered", "after_failures", failures)
			failures = 0
		}

		for _, event := range m.detectChanges(res) {
			m.report(event)
		}

		if !sleepCtx(ctx, duration) {
			return ctx.Err()
		}
	}
}

// report announces a detected change.
//
// @spec DET-EVENT-006
func (m *Monitor) report(e Event) {
	switch e.Kind {
	case NewVariant:
		m.log.Info("new variant",
			"product", e.Product.Title,
			"variant", e.Variant.Title,
			"variant_id", e.Variant.ID,
			"handle", e.Product.Handle,
		)
		webhook.WebhookMaster.SendNewVariant()
	case Restock:
		m.log.Info("restock",
			"product", e.Product.Title,
			"variant", e.Variant.Title,
			"variant_id", e.Variant.ID,
			"handle", e.Product.Handle,
		)
		webhook.WebhookMaster.SendVariantAvail()
	}
}

// nextClient returns a client bound to the next proxy in the rotation, or a
// direct one if no proxy is usable.
//
// It returns a client rather than assigning one to the Monitor because a crawl
// fetches its pages concurrently: sharing a mutable client field across those
// goroutines would be a data race, and they each want a different proxy anyway.
// Safe for concurrent use — ProxyManager.GetProxy holds a mutex.
func (m *Monitor) nextClient() *http.Client {
	proxy, err := m.proxyBroker.GetProxy()
	if err != nil {
		m.log.Warn("failed to get proxy, using local client", "err", err)
		return &http.Client{Timeout: requestTimeout}
	}

	proxyUrl, err := url.Parse(proxy.Stringify())
	if err != nil {
		m.log.Warn("failed to parse proxy url, using local client", "err", err)
		return &http.Client{Timeout: requestTimeout}
	}

	return &http.Client{
		Timeout:   requestTimeout,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
	}
}

// Shopify's ceiling for this endpoint. Omitting it defaults to 30, which caps a
// monitor at the newest 30 products: ordering is by published_at, and a stock
// change does not move a product, so restocks below that line are never seen.
const pageSize = 250

// FetchAllProducts walks the catalogue, newest first, up to maxProducts.
//
// maxProducts of zero means the whole catalogue. Otherwise the crawl stops once
// it holds that many, and returns exactly that many — the cap counts products
// rather than pages so it means the same thing whatever the page size is, and
// so an operator setting it never has to know what the page size is.
//
// Pages go out in concurrent batches. Two reasons beyond speed: each request
// leaves through a different proxy, so Shopify sees one request per address
// rather than a dozen from one (which it answers with 429); and the catalogue is
// ordered by published_at, so a product published mid-crawl shifts everything
// down a slot and can slip across a page boundary already passed. A crawl that
// takes one second instead of seven has far less room for that.
//
// nextClient is called once per page. It must be safe to call concurrently.
//
// @spec ACQ-PAGE-001, ACQ-PAGE-002, ACQ-PAGE-003, ACQ-PAGE-004, ACQ-PAGE-007, ACQ-DEPTH-001, ACQ-DEPTH-002, ACQ-DEPTH-003, ACQ-DEPTH-004
func FetchAllProducts(ctx context.Context, shopifyBaseUrl string, nextClient func() *http.Client, concurrency, maxProducts int) ([]utils.Product, error) {
	if concurrency < 1 {
		concurrency = 1
	}

	// A cap makes the page count knowable before the first request, so a capped
	// crawl asks for exactly the pages it needs. Uncapped, the count is unknown
	// — Shopify reports no total — so the batch speculates a page ahead and
	// stops on the first short page, paying a few empty responses for it.
	lastPage := 0
	if maxProducts > 0 {
		lastPage = (maxProducts + pageSize - 1) / pageSize
	}

	var all []utils.Product

	for start := 1; lastPage == 0 || start <= lastPage; start += concurrency {
		width := concurrency
		if lastPage > 0 && start+width-1 > lastPage {
			width = lastPage - start + 1
		}

		pages := make([][]utils.Product, width)
		errs := make([]error, width)

		var wg sync.WaitGroup
		for i := range width {
			wg.Add(1)
			go func() {
				defer wg.Done()
				page := start + i
				products, err := FetchProductPage(ctx, shopifyBaseUrl, page, nextClient())
				if err != nil {
					errs[i] = fmt.Errorf("page %d: %w", page, err)
					return
				}
				pages[i] = products
			}()
		}
		wg.Wait()

		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}

		// Appended in page order so the result matches a sequential crawl.
		for _, products := range pages {
			all = append(all, products...)

			// A short page is the last one. Anything fetched past it in this
			// batch is beyond the end of the catalogue.
			if len(products) < pageSize {
				return truncate(all, maxProducts), nil
			}
		}
	}

	return truncate(all, maxProducts), nil
}

// truncate applies a product cap. The cap is enforced here rather than by
// asking for a smaller final page, because a page number addresses an offset of
// page size times page index: changing the size mid-crawl would skip or repeat
// products.
func truncate(products []utils.Product, maxProducts int) []utils.Product {
	if maxProducts > 0 && len(products) > maxProducts {
		return products[:maxProducts]
	}
	return products
}

func FetchProductPage(ctx context.Context, shopifyBaseUrl string, page int, client *http.Client) ([]utils.Product, error) {
	url := fmt.Sprintf("%s/products.json?limit=%d&page=%d", shopifyBaseUrl, pageSize, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch product data: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var productsResponse utils.ProductsResponse
	err = json.Unmarshal(body, &productsResponse)
	if err != nil {
		return nil, err
	}

	return productsResponse.Products, nil
}
