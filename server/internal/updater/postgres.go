package updater

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// ErrNotFound is returned by SetStatus / IncrementUseCount / Delete / GetByToken
// when the target record does not exist.
var ErrNotFound = errors.New("not found")

// PostgresDeviceUpdates implements [DeviceUpdateRepository] against PostgreSQL.
type PostgresDeviceUpdates struct {
	db *sql.DB
}

// NewPostgresDeviceUpdates returns a Postgres-backed DeviceUpdateRepository.
// The db package owns the device_updates schema and migrations.
func NewPostgresDeviceUpdates(db *sql.DB) *PostgresDeviceUpdates {
	return &PostgresDeviceUpdates{db: db}
}

func (p *PostgresDeviceUpdates) Create(ctx context.Context, du *DeviceUpdate) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`INSERT INTO device_updates (org_id, device_id, version, status, error, pushed_at)
			 VALUES ($1, $2, $3, $4, $5, NOW()) RETURNING id`,
			tenant.OrgID, du.DeviceID, du.Version, string(du.Status), du.Error).Scan(&du.ID)
	})
}

func (p *PostgresDeviceUpdates) SetStatus(ctx context.Context, deviceID uuid.UUID, version string, status Status, errMsg string) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE device_updates SET status = $1, error = $2, acked_at = NOW()
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $3 AND version = $4`,
			string(status), errMsg, deviceID, version)
		return execAffected(res, err)
	})
}

func (p *PostgresDeviceUpdates) ListByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error) {
	var updates []*DeviceUpdate
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, device_id, version, status, error, pushed_at, acked_at
			 FROM device_updates
			 WHERE org_id = current_setting('app.current_org')::uuid AND version = $1
			 ORDER BY pushed_at DESC`,
			version)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var du DeviceUpdate
			var ackedAt sql.NullTime
			if err := rows.Scan(&du.ID, &du.DeviceID, &du.Version, &du.Status, &du.Error, &du.PushedAt, &ackedAt); err != nil {
				return err
			}
			if ackedAt.Valid {
				t := ackedAt.Time
				du.AckedAt = &t
			}
			updates = append(updates, &du)
		}
		return rows.Err()
	})
	return updates, err
}

// PostgresEnrollment implements [EnrollmentTokenRepository] against PostgreSQL.
type PostgresEnrollment struct {
	db *sql.DB
}

// NewPostgresEnrollment returns a Postgres-backed EnrollmentTokenRepository.
func NewPostgresEnrollment(db *sql.DB) *PostgresEnrollment {
	return &PostgresEnrollment{db: db}
}

func (p *PostgresEnrollment) Create(ctx context.Context, t *EnrollmentToken) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO enrollment_tokens (id, org_id, token, label, created_by, max_uses, use_count, expires_at, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())`,
			t.ID, tenant.OrgID, t.Token, t.Label, t.CreatedBy, t.MaxUses, t.UseCount, t.ExpiresAt.UTC())
		return err
	})
}

func (p *PostgresEnrollment) GetByToken(ctx context.Context, token string) (*EnrollmentToken, error) {
	scopeCtx := ctx
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		scopeCtx = dbtx.WithDefaultTenant(ctx, true)
	}
	var t *EnrollmentToken
	err := dbtx.Scoped(scopeCtx, p.db, func(tx *sql.Tx) error {
		var err error
		t, err = scanEnrollment(tx.QueryRowContext(scopeCtx,
			`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
			 FROM enrollment_tokens
			 WHERE (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
			   AND token = $1`, token))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (p *PostgresEnrollment) List(ctx context.Context, createdBy uuid.UUID) ([]*EnrollmentToken, error) {
	var tokens []*EnrollmentToken
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
			 FROM enrollment_tokens
			 WHERE org_id = current_setting('app.current_org')::uuid AND created_by = $1
			 ORDER BY created_at DESC`,
			createdBy)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			t, err := scanEnrollment(rows)
			if err != nil {
				return err
			}
			tokens = append(tokens, t)
		}
		return rows.Err()
	})
	return tokens, err
}

func (p *PostgresEnrollment) Delete(ctx context.Context, id uuid.UUID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return execAffected(tx.ExecContext(ctx,
			`DELETE FROM enrollment_tokens
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`, id))
	})
}

func (p *PostgresEnrollment) IncrementUseCount(ctx context.Context, id uuid.UUID) error {
	scopeCtx := ctx
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		scopeCtx = dbtx.WithDefaultTenant(ctx, true)
	}
	return dbtx.Scoped(scopeCtx, p.db, func(tx *sql.Tx) error {
		return execAffected(tx.ExecContext(scopeCtx,
			`UPDATE enrollment_tokens SET use_count = use_count + 1
			 WHERE (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
			   AND id = $1`, id))
	})
}

// --- internal helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEnrollment(sc rowScanner) (*EnrollmentToken, error) {
	var t EnrollmentToken
	if err := sc.Scan(&t.ID, &t.Token, &t.Label, &t.CreatedBy, &t.MaxUses, &t.UseCount, &t.ExpiresAt, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func execAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
