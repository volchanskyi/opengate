package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ConsoleIdleLimit and ConsoleTotalLimit end a console session 30 min after its last request and
// 8 h after it started, as OWASP's session guidance sets them.
const (
	ConsoleIdleLimit  = 30 * time.Minute
	ConsoleTotalLimit = 8 * time.Hour
)

// ErrConsoleSessionNotFound is returned for a session id no sign-in recorded, or one signed out.
var ErrConsoleSessionNotFound = errors.New("console session not found")

// ConsoleSession is one console sign-in, keyed by the SHA-256 of the id its cookie carries.
type ConsoleSession struct {
	IDHash   [32]byte
	UserID   UserID
	TenantID uuid.UUID
	Email    string
	IsAdmin  bool
	// CSRFToken is the forged-request value, which reaches the page in a response body only.
	CSRFToken  string
	StartedAt  time.Time
	LastSeenAt time.Time
}

// ConsoleSessionRepository stores console sign-ins.
type ConsoleSessionRepository interface {
	Create(ctx context.Context, s *ConsoleSession) error
	Get(ctx context.Context, idHash [32]byte) (*ConsoleSession, error)
	// Touch records a request at `at`, which renews the session's idle clock.
	Touch(ctx context.Context, idHash [32]byte, at time.Time) error
	Delete(ctx context.Context, idHash [32]byte) error
	// DeleteExpired removes the sessions idle since before idleBefore or started before
	// startedBefore, and returns how many it removed.
	DeleteExpired(ctx context.Context, idleBefore, startedBefore time.Time) (int64, error)
}

// PostgresConsoleSessions implements [ConsoleSessionRepository] against PostgreSQL. The table sits
// outside tenant row security, since a cookie is read before its tenant is known.
type PostgresConsoleSessions struct {
	db *sql.DB
}

// NewPostgresConsoleSessions returns a Postgres-backed [ConsoleSessionRepository].
func NewPostgresConsoleSessions(db *sql.DB) *PostgresConsoleSessions {
	return &PostgresConsoleSessions{db: db}
}

func (p *PostgresConsoleSessions) Create(ctx context.Context, s *ConsoleSession) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO console_sessions
		   (id_hash, user_id, tenant_id, email, is_admin, csrf_token, started_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.IDHash[:], s.UserID, s.TenantID, s.Email, s.IsAdmin, s.CSRFToken, s.StartedAt, s.LastSeenAt)
	if err != nil {
		return fmt.Errorf("create console session: %w", err)
	}
	return nil
}

func (p *PostgresConsoleSessions) Get(ctx context.Context, idHash [32]byte) (*ConsoleSession, error) {
	s := ConsoleSession{IDHash: idHash}
	err := p.db.QueryRowContext(ctx,
		`SELECT user_id, tenant_id, email, is_admin, csrf_token, started_at, last_seen_at
		 FROM console_sessions WHERE id_hash = $1`, idHash[:]).
		Scan(&s.UserID, &s.TenantID, &s.Email, &s.IsAdmin, &s.CSRFToken, &s.StartedAt, &s.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConsoleSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get console session: %w", err)
	}
	return &s, nil
}

func (p *PostgresConsoleSessions) Touch(ctx context.Context, idHash [32]byte, at time.Time) error {
	if _, err := p.db.ExecContext(ctx,
		`UPDATE console_sessions SET last_seen_at = $2 WHERE id_hash = $1`, idHash[:], at); err != nil {
		return fmt.Errorf("touch console session: %w", err)
	}
	return nil
}

func (p *PostgresConsoleSessions) Delete(ctx context.Context, idHash [32]byte) error {
	if _, err := p.db.ExecContext(ctx,
		`DELETE FROM console_sessions WHERE id_hash = $1`, idHash[:]); err != nil {
		return fmt.Errorf("delete console session: %w", err)
	}
	return nil
}

func (p *PostgresConsoleSessions) DeleteExpired(ctx context.Context, idleBefore, startedBefore time.Time) (int64, error) {
	res, err := p.db.ExecContext(ctx,
		`DELETE FROM console_sessions WHERE last_seen_at < $1 OR started_at < $2`,
		idleBefore, startedBefore)
	if err != nil {
		return 0, fmt.Errorf("sweep console sessions: %w", err)
	}
	return res.RowsAffected()
}
