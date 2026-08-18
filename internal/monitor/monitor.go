package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

func NewMonitor(url string, webhookUrl string, pb *proxy.ProxyManager) *Monitor {
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

// Start monitoring the site
func (m *Monitor) StartWatching(ctx context.Context, duration time.Duration) error {
	time.Sleep(duration)

	for {
		m.log.Info("refreshing")
		m.rotateClient()
		res, err := FetchProductData(ctx, m.Url, m.client)
		if err != nil {
			return err
		}

		for _, product := range res {
			for _, variant := range product.Variants {
				// Check if variant is in map
				_, ok := m.VariantMap[variant.ID]
				if !ok {
					m.VariantMap[variant.ID] = variant.Available

					// Variant is not in map (NEW VARIANT), send webhook
					webhook.WebhookMaster.SendNewVariant()
				} else if m.VariantMap[variant.ID] != variant.Available && variant.Available {
					// Variant is in map and availability has changed to true,
					// send webhook
					m.VariantMap[variant.ID] = variant.Available

					webhook.WebhookMaster.SendVariantAvail()
				}
			}
		}

		time.Sleep(duration)
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
