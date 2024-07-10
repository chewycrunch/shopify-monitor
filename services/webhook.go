package services

import (
	"log"
	"net/http"
	"net/url"
)

// Webhook sender, rescheduler
var Webhook *WebhookManager

func init() {
	// Load init
	Webhook = NewWebhookManager(NewProxyManager(10))
}

type WebhookManager struct {
	client      *http.Client
	proxyBroker *ProxyBroker
}

func NewWebhookManager(pb *ProxyBroker) *WebhookManager {
	return &WebhookManager{client: &http.Client{}, proxyBroker: pb}
}

func (webhook *WebhookManager) SendWebhook(url string) {
	webhook.rotateClient()
	// Now send
	// Send a webhook to the webhook URL
}

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
