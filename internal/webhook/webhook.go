package webhook

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/chewycrunch/shopify-monitor/internal/proxy"
)

// Webhook sender, rescheduler
//
// Built in init(), which runs before main can call slog.SetDefault, so this
// package calls slog directly rather than binding a logger on the manager.
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
// Unused until SendWebhook above is implemented.
//
//nolint:unused
func (webhook *WebhookManager) rotateClient() {
	proxy, err := webhook.proxyBroker.GetProxy()
	if err != nil {
		slog.Warn("failed to get proxy, using local client", "component", "webhook", "err", err)
		webhook.useLocalClient()
		return
	}
	proxyUrl, err := url.Parse(proxy.Stringify())
	if err != nil {
		slog.Warn("failed to parse proxy url, using local client", "component", "webhook", "err", err)
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
//
//nolint:unused
func (webhook *WebhookManager) useLocalClient() {
	webhook.client = &http.Client{}
}
