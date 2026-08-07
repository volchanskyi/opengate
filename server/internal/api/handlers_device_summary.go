package api

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// Edge-health band boundaries on a device's latest anomaly rate, in [0,1]. A
// rate at or above anomalousThreshold is anomalous, at or above watchThreshold
// is watch, below that is healthy. The web client classifies each device badge
// with the same numbers; health_bands_sync_test.go fails if the two drift.
const (
	watchThreshold     = 0.1
	anomalousThreshold = 0.3
)

// GetDeviceSummary implements StrictServerInterface. It answers the dashboard
// with a fixed-size rollup: one aggregate row for the status tiles and one
// instant telemetry query for the health bands. Nothing per-device crosses
// either boundary, so the work and the payload are the same for a fleet of one
// and a fleet of ten thousand.
//
// The rollup is tenant-scoped for every caller, administrators included: the
// dashboard describes the caller's own tenant, so the tiles and the bands
// always cover one device set and unknown is exact. Admin cross-tenant
// reads stay available everywhere else.
func (s *Server) GetDeviceSummary(ctx context.Context, _ GetDeviceSummaryRequestObject) (GetDeviceSummaryResponseObject, error) {
	counts, err := s.devices.Counts(ctx)
	if err != nil {
		return nil, err
	}

	bands := s.countHealthBands(ctx, counts.Total)
	return GetDeviceSummary200JSONResponse(DeviceSummary{
		Total:       counts.Total,
		Online:      counts.Online,
		Offline:     counts.Total - counts.Online,
		Maintenance: counts.Maintenance,
		Health:      bands,
	}), nil
}

// countHealthBands classifies the tenant's devices into edge-health bands
// and derives unknown as the remainder — the devices that reported no anomaly
// rate inside the badge lookback window.
//
// It is best-effort by design: with telemetry unconfigured or the query failing,
// every measured band reads zero and every device lands in unknown. The tiles
// must render, so this path never fails the request.
func (s *Server) countHealthBands(ctx context.Context, total int) FleetHealthCounts {
	unknown := FleetHealthCounts{Unknown: total}
	if s.telemetryReader == nil {
		return unknown
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return unknown
	}

	bands, err := s.telemetryReader.CountAnomalyBands(
		ctx, tenant.TenantID, watchThreshold, anomalousThreshold, time.Now(), anomalyBadgeLookback)
	if err != nil {
		s.logger.WarnContext(ctx, "fleet health band query failed", "error", err)
		return unknown
	}

	return FleetHealthCounts{
		Anomalous: bands.Anomalous,
		Watch:     bands.Watch,
		Healthy:   bands.Healthy,
		// A sample can outlive the device row it described, so clamp rather than
		// report a negative remainder.
		Unknown: max(total-bands.Anomalous-bands.Watch-bands.Healthy, 0),
	}
}
