// Package telemetry owns Edge Sentinel server-side telemetry persistence: numeric samples in
// VictoriaMetrics.
package telemetry

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Sample is one numeric Edge Sentinel point destined for VictoriaMetrics.
type Sample struct {
	Name   string
	Value  float64
	TS     time.Time
	Labels map[string]string
}

// NumericWriter persists numeric telemetry points.
type NumericWriter interface {
	WriteSamples(ctx context.Context, tenantID uuid.UUID, deviceID uuid.UUID, samples []Sample) error
}
