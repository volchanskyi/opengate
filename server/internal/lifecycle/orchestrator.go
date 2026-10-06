package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// VerifyConfig bounds the post-delete emptiness check: up to MaxAttempts counts, Interval apart.
type VerifyConfig struct {
	MaxAttempts int
	Interval    time.Duration
}

// DefaultVerifyConfig returns the production emptiness-check budget of a few quick retries.
func DefaultVerifyConfig() VerifyConfig {
	return VerifyConfig{MaxAttempts: 5, Interval: 500 * time.Millisecond}
}

// Orchestrator drives purges: it tombstones the subject, erases it across VictoriaMetrics,
// cold-tier objects and Postgres, and persists per-store progress so a crash resumes.
type Orchestrator struct {
	tombstones *TombstoneStore
	jobs       *JobStore
	series     SeriesPurger
	objects    ObjectPurger // optional; nil when no cold tier
	pg         PGPurger
	edge       EdgeDeregistrar // optional; nil in tests without an agent server
	verify     VerifyConfig
	logger     *slog.Logger
}

// OrchestratorConfig gathers the orchestrator's dependencies.
type OrchestratorConfig struct {
	Tombstones *TombstoneStore
	Jobs       *JobStore
	Series     SeriesPurger
	Objects    ObjectPurger
	PG         PGPurger
	Edge       EdgeDeregistrar
	Verify     VerifyConfig
	Logger     *slog.Logger
}

// NewOrchestrator builds an orchestrator. A zero Verify uses DefaultVerifyConfig;
// a nil Logger uses the default slog logger.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	verify := cfg.Verify
	if verify.MaxAttempts <= 0 {
		verify = DefaultVerifyConfig()
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Orchestrator{
		tombstones: cfg.Tombstones,
		jobs:       cfg.Jobs,
		series:     cfg.Series,
		objects:    cfg.Objects,
		pg:         cfg.PG,
		edge:       cfg.Edge,
		verify:     verify,
		logger:     logger,
	}
}

