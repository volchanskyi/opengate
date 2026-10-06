package notifications

import (
	"context"

	"github.com/google/uuid"
)

// Handlers exposes the notifications use cases to the transport layer.
type Handlers struct {
	webPush  WebPushRepository
	notifier Notifier
}

// NewHandlers wires a Handlers struct against the two notifications ports.
func NewHandlers(webPush WebPushRepository, notifier Notifier) *Handlers {
	return &Handlers{webPush: webPush, notifier: notifier}
}

// Subscribe persists a browser's web-push subscription.
func (h *Handlers) Subscribe(ctx context.Context, sub *WebPushSubscription) error {
	return h.webPush.Upsert(ctx, sub)
}

// Unsubscribe removes the calling user's subscription for this endpoint URL.
func (h *Handlers) Unsubscribe(ctx context.Context, endpoint string, userID uuid.UUID) error {
	return h.webPush.Delete(ctx, endpoint, userID)
}

// VAPIDPublicKey returns the server's VAPID public key for browser subscriptions.
func (h *Handlers) VAPIDPublicKey() string {
	return h.notifier.VAPIDPublicKey()
}
