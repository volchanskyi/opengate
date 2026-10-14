package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresSessions implements [Repository] against PostgreSQL.
type PostgresSessions struct {
	db *sql.DB
}

// NewPostgresSessions returns a Postgres-backed [Repository].
func NewPostgresSessions(db *sql.DB) *PostgresSessions {
	return &PostgresSessions{db: db}
}

// uniqueViolation is the Postgres code for a duplicate key, raised by the one-per-device index.
const uniqueViolation = "23505"

// Each read joins the holder so a session is always named; NULLIF falls back to the email.
const (
	getSessionQuery = `SELECT s.token, s.device_id, s.user_id, s.created_at,
	        COALESCE(NULLIF(u.display_name, ''), u.email, '')
	   FROM agent_sessions s LEFT JOIN users u ON u.id = s.user_id
	  WHERE s.tenant_id = current_setting('app.current_tenant')::uuid AND s.token = $1`

	sessionForDeviceQuery = `SELECT s.token, s.device_id, s.user_id, s.created_at,
	        COALESCE(NULLIF(u.display_name, ''), u.email, '')
	   FROM agent_sessions s LEFT JOIN users u ON u.id = s.user_id
	  WHERE s.tenant_id = current_setting('app.current_tenant')::uuid AND s.device_id = $1`
)

func (p *PostgresSessions) Create(ctx context.Context, s *Session) (string, error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return "", dbtx.ErrTenantRequired
	}
	var replaced string
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx,
			`DELETE FROM agent_sessions
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid
			    AND device_id = $1 AND user_id = $2
			 RETURNING token`, s.DeviceID, s.UserID).Scan(&replaced)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO agent_sessions (token, tenant_id, device_id, user_id, created_at)
			 VALUES ($1, $2, $3, $4, NOW())`,
			s.Token, tenant.TenantID, s.DeviceID, s.UserID)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return "", ErrDeviceHeld
	}
	if err != nil {
		return "", err
	}
	return replaced, nil
}

func (p *PostgresSessions) Get(ctx context.Context, token string) (*Session, error) {
	return p.readOne(ctx, getSessionQuery, token)
}

// ForDevice implements [Repository].
func (p *PostgresSessions) ForDevice(ctx context.Context, deviceID uuid.UUID) (*Session, error) {
	return p.readOne(ctx, sessionForDeviceQuery, deviceID)
}

// readOne scans the one session a holder-joined query selects.
func (p *PostgresSessions) readOne(ctx context.Context, query string, arg any) (*Session, error) {
	var s Session
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, query, arg).
			Scan(&s.Token, &s.DeviceID, &s.UserID, &s.CreatedAt, &s.HolderName)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// MarkOutcome implements [Repository]; the bit test and set are one statement, so concurrent
// reports of the same outcome set it once.
func (p *PostgresSessions) MarkOutcome(ctx context.Context, token string, outcome int) (bool, error) {
	var set bool
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM agent_sessions
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND token = $1)`,
			token).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrSessionNotFound
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE agent_sessions SET link_outcomes = link_outcomes | (1 << $2)::smallint
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND token = $1
			    AND link_outcomes & (1 << $2)::smallint = 0`, token, outcome)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		set = n == 1
		return err
	})
	return set, err
}

func (p *PostgresSessions) Delete(ctx context.Context, token string) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM agent_sessions
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND token = $1`, token)
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

// DeleteRelaySession implements [Repository] under an admin scope, keyed by the unique token.
func (p *PostgresSessions) DeleteRelaySession(ctx context.Context, token string) error {
	ctx = dbtx.WithDefaultTenant(ctx, true)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM agent_sessions WHERE token = $1`, token)
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

// DeleteStale implements [Repository] with default-tenant admin rights,
// which the RLS policy widens to every tenant.
func (p *PostgresSessions) DeleteStale(ctx context.Context, cutoff time.Time, keep []string) (int, error) {
	if keep == nil {
		// A nil slice binds as SQL NULL, which makes `token = ANY(NULL)` NULL and spares every row.
		keep = []string{}
	}
	ctx = dbtx.WithDefaultTenant(ctx, true)
	var deleted int64
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM agent_sessions WHERE created_at < $1 AND NOT (token = ANY($2))`,
			cutoff, keep)
		if err != nil {
			return err
		}
		deleted, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return 0, err
	}
	return int(deleted), nil
}

func (p *PostgresSessions) ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error) {
	held, err := p.ForDevice(ctx, deviceID)
	if errors.Is(err, ErrSessionNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []*Session{held}, nil
}
