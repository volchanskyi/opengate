package session

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
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
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO agent_sessions (token, device_id, user_id, created_at) VALUES ($1, $2, $3, NOW())`,
		s.Token, s.DeviceID, s.UserID)
	return err
}

func (p *PostgresSessions) Get(ctx context.Context, token string) (*Session, error) {
	var s Session
	err := p.db.QueryRowContext(ctx,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE token = $1`,
		token).Scan(&s.Token, &s.DeviceID, &s.UserID, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (p *PostgresSessions) Delete(ctx context.Context, token string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM agent_sessions WHERE token = $1`, token)
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
}

func (p *PostgresSessions) ListActiveForDevice(ctx context.Context, deviceID uuid.UUID) ([]*Session, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE device_id = $1`,
		deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.Token, &s.DeviceID, &s.UserID, &s.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &s)
	}
	return sessions, rows.Err()
}
