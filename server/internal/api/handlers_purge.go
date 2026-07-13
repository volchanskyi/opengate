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
	PurgeDevice(ctx context.Context, orgID, deviceID uuid.UUID, by *uuid.UUID) (*lifecycle.PurgeJob, error)
	PurgeOrg(ctx context.Context, orgID uuid.UUID, by *uuid.UUID) (*lifecycle.PurgeJob, error)
	Run(ctx context.Context, job *lifecycle.PurgeJob) error
	RunInBackground(job *lifecycle.PurgeJob)
}

// PurgeJobReader reads persisted purge jobs for the status endpoint.
// *lifecycle.JobStore satisfies it.
type PurgeJobReader interface {
	GetJob(ctx context.Context, id uuid.UUID) (*lifecycle.PurgeJob, error)
}

// PurgeOrg implements StrictServerInterface: an admin-only, tenant-scoped,
// asynchronous purge of an organization's entire telemetry footprint.
func (s *Server) PurgeOrg(ctx context.Context, request PurgeOrgRequestObject) (PurgeOrgResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PurgeOrg403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.purger == nil {
		return PurgeOrg403JSONResponse{Error: "purge not configured"}, nil
	}
	// An admin may only purge within their own tenant.
	claims := ContextClaims(ctx)
	if claims == nil || claims.OrgID != request.OrgId {
		return PurgeOrg403JSONResponse{Error: msgForbidden}, nil
	}

	userID := ContextUserID(ctx)
	job, err := s.purger.PurgeOrg(ctx, request.OrgId, &userID)
	if err != nil {
		return nil, err
	}
	s.purger.RunInBackground(job)
	s.auditLog(ctx, userID, "org.purge", request.OrgId.String(), "tenant telemetry erasure")
	return PurgeOrg202JSONResponse(purgeJobToAPI(job)), nil
}

// GetPurgeJob implements StrictServerInterface: report a purge job's progress.
// Tenant-scoped — a caller only sees their own org's jobs.
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
	if claims == nil || (!claims.IsAdmin && claims.OrgID != job.OrgID) {
		return GetPurgeJob403JSONResponse{Error: msgForbidden}, nil
	}
	return GetPurgeJob200JSONResponse(purgeJobToAPI(job)), nil
}

// purgeJobToAPI maps a domain purge job to its API representation.
func purgeJobToAPI(job *lifecycle.PurgeJob) PurgeJob {
	out := PurgeJob{
		Id:            job.ID,
		OrgId:         job.OrgID,
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
