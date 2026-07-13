package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// VerifyConfig bounds the post-delete VictoriaMetrics emptiness check. VM
// delete-series is async, so a purge may reach verification before the series
// have merged away; the orchestrator retries up to MaxAttempts, then leaves the
// job in the physical-compaction-pending state for the reconciliation sweep to
// finish.
type VerifyConfig struct {
	MaxAttempts int
	Interval    time.Duration
}

// DefaultVerifyConfig is the production emptiness-check budget: a handful of
// quick retries so a synchronous device delete usually completes in-request,
// while a slow compaction falls through to the periodic sweep.
func DefaultVerifyConfig() VerifyConfig {
	return VerifyConfig{MaxAttempts: 5, Interval: 500 * time.Millisecond}
}

// Orchestrator drives right-to-be-forgotten purges: it tombstones the subject,
// fans the erasure out across VictoriaMetrics, optional cold-tier objects, and
// Postgres, verifies central emptiness, and persists per-store progress so a
// crash mid-purge resumes idempotently.
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

// OrchestratorConfig groups the orchestrator's dependencies.
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

// PurgeDevice records a device tombstone, deregisters the connected agent, and
// creates a purge job. It does not run the fan-out — the caller runs it
// synchronously (device delete) or in the background (returned job).
func (o *Orchestrator) PurgeDevice(ctx context.Context, orgID, deviceID uuid.UUID, by *uuid.UUID) (*PurgeJob, error) {
	job := &PurgeJob{
		ID:          uuid.New(),
		OrgID:       orgID,
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

// PurgeOrg records an org-wide tombstone, deregisters every connected agent in
// the org, and creates an org-scoped purge job.
func (o *Orchestrator) PurgeOrg(ctx context.Context, orgID uuid.UUID, by *uuid.UUID) (*PurgeJob, error) {
	job := &PurgeJob{
		ID:          uuid.New(),
		OrgID:       orgID,
		Scope:       ScopeOrg,
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

// applyTombstone writes the deny-list entry first, then deregisters the edge so
// no live stream, in-flight backfill, or reconnecting agent can re-create the
// subject's data. Idempotent, so a resumed job re-applies it safely.
func (o *Orchestrator) applyTombstone(ctx context.Context, job *PurgeJob) error {
	if job.Scope == ScopeOrg {
		return o.applyOrgTombstone(ctx, job)
	}
	if err := o.tombstones.TombstoneDevice(ctx, job.OrgID, *job.DeviceID, job.RequestedBy); err != nil {
		return err
	}
	if o.edge != nil {
		o.edge.DeregisterAgent(ctx, *job.DeviceID)
	}
	return nil
}

// applyOrgTombstone tombstones the org and every device in it. Recording a
// per-device deny-list entry (not only the org one) means a device that was
// offline during the purge is still rejected by its own id when it reconnects,
// after its Postgres row — and thus its org linkage — is gone. Best-effort
// enumeration: once the device rows are deleted a resumed job simply finds none,
// having already persisted their tombstones on the first pass.
func (o *Orchestrator) applyOrgTombstone(ctx context.Context, job *PurgeJob) error {
	if err := o.tombstones.TombstoneOrg(ctx, job.OrgID, job.RequestedBy); err != nil {
		return err
	}
	ids, err := o.pg.ListOrgDeviceIDs(ctx, job.OrgID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := o.tombstones.TombstoneDevice(ctx, job.OrgID, id, job.RequestedBy); err != nil {
			return err
		}
	}
	if o.edge != nil {
		o.edge.DeregisterOrg(ctx, job.OrgID)
	}
	return nil
}

// Run executes the purge fan-out for a job. It is idempotent and resumable: each
// stage is guarded by the job's persisted per-store flag, so a resumed job skips
// finished stages. Strict ordering — VictoriaMetrics delete, cold-tier objects,
// then Postgres rows last — keeps labels and FKs alive while the stores drain.
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

// stageVMDelete issues the VictoriaMetrics delete-series (logical delete; ingest
// is already blocked by the tombstone).
func (o *Orchestrator) stageVMDelete(ctx context.Context, job *PurgeJob) error {
	if job.VMDeleted {
		return nil
	}
	if err := o.series.DeleteSeries(ctx, job.OrgID, job.DeviceID); err != nil {
		return o.fail(ctx, job, "vm-delete", err)
	}
	job.VMDeleted = true
	job.State = StateCentralLogicalComplete
	job.LastError = ""
	return o.jobs.UpdateProgress(ctx, job)
}

// stageObjectDelete removes cold-tier object prefixes, or marks the stage done
// when no cold tier is wired.
func (o *Orchestrator) stageObjectDelete(ctx context.Context, job *PurgeJob) error {
	if job.ObjectDeleted {
		return nil
	}
	if o.objects != nil {
		job.State = StateObjectDeletePending
		_ = o.jobs.UpdateProgress(ctx, job)
		if err := o.objects.DeletePrefix(ctx, job.OrgID, job.DeviceID); err != nil {
			return o.fail(ctx, job, "object-delete", err)
		}
	}
	job.ObjectDeleted = true
	return o.jobs.UpdateProgress(ctx, job)
}

// stagePostgresDelete removes the device/org descriptive rows last, cascading
// their telemetry.
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

// stageVerifyComplete gates completion on VictoriaMetrics emptiness; an
// unverified job is left pending compaction for the reconciliation sweep.
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

// RunInBackground runs a purge on a detached context, for the async tenant case.
// A long-lived timeout bounds the whole fan-out including verify retries.
func (o *Orchestrator) RunInBackground(job *PurgeJob) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := o.Run(ctx, job); err != nil {
			o.logger.Error("background purge failed", "job_id", job.ID, "org_id", job.OrgID, "error", err)
		}
	}()
}

// Resume re-applies the tombstone and re-runs every incomplete job after a
// server restart. Each job is idempotent, so re-running finished stages is safe.
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
	if job.Scope == ScopeOrg {
		_, err := o.pg.DeleteOrgDevices(ctx, job.OrgID)
		return err
	}
	return o.pg.DeleteDevice(ctx, job.OrgID, *job.DeviceID)
}

// verifyEmpty polls VictoriaMetrics until no series match the subject or the
// attempt budget is exhausted.
func (o *Orchestrator) verifyEmpty(ctx context.Context, job *PurgeJob) (bool, error) {
	attempts := max(o.verify.MaxAttempts, 1)
	for i := range attempts {
		n, err := o.series.CountSeries(ctx, job.OrgID, job.DeviceID)
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
