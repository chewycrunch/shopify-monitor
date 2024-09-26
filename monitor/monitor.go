package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/chewycrunch/shopify-monitor/proxy"
	"github.com/chewycrunch/shopify-monitor/utils"
	"github.com/chewycrunch/shopify-monitor/webhook"
)

type Monitor struct {
	Url        string
	WebhookUrl string
	VariantMap map[int64]bool

	client      *http.Client
	proxyBroker *proxy.ProxyManager
}

func NewMonitor(url string, webhookUrl string, pb *proxy.ProxyManager) *Monitor {
	log.Printf("%v | Creating monitor", url)
	return &Monitor{Url: url, WebhookUrl: webhookUrl, VariantMap: make(map[int64]bool), client: &http.Client{}, proxyBroker: pb}
}

// Initialize variants for the monitor
func (m *Monitor) InitializeVariants() error {
	log.Printf("%v | Initializing variants", m.Url)
	// Fetch variants and load them into the map
	m.rotateClient()
	res, err := FetchProductData(m.Url, m.client)
	if err != nil {
		log.Printf("Failed to fetch product data for %s: %v", m.Url, err)
		return err
	}

	counter := 0
	for _, product := range res {
		for _, variant := range product.Variants {
			m.VariantMap[variant.ID] = variant.Available
			counter++
		}
	}

	log.Printf("%v | Initialized %d variants", m.Url, counter)

	return nil
}

// Start monitoring the site
func (m *Monitor) StartWatching(duration time.Duration) error {
	time.Sleep(duration)

	for {
		log.Printf("%v | Refreshing", m.Url)
		m.rotateClient()
		res, err := FetchProductData(m.Url, m.client)
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
				} else {
					// Variant is in map, check if availability has changed
					if m.VariantMap[variant.ID] != variant.Available && variant.Available {
						m.VariantMap[variant.ID] = variant.Available

						webhook.WebhookMaster.SendVariantAvail()
						// Availability has changed to true, send webhook
					}
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
		log.Printf("Failed to get proxy for %s: %v", m.Url, err)
		m.useLocalClient()
		return
	}
	proxyUrl, err := url.Parse(proxy.Stringify())
	if err != nil {
		log.Printf("Failed to parse proxy URL for %s: %v", m.Url, err)
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

func FetchProductData(shopifyBaseUrl string, client *http.Client) ([]utils.Product, error) {
	req, err := http.NewRequest("GET", shopifyBaseUrl+"/products.json", nil)
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
