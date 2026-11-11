package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	writes     WriteWaiter  // optional; nil when no writer runs
	objects    ObjectPurger // optional; nil when no cold tier
	pg         PGPurger
	edge       EdgeDeregistrar // optional; nil in tests without an agent server
	verify     VerifyConfig
	logger     *slog.Logger

	// running maps each record being run to the copy its runner holds, so a second caller with
	// another copy of the same record leaves it alone.
	mu      sync.Mutex
	running map[uuid.UUID]*PurgeJob
}

// errPurgeRunning is returned by Run for a record another caller is running.
var errPurgeRunning = errors.New("purge job is already running")

// OrchestratorConfig gathers the orchestrator's dependencies.
type OrchestratorConfig struct {
	Tombstones *TombstoneStore
	Jobs       *JobStore
	Series     SeriesPurger
	Writes     WriteWaiter
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
		writes:     cfg.Writes,
		objects:    cfg.Objects,
		pg:         cfg.PG,
		edge:       cfg.Edge,
		verify:     verify,
		logger:     logger,
		running:    make(map[uuid.UUID]*PurgeJob),
	}
}

// claim reserves the record for the caller holding job and reports whether it was free.
func (o *Orchestrator) claim(job *PurgeJob) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if holder, held := o.running[job.ID]; held {
		return holder == job
	}
	o.running[job.ID] = job
	return true
}

func (o *Orchestrator) release(job *PurgeJob) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.running, job.ID)
}

// PurgeDevice tombstones a device, deregisters its agent and creates a purge job unrun, held for
// the caller's Run.
func (o *Orchestrator) PurgeDevice(ctx context.Context, tenantID, deviceID uuid.UUID, by *uuid.UUID) (*PurgeJob, error) {
	job := &PurgeJob{
		ID:          uuid.New(),
		TenantID:    tenantID,
		DeviceID:    &deviceID,
		Scope:       ScopeDevice,
		State:       StateRequested,
		RequestedBy: by,
	}
	return o.create(ctx, job)
}

// PurgeTenant records a tenant-wide tombstone, deregisters every connected agent in the tenant,
// and creates a tenant-scoped purge job held for the caller's Run.
func (o *Orchestrator) PurgeTenant(ctx context.Context, tenantID uuid.UUID, by *uuid.UUID) (*PurgeJob, error) {
	job := &PurgeJob{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Scope:       ScopeTenant,
		State:       StateRequested,
		RequestedBy: by,
	}
	return o.create(ctx, job)
}

// create records a new job claimed for its caller, so a retry pass never takes a job the request
// that made it is about to run.
func (o *Orchestrator) create(ctx context.Context, job *PurgeJob) (*PurgeJob, error) {
	o.claim(job)
	if err := o.jobs.CreateJob(ctx, job); err != nil {
		o.release(job)
		return nil, err
	}
	if err := o.applyTombstone(ctx, job); err != nil {
		o.release(job)
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
// A job another caller is running returns errPurgeRunning.
func (o *Orchestrator) Run(ctx context.Context, job *PurgeJob) error {
	if !o.claim(job) {
		return errPurgeRunning
	}
	defer o.release(job)
	return o.runStages(ctx, job)
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

// Resume re-applies the tombstone and re-runs every incomplete job no other caller is running,
// from the stage it reached, and returns how many stay incomplete. It runs at start-up and hourly.
func (o *Orchestrator) Resume(ctx context.Context) (int, error) {
	jobs, err := o.jobs.ListIncomplete(ctx)
	if err != nil {
		return 0, err
	}
	unfinished := 0
	for _, job := range jobs {
		if !o.resumeOne(ctx, job) {
			unfinished++
		}
	}
	return unfinished, nil
}

// resumeOne reports whether the job is complete once its pass ends.
func (o *Orchestrator) resumeOne(ctx context.Context, job *PurgeJob) bool {
	if !o.claim(job) {
		return false
	}
	defer o.release(job)
	if err := o.applyTombstone(ctx, job); err != nil {
		o.logger.Error("resume: re-tombstone failed", "job_id", job.ID, "error", err)
		return false
	}
	if err := o.runStages(ctx, job); err != nil {
		o.logger.Error("resume: run failed", "job_id", job.ID, "error", err)
		return false
	}
	return job.State == StateComplete
}

func (o *Orchestrator) fail(ctx context.Context, job *PurgeJob, stage string, cause error) error {
	job.LastError = stage + ": " + cause.Error()
	if err := o.jobs.UpdateProgress(ctx, job); err != nil {
		o.logger.Error("persist purge failure", "job_id", job.ID, "error", err)
	}
	return fmt.Errorf("purge %s: %w", stage, cause)
}
