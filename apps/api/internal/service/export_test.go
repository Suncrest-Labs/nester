package service

import "github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"

// DeliverWebhookForTest runs delivery with no sleep between retries (for fast tests).
func DeliverWebhookForTest(wh webhook.Webhook, payload []byte) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if deliver(wh, payload) {
			return
		}
	}
}
