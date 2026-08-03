package notifications

import (
	"context"

	"github.com/google/uuid"
)

// Handlers exposes the notifications module's use cases to transport-layer
// callers. The api package's push/VAPID handlers translate HTTP requests and
// responses to method calls on this
// struct. The webPush port handles subscription persistence; the notifier
// port answers configuration-style queries (VAPID public key) and is also
// invoked from non-transport flows (agent updates, device-state changes).
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

// VAPIDPublicKey returns the server's VAPID public key for the browser to
// register a push subscription against.
func (h *Handlers) VAPIDPublicKey() string {
	return h.notifier.VAPIDPublicKey()
}
