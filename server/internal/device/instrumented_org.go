package device

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OrgForDevice records the organization lookup used to scope an agent stream.
func (i *InstrumentedDevices) OrgForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error) {
	start := time.Now()
	orgID, err := i.inner.OrgForDevice(ctx, id)
	i.observer.Observe("device.Device.OrgForDevice", time.Since(start), err == nil)
	return orgID, err
}
