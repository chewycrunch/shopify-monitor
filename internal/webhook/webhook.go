package webhook

import (
	"net/http"
	"time"
)

// Webhook sender, rescheduler
var WebhookMaster *WebhookManager

func init() {
	// Load init
	WebhookMaster = NewWebhookManager()
}

// WebhookManager posts to Discord directly rather than through the proxy pool.
// The proxies exist to keep Shopify from rate limiting us by IP; Discord limits
// per webhook, not per source address, so routing through a residential proxy
// would buy nothing and add a hop that can fail.
type WebhookManager struct {
	client *http.Client
}

func NewWebhookManager() *WebhookManager {
	// An explicit timeout: http.Client's zero value waits forever, and a
	// webhook that never returns would wedge whatever calls it.
	return &WebhookManager{client: &http.Client{Timeout: 10 * time.Second}}
}

func (webhook *WebhookManager) SendNewVariant() {

}

func (webhook *WebhookManager) SendVariantAvail() {

}

// func (webhook *WebhookManager) SendWebhook(url string, status string) {
// 	// Now send
// 	webhook.client.Post(url, "application/json", nil)
// 	// Send a webhook to the webhook URL
// }
