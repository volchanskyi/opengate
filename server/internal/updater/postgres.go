package updater

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
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
	return p.db.QueryRowContext(ctx,
		`INSERT INTO device_updates (device_id, version, status, error, pushed_at) VALUES ($1, $2, $3, $4, NOW()) RETURNING id`,
		du.DeviceID, du.Version, string(du.Status), du.Error).Scan(&du.ID)
}

func (p *PostgresDeviceUpdates) SetStatus(ctx context.Context, deviceID uuid.UUID, version string, status Status, errMsg string) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE device_updates SET status = $1, error = $2, acked_at = NOW() WHERE device_id = $3 AND version = $4`,
		string(status), errMsg, deviceID, version)
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

func (p *PostgresDeviceUpdates) ListByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, device_id, version, status, error, pushed_at, acked_at FROM device_updates WHERE version = $1 ORDER BY pushed_at DESC`,
		version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var updates []*DeviceUpdate
	for rows.Next() {
		var du DeviceUpdate
		var ackedAt sql.NullTime
		if err := rows.Scan(&du.ID, &du.DeviceID, &du.Version, &du.Status, &du.Error, &du.PushedAt, &ackedAt); err != nil {
			return nil, err
		}
		if ackedAt.Valid {
			t := ackedAt.Time
			du.AckedAt = &t
		}
		updates = append(updates, &du)
	}
	return updates, rows.Err()
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
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (id, token, label, created_by, max_uses, use_count, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		t.ID, t.Token, t.Label, t.CreatedBy, t.MaxUses, t.UseCount, t.ExpiresAt.UTC())
	return err
}

func (p *PostgresEnrollment) GetByToken(ctx context.Context, token string) (*EnrollmentToken, error) {
	t, err := scanEnrollment(p.db.QueryRowContext(ctx,
		`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
		 FROM enrollment_tokens WHERE token = $1`, token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (p *PostgresEnrollment) List(ctx context.Context, createdBy uuid.UUID) ([]*EnrollmentToken, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
		 FROM enrollment_tokens WHERE created_by = $1 ORDER BY created_at DESC`,
		createdBy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*EnrollmentToken
	for rows.Next() {
		t, err := scanEnrollment(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (p *PostgresEnrollment) Delete(ctx context.Context, id uuid.UUID) error {
	return execAffected(p.db.ExecContext(ctx, `DELETE FROM enrollment_tokens WHERE id = $1`, id))
}

func (p *PostgresEnrollment) IncrementUseCount(ctx context.Context, id uuid.UUID) error {
	return execAffected(p.db.ExecContext(ctx,
		`UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = $1`, id))
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
