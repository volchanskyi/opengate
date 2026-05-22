package notifications

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// testVAPIDKeys generates a valid VAPID key pair for use in tests.
func testVAPIDKeys(t *testing.T) (priv, pub string) {
	t.Helper()
	dir := t.TempDir()
	priv, pub, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)
	return priv, pub
}

// mockSub is a test helper for creating push subscriptions.
type mockSub struct {
	Endpoint string
	UserID   uuid.UUID
	P256dh   string
	Auth     string
}

// notifMockRepo implements WebPushRepository for PushNotifier tests.
type notifMockRepo struct {
	subs       []*WebPushSubscription
	deletedEPs []string
	mu         sync.Mutex
}

func newMockNotifRepo(subs []*mockSub) *notifMockRepo {
	var ws []*WebPushSubscription
	for _, s := range subs {
		ws = append(ws, &WebPushSubscription{
			Endpoint: s.Endpoint,
			UserID:   s.UserID,
			P256dh:   s.P256dh,
			Auth:     s.Auth,
		})
	}
	return &notifMockRepo{subs: ws}
}

func (m *notifMockRepo) Upsert(_ context.Context, _ *WebPushSubscription) error { return nil }

func (m *notifMockRepo) ListForUser(_ context.Context, _ uuid.UUID) ([]*WebPushSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs, nil
}

func (m *notifMockRepo) ListAll(_ context.Context) ([]*WebPushSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs, nil
}

func (m *notifMockRepo) Delete(_ context.Context, endpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedEPs = append(m.deletedEPs, endpoint)
	return nil
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
