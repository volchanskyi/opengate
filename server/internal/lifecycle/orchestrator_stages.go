package lifecycle

import (
	"context"
	"time"
)

// Postgres rows go last so labels and foreign keys stay available while the other stores drain.
func (o *Orchestrator) runStages(ctx context.Context, job *PurgeJob) error {
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
	// The deny-list entry is in place, so the writes still to finish are the last that can land.
	if o.writes != nil {
		if err := o.writes.WaitForWrites(ctx, job.TenantID, job.DeviceID); err != nil {
			return o.fail(ctx, job, "vm-delete", err)
		}
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
