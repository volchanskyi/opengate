package device

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TenantForDevice records the tenant lookup used to scope an agent stream.
func (i *InstrumentedDevices) TenantForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error) {
	start := time.Now()
	tenantID, err := i.inner.TenantForDevice(ctx, id)
	i.observer.Observe("device.Device.TenantForDevice", time.Since(start), err == nil)
	return tenantID, err
}
