// Package telemetry owns Edge Sentinel server-side telemetry persistence:
// numeric samples in VictoriaMetrics and process snapshots in Postgres RLS.
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
	WriteSamples(ctx context.Context, orgID uuid.UUID, deviceID uuid.UUID, samples []Sample) error
}

// ProcessSample is one sanitized process row from an Edge Sentinel report.
type ProcessSample struct {
	TS          time.Time
	Rank        uint32
	Basename    string
	CmdlineHash *string
	PID         uint32
	CPU         float64
	Mem         float64
}

// ProcessRepository persists and reads tenant-scoped process snapshots.
type ProcessRepository interface {
	UpsertReport(ctx context.Context, deviceID uuid.UUID, ts time.Time, samples []ProcessSample) error
	ListLatest(ctx context.Context, deviceID uuid.UUID, limit int) ([]ProcessSample, error)
}
