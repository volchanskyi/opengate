// Package notifications handles Web Push and real-time event dispatch.
package notifications

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EventType identifies the kind of notification event.
type EventType string

const (
	// EventDeviceOnline fires when an agent connects.
	EventDeviceOnline EventType = "device_online"
	// EventDeviceOffline fires when an agent disconnects.
	EventDeviceOffline EventType = "device_offline"
	// EventSessionStarted fires when a relay session is created.
	EventSessionStarted EventType = "session_started"
	// EventSessionEnded fires when a relay session is deleted.
	EventSessionEnded EventType = "session_ended"
)

// Event represents a notification-worthy occurrence in the system.
type Event struct {
	Type           EventType
	DeviceID       uuid.UUID
	DeviceHostname string
	UserID         uuid.UUID
	Timestamp      time.Time
}

// PushPayload is the JSON body sent in a Web Push message.
type PushPayload struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	DeviceID  string `json:"device_id"`
	Timestamp string `json:"timestamp"`
}

// Notifier sends push notifications for system events.
type Notifier interface {
	// Notify dispatches a push notification for the given event.
	Notify(ctx context.Context, event Event) error
	// VAPIDPublicKey returns the server's VAPID public key.
	VAPIDPublicKey() string
}

// EventToPayload converts an Event to a PushPayload.
func EventToPayload(e Event) PushPayload {
	var title, body string
	switch e.Type {
	case EventDeviceOnline:
		title = "Device Online"
		body = e.DeviceHostname + " is now online"
	case EventDeviceOffline:
		title = "Device Offline"
		body = e.DeviceHostname + " went offline"
	case EventSessionStarted:
		title = "Session Started"
		body = "Remote session started on " + e.DeviceHostname
	case EventSessionEnded:
		title = "Session Ended"
		body = "Remote session ended on " + e.DeviceHostname
	default:
		title = "Notification"
		body = "Event: " + string(e.Type)
	}
	return PushPayload{
		Type:      string(e.Type),
		Title:     title,
		Body:      body,
		DeviceID:  e.DeviceID.String(),
		Timestamp: e.Timestamp.UTC().Format(time.RFC3339),
	}
}
