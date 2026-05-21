package audit

import (
	"context"
	"database/sql"
)

// Postgres implements [Repository] against a PostgreSQL database.
type Postgres struct {
	db *sql.DB
}

// NewPostgres returns a Postgres-backed Repository using the provided handle.
// The db package owns the audit_events schema and migrations.
func NewPostgres(db *sql.DB) *Postgres {
	return &Postgres{db: db}
}

func (p *Postgres) Write(ctx context.Context, event *Event) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO audit_events (user_id, action, target, details, created_at) VALUES ($1, $2, $3, $4, NOW())`,
		event.UserID, event.Action, event.Target, event.Details)
	return err
}

func (p *Postgres) Query(ctx context.Context, q Query) ([]*Event, error) {
	// Sentinel-parameter pattern: always pass every filter so the query is a
	// single static literal (avoids go:S2077 dynamic-SQL hotspot).
	var userID any
	if q.UserID != nil {
		userID = *q.UserID
	}

	rows, err := p.db.QueryContext(ctx,
		`SELECT id, user_id, action, target, details, created_at FROM audit_events
		 WHERE ($1::uuid IS NULL OR user_id = $1)
		   AND ($2 = '' OR action = $2)
		 ORDER BY created_at DESC, id DESC
		 LIMIT NULLIF($3, 0) OFFSET $4`,
		userID, q.Action, q.Limit, q.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}
