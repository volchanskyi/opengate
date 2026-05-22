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

// WebPushRepository is the outbound persistence port for Web Push
// subscriptions. Per ADR-021, the interface lives with the consuming module
// (notifications); the Postgres adapter lives alongside in this package.
type WebPushRepository interface {
	Upsert(ctx context.Context, sub *WebPushSubscription) error
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*WebPushSubscription, error)
	ListAll(ctx context.Context) ([]*WebPushSubscription, error)
	Delete(ctx context.Context, endpoint string) error
}
