package lifecycle

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// SeriesInventory lists the (org, device) subjects that own VictoriaMetrics
// series, so the reconciler can diff them against Postgres.
type SeriesInventory interface {
	ListSubjects(ctx context.Context) ([]telemetry.SeriesSubject, error)
}

// Reconciler is the periodic defense-in-depth sweep: it deletes VictoriaMetrics
// series whose device no longer exists in Postgres. The purge stores are not one
// transaction, so a partial failure — or a purge that crashed before Resume ran
// — can leave series behind; this sweep garbage-collects them. Device rows are
// created at handshake before any telemetry ingest, so a device with series but
// no row is genuinely orphaned, not mid-enrollment.
type Reconciler struct {
	inventory SeriesInventory
	series    SeriesPurger
	pg        PGPurger
	logger    *slog.Logger
}

// NewReconciler builds a reconciliation sweep. A nil logger uses slog.Default.
func NewReconciler(inventory SeriesInventory, series SeriesPurger, pg PGPurger, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{inventory: inventory, series: series, pg: pg, logger: logger}
}

// Sweep deletes every VictoriaMetrics subject whose device id is absent from
// Postgres and returns how many orphans it purged. It is idempotent: a second
// run over a clean store deletes nothing.
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
		if err := r.series.DeleteSeries(ctx, subject.OrgID, &deviceID); err != nil {
			r.logger.Error("reconcile: delete orphan series failed",
				"org_id", subject.OrgID, "device_id", deviceID, "error", err)
			continue
		}
		r.logger.Warn("reconcile: purged orphan telemetry series",
			"org_id", subject.OrgID, "device_id", deviceID)
		purged++
	}
	return purged, nil
}
