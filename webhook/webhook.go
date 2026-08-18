package webhook

import (
	"log"
	"net/http"
	"net/url"

	"github.com/chewycrunch/shopify-monitor/internal/proxy"
)

// Webhook sender, rescheduler
var WebhookMaster *WebhookManager

func init() {
	// Load init
	WebhookMaster = NewWebhookManager(proxy.NewProxyManager(10))
}

type WebhookManager struct {
	client      *http.Client
	proxyBroker *proxy.ProxyManager
}

func NewWebhookManager(pb *proxy.ProxyManager) *WebhookManager {
	return &WebhookManager{client: &http.Client{}, proxyBroker: pb}
}

func (webhook *WebhookManager) SendNewVariant() {

}

func (webhook *WebhookManager) SendVariantAvail() {

}

// func (webhook *WebhookManager) SendWebhook(url string, status string) {
// 	webhook.rotateClient()
// 	// Now send
// 	webhook.client.Post(url, "application/json", nil)
// 	// Send a webhook to the webhook URL
// }

// Rotate proxy client (fallback to local client if no proxies available)
func (webhook *WebhookManager) rotateClient() {
	proxy, err := webhook.proxyBroker.GetProxy()
	if err != nil {
		log.Printf("Failed to get proxy for webhook: %v", err)
		webhook.useLocalClient()
		return
	}
	proxyUrl, err := url.Parse(proxy.Stringify())
	if err != nil {
		log.Printf("Failed to parse proxy URL for: %v", err)
		webhook.useLocalClient()
		return
	}

	webhook.client = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyUrl),
		},
	}

}

// Use local client
func (webhook *WebhookManager) useLocalClient() {
	webhook.client = &http.Client{}
}
