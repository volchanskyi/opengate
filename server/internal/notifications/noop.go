package notifications

import "context"

// NoopNotifier is a no-op Notifier used in tests and when push is disabled.
type NoopNotifier struct{}

// Notify does nothing and returns nil.
func (n *NoopNotifier) Notify(_ context.Context, _ Event) error { return nil }

// VAPIDPublicKey returns an empty string.
func (n *NoopNotifier) VAPIDPublicKey() string { return "" }