// PurgeDevice tombstones a device, deregisters its agent and creates a purge job unrun.
func (o *Orchestrator) PurgeDevice(ctx context.Context, tenantID, deviceID uuid.UUID, by *uuid.UUID) (*PurgeJob, error) {
	job := &PurgeJob{
		ID:          uuid.New(),
		TenantID:    tenantID,
		DeviceID:    &deviceID,
		Scope:       ScopeDevice,
		State:       StateRequested,
		RequestedBy: by,
	}
	if err := o.jobs.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	if err := o.applyTombstone(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// PurgeTenant records a tenant-wide tombstone, deregisters every connected agent in
// the tenant, and creates a tenant-scoped purge job.
func (o *Orchestrator) PurgeTenant(ctx context.Context, tenantID uuid.UUID, by *uuid.UUID) (*PurgeJob, error) {
	job := &PurgeJob{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Scope:       ScopeTenant,
		State:       StateRequested,
		RequestedBy: by,
	}
	if err := o.jobs.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	if err := o.applyTombstone(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// The deny-list entry is written before the edge is deregistered, so no reconnecting agent
// re-creates the subject's data.
func (o *Orchestrator) applyTombstone(ctx context.Context, job *PurgeJob) error {
	if job.Scope == ScopeTenant {
		return o.applyTenantTombstone(ctx, job)
	}
	if err := o.tombstones.TombstoneDevice(ctx, job.TenantID, *job.DeviceID, job.RequestedBy); err != nil {
		return err
	}
	if o.edge != nil {
		o.edge.DeregisterAgent(ctx, *job.DeviceID)
	}
	return nil
}

// Each device gets its own deny-list entry, so an offline device is rejected by id on reconnect
// after its Postgres row is gone.
func (o *Orchestrator) applyTenantTombstone(ctx context.Context, job *PurgeJob) error {
	if err := o.tombstones.TombstoneTenant(ctx, job.TenantID, job.RequestedBy); err != nil {
		return err
	}
	ids, err := o.pg.ListTenantDeviceIDs(ctx, job.TenantID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := o.tombstones.TombstoneDevice(ctx, job.TenantID, id, job.RequestedBy); err != nil {
			return err
		}
	}
	if o.edge != nil {
		o.edge.DeregisterTenant(ctx, job.TenantID)
	}
	return nil
}

// Run executes the purge stages in order, skipping stages the job's persisted flags mark done.
// Postgres rows go last so labels and foreign keys stay available while the other stores drain.
func (o *Orchestrator) Run(ctx context.Context, job *PurgeJob) error {
	if err := o.stageVMDelete(ctx, job); err != nil {
		return err
	}
	if err := o.stageObjectDelete(ctx, job); err != nil {
		return err
	}
	if err := o.stagePostgresDelete(ctx, job); err != nil {
		return err
	}
	return o.stageVerifyComplete(ctx, job)
}

func (o *Orchestrator) stageVMDelete(ctx context.Context, job *PurgeJob) error {
	if job.VMDeleted {
		return nil
	}
	if err := o.series.DeleteSeries(ctx, job.TenantID, job.DeviceID); err != nil {
		return o.fail(ctx, job, "vm-delete", err)
	}
	job.VMDeleted = true
	job.State = StateCentralLogicalComplete
	job.LastError = ""
	return o.jobs.UpdateProgress(ctx, job)
}

func (o *Orchestrator) stageObjectDelete(ctx context.Context, job *PurgeJob) error {
	if job.ObjectDeleted {
		return nil
	}
	if o.objects != nil {
		job.State = StateObjectDeletePending
		_ = o.jobs.UpdateProgress(ctx, job)
		if err := o.objects.DeletePrefix(ctx, job.TenantID, job.DeviceID); err != nil {
			return o.fail(ctx, job, "object-delete", err)
		}
	}
	job.ObjectDeleted = true
	return o.jobs.UpdateProgress(ctx, job)
}

func (o *Orchestrator) stagePostgresDelete(ctx context.Context, job *PurgeJob) error {
	if job.PGDeleted {
		return nil
	}
	if err := o.deletePostgres(ctx, job); err != nil {
		return o.fail(ctx, job, "postgres-delete", err)
	}
	job.PGDeleted = true
	return o.jobs.UpdateProgress(ctx, job)
}

func (o *Orchestrator) stageVerifyComplete(ctx context.Context, job *PurgeJob) error {
	if !job.Verified {
		empty, err := o.verifyEmpty(ctx, job)
		if err != nil {
			return o.fail(ctx, job, "verify", err)
		}
		if !empty {
			job.State = StateCentralPhysicalPending
			job.LastError = "vm series awaiting compaction"
			return o.jobs.UpdateProgress(ctx, job)
		}
		job.Verified = true
	}
	job.State = StateComplete
	job.LastError = ""
	return o.jobs.MarkComplete(ctx, job)
}

// RunInBackground runs a purge on a detached context bounded by a ten-minute timeout.
func (o *Orchestrator) RunInBackground(job *PurgeJob) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := o.Run(ctx, job); err != nil {
			o.logger.Error("background purge failed", "job_id", job.ID, "tenant_id", job.TenantID, "error", err)
		}
	}()
}

// Resume re-applies the tombstone and re-runs every incomplete job after a server restart.
func (o *Orchestrator) Resume(ctx context.Context) error {
	jobs, err := o.jobs.ListIncomplete(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := o.applyTombstone(ctx, job); err != nil {
			o.logger.Error("resume: re-tombstone failed", "job_id", job.ID, "error", err)
			continue
		}
		if err := o.Run(ctx, job); err != nil {
			o.logger.Error("resume: run failed", "job_id", job.ID, "error", err)
		}
	}
	return nil
}

func (o *Orchestrator) deletePostgres(ctx context.Context, job *PurgeJob) error {
	if job.Scope == ScopeTenant {
		_, err := o.pg.DeleteTenantDevices(ctx, job.TenantID)
		return err
	}
	return o.pg.DeleteDevice(ctx, job.TenantID, *job.DeviceID)
}

func (o *Orchestrator) verifyEmpty(ctx context.Context, job *PurgeJob) (bool, error) {
	attempts := max(o.verify.MaxAttempts, 1)
	for i := range attempts {
		n, err := o.series.CountSeries(ctx, job.TenantID, job.DeviceID)
		if err != nil {
			return false, err
		}
		if n == 0 {
			return true, nil
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(o.verify.Interval):
			}
		}
	}
	return false, nil
}

func (o *Orchestrator) fail(ctx context.Context, job *PurgeJob, stage string, cause error) error {
	job.LastError = stage + ": " + cause.Error()
	if err := o.jobs.UpdateProgress(ctx, job); err != nil {
		o.logger.Error("persist purge failure", "job_id", job.ID, "error", err)
	}
	return fmt.Errorf("purge %s: %w", stage, cause)
}
