package lifecycle

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// SubjectLister lists the (tenant, device) subjects that own VictoriaMetrics
// series, so the reconciler can diff them against Postgres.
type SubjectLister interface {
	ListSubjects(ctx context.Context) ([]telemetry.SeriesSubject, error)
}

// Reconciler deletes VictoriaMetrics series whose device row is absent from Postgres.
// A device row exists before its first telemetry ingest, so a series without one is orphaned.
type Reconciler struct {
	inventory SubjectLister
	series    SeriesPurger
	pg        PGPurger
	logger    *slog.Logger
}

// NewReconciler builds a reconciliation sweep. A nil logger uses slog.Default.
func NewReconciler(inventory SubjectLister, series SeriesPurger, pg PGPurger, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{inventory: inventory, series: series, pg: pg, logger: logger}
}

// Sweep deletes every VictoriaMetrics subject whose device id is absent from Postgres and
// returns the orphan count; a second run over a clean store deletes nothing.
func (r *Reconciler) Sweep(ctx context.Context) (int, error) {
	live, err := r.pg.ListAllDeviceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconcile: list live devices: %w", err)
	}
	liveSet := make(map[uuid.UUID]struct{}, len(live))
	for _, id := range live {
		liveSet[id] = struct{}{}
	}

	subjects, err := r.inventory.ListSubjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconcile: list vm subjects: %w", err)
	}

	purged := 0
	for _, subject := range subjects {
		if _, ok := liveSet[subject.DeviceID]; ok {
			continue
		}
		deviceID := subject.DeviceID
		if err := r.series.DeleteSeries(ctx, subject.TenantID, &deviceID); err != nil {
			r.logger.Error("reconcile: delete orphan series failed",
				"tenant_id", subject.TenantID, "device_id", deviceID, "error", err)
			continue
		}
		r.logger.Warn("reconcile: purged orphan telemetry series",
			"tenant_id", subject.TenantID, "device_id", deviceID)
		purged++
	}
	return purged, nil
}
