package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
)

// DevicePurger runs right-to-be-forgotten purges for the delete handlers.
// *lifecycle.Orchestrator satisfies it.
type DevicePurger interface {
	PurgeDevice(ctx context.Context, tenantID, deviceID uuid.UUID, by *uuid.UUID) (*lifecycle.PurgeJob, error)
	PurgeTenant(ctx context.Context, tenantID uuid.UUID, by *uuid.UUID) (*lifecycle.PurgeJob, error)
	Run(ctx context.Context, job *lifecycle.PurgeJob) error
	RunInBackground(job *lifecycle.PurgeJob)
}

// PurgeJobReader reads persisted purge jobs for the status endpoint.
// *lifecycle.JobStore satisfies it.
type PurgeJobReader interface {
	GetJob(ctx context.Context, id uuid.UUID) (*lifecycle.PurgeJob, error)
}

// PurgeTenant implements StrictServerInterface: an admin-only, tenant-scoped,
// asynchronous purge of a tenant's entire telemetry footprint.
func (s *Server) PurgeTenant(ctx context.Context, request PurgeTenantRequestObject) (PurgeTenantResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PurgeTenant403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.purger == nil {
		return PurgeTenant403JSONResponse{Error: msgPurgeNotConfigured}, nil
	}
	// An admin may only purge within their own tenant.
	claims := ContextClaims(ctx)
	if claims == nil || claims.TenantID != request.TenantId {
		return PurgeTenant403JSONResponse{Error: msgForbidden}, nil
	}

	userID := ContextUserID(ctx)
	job, err := s.purger.PurgeTenant(ctx, request.TenantId, &userID)
	if err != nil {
		return nil, err
	}
	s.purger.RunInBackground(job)
	s.auditLog(ctx, userID, "tenant.purge", request.TenantId.String(), "tenant telemetry erasure")
	return PurgeTenant202JSONResponse(purgeJobToAPI(job)), nil
}

// GetPurgeJob implements StrictServerInterface: report a purge job's progress.
// Tenant-scoped — a caller only sees their own tenant's jobs.
func (s *Server) GetPurgeJob(ctx context.Context, request GetPurgeJobRequestObject) (GetPurgeJobResponseObject, error) {
	if s.purgeJobs == nil {
		return GetPurgeJob404JSONResponse{Error: "purge job not found"}, nil
	}
	job, err := s.purgeJobs.GetJob(ctx, request.JobId)
	if err != nil {
		if errors.Is(err, lifecycle.ErrJobNotFound) {
			return GetPurgeJob404JSONResponse{Error: "purge job not found"}, nil
		}
		return nil, err
	}
	claims := ContextClaims(ctx)
	if claims == nil || (!claims.IsAdmin && claims.TenantID != job.TenantID) {
		return GetPurgeJob403JSONResponse{Error: msgForbidden}, nil
	}
	return GetPurgeJob200JSONResponse(purgeJobToAPI(job)), nil
}

// purgeJobToAPI maps a domain purge job to its API representation.
func purgeJobToAPI(job *lifecycle.PurgeJob) PurgeJob {
	out := PurgeJob{
		Id:            job.ID,
		TenantId:      job.TenantID,
		DeviceId:      job.DeviceID,
		Scope:         PurgeJobScope(job.Scope),
		State:         PurgeJobState(job.State),
		VmDeleted:     job.VMDeleted,
		ObjectDeleted: job.ObjectDeleted,
		PgDeleted:     job.PGDeleted,
		Verified:      job.Verified,
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
		CompletedAt:   job.CompletedAt,
	}
	if job.LastError != "" {
		out.LastError = &job.LastError
	}
	return out
}
