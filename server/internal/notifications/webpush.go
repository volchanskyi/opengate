package notifications

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrSubscriptionNotFound is returned when Delete targets a subscription that
// does not exist.
var ErrSubscriptionNotFound = errors.New("web push subscription not found")

// WebPushSubscription stores a user's Web Push subscription endpoint and the
// VAPID key material needed to encrypt payloads for it.
type WebPushSubscription struct {
	Endpoint string    `json:"endpoint"`
	UserID   uuid.UUID `json:"user_id"`
	P256dh   string    `json:"p256dh"`
	Auth     string    `json:"auth"`
}

// WebPushRepository is the persistence port for Web Push subscriptions.
type WebPushRepository interface {
	Upsert(ctx context.Context, sub *WebPushSubscription) error
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*WebPushSubscription, error)
	ListAll(ctx context.Context) ([]*WebPushSubscription, error)
	// Delete removes the endpoint's subscription owned by userID; the owner is part of the key,
	// so a colleague holding only the endpoint URL cannot cancel it.
	Delete(ctx context.Context, endpoint string, userID uuid.UUID) error
}
