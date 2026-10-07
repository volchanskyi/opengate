package api

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// Edge-health band boundaries on a device's latest anomaly rate in [0,1]: at or above
// anomalousThreshold is anomalous, at or above watchThreshold is watch, otherwise healthy.
const (
	watchThreshold     = 0.1
	anomalousThreshold = 0.3
)

// GetDeviceSummary implements StrictServerInterface as a fixed-size rollup scoped to the
// caller's tenant, administrators included.
func (s *Server) GetDeviceSummary(ctx context.Context, request GetDeviceSummaryRequestObject) (GetDeviceSummaryResponseObject, error) {
	var organizationID device.OrganizationID
	if request.Params.OrganizationId != nil {
		organizationID = *request.Params.OrganizationId
	}
	counts, err := s.devices.Counts(ctx, organizationID)
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

// countHealthBands classifies the tenant's devices into edge-health bands; unknown is the
// remainder with no anomaly rate in the lookback window, and absent telemetry leaves all unknown.
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
		// A sample can outlive its device row, so the remainder clamps at zero.
		Unknown: max(total-bands.Anomalous-bands.Watch-bands.Healthy, 0),
	}
}
