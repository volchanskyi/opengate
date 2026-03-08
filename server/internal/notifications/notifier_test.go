package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventToPayload(t *testing.T) {
	ts := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	devID := uuid.New()

	tests := []struct {
		name      string
		event     Event
		wantTitle string
		wantBody  string
	}{
		{
			name: "device_online",
			event: Event{
				Type: EventDeviceOnline, DeviceHostname: "srv-01",
				DeviceID: devID, Timestamp: ts,
			},
			wantTitle: "Device Online",
			wantBody:  "srv-01 is now online",
		},
		{
			name: "device_offline",
			event: Event{
				Type: EventDeviceOffline, DeviceHostname: "srv-01",
				DeviceID: devID, Timestamp: ts,
			},
			wantTitle: "Device Offline",
			wantBody:  "srv-01 went offline",
		},
		{
			name: "session_started",
			event: Event{
				Type: EventSessionStarted, DeviceHostname: "dev-pc",
				DeviceID: devID, Timestamp: ts,
			},
			wantTitle: "Session Started",
			wantBody:  "Remote session started on dev-pc",
		},
		{
			name: "session_ended",
			event: Event{
				Type: EventSessionEnded, DeviceHostname: "dev-pc",
				DeviceID: devID, Timestamp: ts,
			},
			wantTitle: "Session Ended",
			wantBody:  "Remote session ended on dev-pc",
		},
		{
			name: "unknown_event",
			event: Event{
				Type: "custom_event", DeviceHostname: "box",
				DeviceID: devID, Timestamp: ts,
			},
			wantTitle: "Notification",
			wantBody:  "Event: custom_event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := EventToPayload(tt.event)
			assert.Equal(t, tt.wantTitle, p.Title)
			assert.Equal(t, tt.wantBody, p.Body)
			assert.Equal(t, string(tt.event.Type), p.Type)
			assert.Equal(t, devID.String(), p.DeviceID)
			assert.Equal(t, "2026-03-07T12:00:00Z", p.Timestamp)
		})
	}
}

func TestNoopNotifier(t *testing.T) {
	n := &NoopNotifier{}
	err := n.Notify(context.Background(), Event{Type: EventDeviceOnline})
	assert.NoError(t, err)
	assert.Equal(t, "", n.VAPIDPublicKey())
}

func TestPushNotifier_NoSubscriptions(t *testing.T) {
	store := newMockNotifStore(nil)
	logger := newDiscardLogger()
	priv, pub := testVAPIDKeys(t)
	n := NewPushNotifier(store, priv, pub, "test@example.com", logger)

	err := n.Notify(context.Background(), Event{Type: EventDeviceOnline})
	assert.NoError(t, err)
}

func TestPushNotifier_VAPIDPublicKey(t *testing.T) {
	store := newMockNotifStore(nil)
	logger := newDiscardLogger()
	n := NewPushNotifier(store, "privkey", "mypubkey", "test@example.com", logger)

	assert.Equal(t, "mypubkey", n.VAPIDPublicKey())
}

func TestPushNotifier_StaleSubscriptionDeleted(t *testing.T) {
	// Spin up a test server that returns 410 Gone.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	priv, pub := testVAPIDKeys(t)
	store := newMockNotifStore([]*mockSub{
		{Endpoint: srv.URL, P256dh: pub, Auth: priv[:22]},
	})
	logger := newDiscardLogger()
	n := NewPushNotifier(store, priv, pub, "test@example.com", logger)

	err := n.Notify(context.Background(), Event{
		Type:           EventDeviceOffline,
		DeviceHostname: "srv-01",
		DeviceID:       uuid.New(),
		Timestamp:      time.Now(),
	})
	assert.NoError(t, err)

	// The stale subscription should have been deleted.
	store.mu.Lock()
	defer store.mu.Unlock()
	assert.Contains(t, store.deletedEPs, srv.URL)
}

func TestPushNotifier_NonGoneErrorDoesNotDelete(t *testing.T) {
	// Server returns 500 — subscription should NOT be deleted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	priv, pub := testVAPIDKeys(t)
	store := newMockNotifStore([]*mockSub{
		{Endpoint: srv.URL, P256dh: pub, Auth: priv[:22]},
	})
	logger := newDiscardLogger()
	n := NewPushNotifier(store, priv, pub, "test@example.com", logger)

	err := n.Notify(context.Background(), Event{
		Type:           EventDeviceOnline,
		DeviceHostname: "srv-01",
		DeviceID:       uuid.New(),
		Timestamp:      time.Now(),
	})
	assert.NoError(t, err)

	store.mu.Lock()
	defer store.mu.Unlock()
	assert.Empty(t, store.deletedEPs)
}

func TestPushNotifier_SendPayloadReachesServer(t *testing.T) {
	requestReceived := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestReceived = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	priv, pub := testVAPIDKeys(t)
	store := newMockNotifStore([]*mockSub{
		{Endpoint: srv.URL, P256dh: pub, Auth: priv[:22]},
	})
	logger := newDiscardLogger()
	n := NewPushNotifier(store, priv, pub, "test@example.com", logger)

	_ = n.Notify(context.Background(), Event{
		Type:           EventDeviceOnline,
		DeviceHostname: "myhost",
		DeviceID:       uuid.New(),
		Timestamp:      time.Now(),
	})
	assert.True(t, requestReceived, "push request should reach the test server")
}

func TestEventToPayload_JSONRoundtrip(t *testing.T) {
	p := EventToPayload(Event{
		Type:           EventDeviceOnline,
		DeviceHostname: "test-host",
		DeviceID:       uuid.New(),
		Timestamp:      time.Now().UTC(),
	})
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded PushPayload
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, p.Title, decoded.Title)
	assert.Equal(t, p.Body, decoded.Body)
	assert.Equal(t, p.Type, decoded.Type)
}
