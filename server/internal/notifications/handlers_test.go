package notifications_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/notifications"
)

type stubWebPush struct {
	upsertCalled    *notifications.WebPushSubscription
	deletedEndpoint string
	err             error
}

func (s *stubWebPush) Upsert(_ context.Context, sub *notifications.WebPushSubscription) error {
	s.upsertCalled = sub
	return s.err
}
func (s *stubWebPush) Delete(_ context.Context, endpoint string) error {
	s.deletedEndpoint = endpoint
	return s.err
}
func (s *stubWebPush) ListForUser(context.Context, uuid.UUID) ([]*notifications.WebPushSubscription, error) {
	return nil, nil
}
func (s *stubWebPush) ListAll(context.Context) ([]*notifications.WebPushSubscription, error) {
	return nil, nil
}

type stubNotifier struct{ vapidKey string }

func (s *stubNotifier) Notify(context.Context, notifications.Event) error {
	return nil
}
func (s *stubNotifier) VAPIDPublicKey() string { return s.vapidKey }

func TestHandlers_Subscribe_DelegatesUpsert(t *testing.T) {
	repo := &stubWebPush{}
	h := notifications.NewHandlers(repo, &stubNotifier{})
	uid := uuid.New()
	sub := &notifications.WebPushSubscription{
		Endpoint: "https://e/x", UserID: uid, P256dh: "k", Auth: "a",
	}

	err := h.Subscribe(context.Background(), sub)

	require.NoError(t, err)
	require.Equal(t, sub, repo.upsertCalled)
}

func TestHandlers_Subscribe_PassesError(t *testing.T) {
	repo := &stubWebPush{err: errors.New("db down")}
	h := notifications.NewHandlers(repo, &stubNotifier{})

	err := h.Subscribe(context.Background(), &notifications.WebPushSubscription{})

	require.EqualError(t, err, "db down")
}

func TestHandlers_Unsubscribe_DelegatesDelete(t *testing.T) {
	repo := &stubWebPush{}
	h := notifications.NewHandlers(repo, &stubNotifier{})

	err := h.Unsubscribe(context.Background(), "https://e/x")

	require.NoError(t, err)
	require.Equal(t, "https://e/x", repo.deletedEndpoint)
}

func TestHandlers_VAPIDPublicKey_DelegatesNotifier(t *testing.T) {
	h := notifications.NewHandlers(&stubWebPush{}, &stubNotifier{vapidKey: "BabcDEF123"})

	got := h.VAPIDPublicKey()

	require.Equal(t, "BabcDEF123", got)
}
