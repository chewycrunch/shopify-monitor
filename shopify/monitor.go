package shopify

import (
	"log"
	"time"
)

func init() {
	// Load init
}

func main() {
	// for each site
	// start go subroutine for initla pull
	// start go subroutine for monitoring

	// Load main (start monitoring)
}

// Handle variant delisting
// Compare variants to the map, see if any availabilty has changed, or new variants exist

// If so, send a webhook to the webhook URL

type Monitor struct {
	Url        string
	WebhookUrl string
	VariantMap map[int64]bool
}

func NewMonitor(url string, webhookUrl string) *Monitor {
	return &Monitor{Url: url, WebhookUrl: webhookUrl, VariantMap: make(map[int64]bool)}
}

func (m *Monitor) InitializeVariants() error {
	// Fetch variants and load them into the map
	res, err := FetchProductData(m.Url)
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

	log.Printf("Initialized %d variants for %s", counter, m.Url)

	return nil
}

func (m *Monitor) StartWatching(duration time.Duration) error {
	time.Sleep(duration)

	for {
		log.Printf("Refreshing %v", m.Url)
		res, err := FetchProductData(m.Url)
		if err != nil {
			return err
		}

		for _, product := range res {
			for _, variant := range product.Variants {
				// Check if variant is in map
				_, ok := m.VariantMap[variant.ID]
				if !ok {
					// Variant is not in map (NEW VARIANT), send webhook
					// SendWebhook()
					m.VariantMap[variant.ID] = variant.Available
				} else {
					// Variant is in map, check if availability has changed
					if m.VariantMap[variant.ID] != variant.Available {
						// Availability has changed, send webhook
						// SendWebhook()
						m.VariantMap[variant.ID] = variant.Available
					}
				}
			}
		}

		time.Sleep(duration)
	}
}
