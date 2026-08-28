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
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/proxy"
	"github.com/chewycrunch/shopify-monitor/internal/utils"
	"github.com/chewycrunch/shopify-monitor/internal/webhook"
)

type Monitor struct {
	Url        string
	WebhookUrl string
	VariantMap map[int64]bool

	client      *http.Client
	proxyBroker *proxy.ProxyManager
	log         *slog.Logger
}

// normalizeBaseURL trims whitespace and trailing slashes, which the request
// path is concatenated onto.
func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// Instanciates a new monitor given a store, webhook, and proxy manager instance
func NewMonitor(url string, webhookUrl string, pb *proxy.ProxyManager) *Monitor {
	url = normalizeBaseURL(url)

	// Bound once here so every line this monitor logs carries its site.
	log := slog.Default().With("site", url)
	log.Info("creating monitor")
	return &Monitor{Url: url, WebhookUrl: webhookUrl, VariantMap: make(map[int64]bool), client: &http.Client{}, proxyBroker: pb, log: log}
}

// Initialize variants for the monitor
func (m *Monitor) InitializeVariants(ctx context.Context) error {
	m.log.Info("initializing variants")
	// Fetch variants and load them into the map
	m.rotateClient()
	res, err := FetchProductData(ctx, m.Url, m.client)
	if err != nil {
		return err
	}

	counter := 0
	for _, product := range res {
		for _, variant := range product.Variants {
			m.VariantMap[variant.ID] = variant.Available
			counter++
		}
	}

	m.log.Info("initialized variants", "count", counter)

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
		m.rotateClient()
		res, err := FetchProductData(ctx, m.Url, m.client)
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

		for _, product := range res {
			for _, variant := range product.Variants {
				// Check if variant is in map
				_, ok := m.VariantMap[variant.ID]
				if !ok {
					m.VariantMap[variant.ID] = variant.Available

					// Variant is not in map (NEW VARIANT), send webhook
					m.log.Info("new variant",
						"product", product.Title,
						"variant", variant.Title,
						"variant_id", variant.ID,
						"available", variant.Available,
						"handle", product.Handle,
					)
					webhook.WebhookMaster.SendNewVariant()
				} else if m.VariantMap[variant.ID] != variant.Available && variant.Available {
					// Variant is in map and availability has changed to true,
					// send webhook
					m.VariantMap[variant.ID] = variant.Available

					m.log.Info("restock",
						"product", product.Title,
						"variant", variant.Title,
						"variant_id", variant.ID,
						"handle", product.Handle,
					)
					webhook.WebhookMaster.SendVariantAvail()
				}
			}
		}

		if !sleepCtx(ctx, duration) {
			return ctx.Err()
		}
	}
}

// Rotate proxy client (fallback to local client if no proxies available)
func (m *Monitor) rotateClient() {
	proxy, err := m.proxyBroker.GetProxy()
	if err != nil {
		m.log.Warn("failed to get proxy, using local client", "err", err)
		m.useLocalClient()
		return
	}
	proxyUrl, err := url.Parse(proxy.Stringify())
	if err != nil {
		m.log.Warn("failed to parse proxy url, using local client", "err", err)
		m.useLocalClient()
		return
	}

	m.client = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyUrl),
		},
	}

}

// Use local client
func (m *Monitor) useLocalClient() {
	m.client = &http.Client{}
}

func FetchProductData(ctx context.Context, shopifyBaseUrl string, client *http.Client) ([]utils.Product, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shopifyBaseUrl+"/products.json", nil)
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
