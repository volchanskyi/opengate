package notifications

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// PushNotifier sends Web Push notifications using VAPID.
type PushNotifier struct {
	store      db.Store
	vapidPriv  string
	vapidPub   string
	contact    string
	logger     *slog.Logger
	httpClient *http.Client
}

// NewPushNotifier creates a PushNotifier.
func NewPushNotifier(store db.Store, vapidPriv, vapidPub, contact string, logger *slog.Logger) *PushNotifier {
	return &PushNotifier{
		store:      store,
		vapidPriv:  vapidPriv,
		vapidPub:   vapidPub,
		contact:    contact,
		logger:     logger,
		httpClient: http.DefaultClient,
	}
}

// VAPIDPublicKey returns the server's VAPID public key.
func (p *PushNotifier) VAPIDPublicKey() string {
	return p.vapidPub
}

// Notify sends a push notification for the given event to all subscribers.
func (p *PushNotifier) Notify(ctx context.Context, event Event) error {
	subs, err := p.store.ListAllWebPushSubscriptions(ctx)
	if err != nil {
		p.logger.Error("list push subscriptions", "error", err)
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	payload := EventToPayload(event)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for _, sub := range subs {
		wpSub := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.P256dh,
				Auth:   sub.Auth,
			},
		}
		resp, err := webpush.SendNotificationWithContext(ctx, payloadJSON, wpSub, &webpush.Options{
			Subscriber:      p.contact,
			VAPIDPublicKey:  p.vapidPub,
			VAPIDPrivateKey: p.vapidPriv,
			HTTPClient:      p.httpClient,
		})
		if err != nil {
			p.logger.Warn("push notification failed", "endpoint", sub.Endpoint, "error", err)
			continue
		}
		resp.Body.Close()

		// 410 Gone means the subscription is stale — remove it.
		if resp.StatusCode == http.StatusGone {
			p.logger.Info("removing stale push subscription", "endpoint", sub.Endpoint)
			if delErr := p.store.DeleteWebPushSubscription(ctx, sub.Endpoint); delErr != nil {
				p.logger.Warn("delete stale subscription", "endpoint", sub.Endpoint, "error", delErr)
			}
		}
	}

	return nil
}
