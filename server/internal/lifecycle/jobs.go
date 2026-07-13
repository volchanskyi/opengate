package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// JobStore persists purge jobs and their per-store progress on the non-tenant
// purge_jobs table, so a purge resumes idempotently after a server crash and
// the completion record outlives the org's own data.
type JobStore struct {
	db *sql.DB
}

// NewJobStore returns a purge-job store over db.
func NewJobStore(db *sql.DB) *JobStore {
	return &JobStore{db: db}
}

// CreateJob inserts a new purge job in its requested state.
func (s *JobStore) CreateJob(ctx context.Context, job *PurgeJob) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO purge_jobs (id, org_id, device_id, scope, state, requested_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		job.ID, job.OrgID, nullableUUID(job.DeviceID), string(job.Scope), string(job.State), nullableUUID(job.RequestedBy))
	if err != nil {
		return fmt.Errorf("create purge job: %w", err)
	}
	return nil
}

// UpdateProgress persists a job's mid-flight state, per-store flags, and last
// error without stamping completion.
func (s *JobStore) UpdateProgress(ctx context.Context, job *PurgeJob) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE purge_jobs
		 SET state = $2, vm_deleted = $3, object_deleted = $4, pg_deleted = $5,
		     verified = $6, last_error = $7, updated_at = NOW()
		 WHERE id = $1`,
		job.ID, string(job.State), job.VMDeleted, job.ObjectDeleted, job.PGDeleted,
		job.Verified, job.LastError)
	if err != nil {
		return fmt.Errorf("update purge job: %w", err)
	}
	return nil
}

// MarkComplete persists a job's terminal state and stamps completed_at.
func (s *JobStore) MarkComplete(ctx context.Context, job *PurgeJob) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE purge_jobs
		 SET state = $2, vm_deleted = $3, object_deleted = $4, pg_deleted = $5,
		     verified = $6, last_error = $7, updated_at = NOW(), completed_at = NOW()
		 WHERE id = $1`,
		job.ID, string(job.State), job.VMDeleted, job.ObjectDeleted, job.PGDeleted,
		job.Verified, job.LastError)
	if err != nil {
		return fmt.Errorf("complete purge job: %w", err)
	}
	return nil
}

// GetJob loads one job by id.
func (s *JobStore) GetJob(ctx context.Context, id uuid.UUID) (*PurgeJob, error) {
	row := s.db.QueryRowContext(ctx, getPurgeJobSQL, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	return job, err
}

// ListIncomplete returns every job a restarting server must resume, oldest first.
func (s *JobStore) ListIncomplete(ctx context.Context) ([]*PurgeJob, error) {
	rows, err := s.db.QueryContext(ctx, listIncompletePurgeJobsSQL)
	if err != nil {
		return nil, fmt.Errorf("list incomplete purge jobs: %w", err)
	}
	defer rows.Close()
	return scanJobs(rows)
}

// LatestForOrg returns the most recent purge job for an org, or nil when the org
// has never been purged. The status API uses it to report progress.
func (s *JobStore) LatestForOrg(ctx context.Context, orgID uuid.UUID) (*PurgeJob, error) {
	row := s.db.QueryRowContext(ctx, latestPurgeJobForOrgSQL, orgID)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

// ErrJobNotFound is returned when a purge job id does not exist.
var ErrJobNotFound = errors.New("purge job not found")

const (
	purgeJobColumns = `SELECT id, org_id, device_id, scope, state, vm_deleted, object_deleted,
	pg_deleted, verified, requested_by, last_error, created_at, updated_at, completed_at
	FROM purge_jobs`
	getPurgeJobSQL             = purgeJobColumns + ` WHERE id = $1`
	listIncompletePurgeJobsSQL = purgeJobColumns + ` WHERE completed_at IS NULL ORDER BY created_at`
	latestPurgeJobForOrgSQL    = purgeJobColumns + ` WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*PurgeJob, error) {
	var (
		job         PurgeJob
		device      uuid.NullUUID
		requestedBy uuid.NullUUID
		scope       string
		state       string
		completedAt sql.NullTime
	)
	if err := row.Scan(&job.ID, &job.OrgID, &device, &scope, &state, &job.VMDeleted,
		&job.ObjectDeleted, &job.PGDeleted, &job.Verified, &requestedBy, &job.LastError,
		&job.CreatedAt, &job.UpdatedAt, &completedAt); err != nil {
		return nil, err
	}
	job.Scope = Scope(scope)
	job.State = PurgeState(state)
	if device.Valid {
		id := device.UUID
		job.DeviceID = &id
	}
	if requestedBy.Valid {
		id := requestedBy.UUID
		job.RequestedBy = &id
	}
	if completedAt.Valid {
		t := completedAt.Time
		job.CompletedAt = &t
	}
	return &job, nil
}

func scanJobs(rows *sql.Rows) ([]*PurgeJob, error) {
	var out []*PurgeJob
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan purge job: %w", err)
		}
		out = append(out, job)
	}
	return out, rows.Err()
}
