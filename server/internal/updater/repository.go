package updater

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Status represents the outcome of a pushed device update.
type Status string

// Status values.
const (
	StatusPending Status = "pending"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

// DeviceUpdate tracks a single update push to a device.
type DeviceUpdate struct {
	ID       int64      `json:"id"`
	DeviceID uuid.UUID  `json:"device_id"`
	Version  string     `json:"version"`
	Status   Status     `json:"status"`
	Error    string     `json:"error"`
	PushedAt time.Time  `json:"pushed_at"`
	AckedAt  *time.Time `json:"acked_at,omitempty"`
}

// DeviceUpdateRepository is the outbound persistence port for device update
// records. Create populates the generated ID on success; SetStatus is the
// terminal ack from the agent (success or failed) and updates the AckedAt
// timestamp atomically.
type DeviceUpdateRepository interface {
	Create(ctx context.Context, du *DeviceUpdate) error
	SetStatus(ctx context.Context, deviceID uuid.UUID, version string, status Status, errMsg string) error
	ListByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error)
}
