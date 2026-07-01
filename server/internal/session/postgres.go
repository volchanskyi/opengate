package session

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresSessions implements [Repository] against PostgreSQL. The db
// package owns the agent_sessions schema and migrations; this adapter only
// issues queries.
type PostgresSessions struct {
	db *sql.DB
}

// NewPostgresSessions returns a Postgres-backed [Repository].
func NewPostgresSessions(db *sql.DB) *PostgresSessions {
	return &PostgresSessions{db: db}
}

func (p *PostgresSessions) Create(ctx context.Context, s *Session) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO agent_sessions (token, org_id, device_id, user_id, created_at)
			 VALUES ($1, $2, $3, $4, NOW())`,
			s.Token, tenant.OrgID, s.DeviceID, s.UserID)
		return err
	})
}

func (p *PostgresSessions) Get(ctx context.Context, token string) (*Session, error) {
	var s Session
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT token, device_id, user_id, created_at FROM agent_sessions
			 WHERE org_id = current_setting('app.current_org')::uuid AND token = $1`,
			token).Scan(&s.Token, &s.DeviceID, &s.UserID, &s.CreatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (p *PostgresSessions) Delete(ctx context.Context, token string) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM agent_sessions
			 WHERE org_id = current_setting('app.current_org')::uuid AND token = $1`, token)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrSessionNotFound
		}
		return nil
	})
}

func (p *PostgresSessions) ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error) {
	var sessions []*Session
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT token, device_id, user_id, created_at FROM agent_sessions
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1`,
			deviceID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var s Session
			if err := rows.Scan(&s.Token, &s.DeviceID, &s.UserID, &s.CreatedAt); err != nil {
				return err
			}
			sessions = append(sessions, &s)
		}
		return rows.Err()
	})
	return sessions, err
}
