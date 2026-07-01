package audit

import (
	"context"
	"database/sql"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
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
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO audit_events (org_id, user_id, action, target, details, created_at)
			 VALUES ($1, $2, $3, $4, $5, NOW())`,
			tenant.OrgID, event.UserID, event.Action, event.Target, event.Details)
		return err
	})
}

func (p *Postgres) Query(ctx context.Context, q Query) ([]*Event, error) {
	// Sentinel-parameter pattern: always pass every filter so the query is a
	// single static literal (avoids go:S2077 dynamic-SQL hotspot).
	var userID any
	if q.UserID != nil {
		userID = *q.UserID
	}

	var events []*Event
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, user_id, action, target, details, created_at FROM audit_events
			 WHERE org_id = current_setting('app.current_org')::uuid
			   AND ($1::uuid IS NULL OR user_id = $1)
			   AND ($2 = '' OR action = $2)
			 ORDER BY created_at DESC, id DESC
			 LIMIT NULLIF($3, 0) OFFSET $4`,
			userID, q.Action, q.Limit, q.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.Details, &e.CreatedAt); err != nil {
				return err
			}
			events = append(events, &e)
		}
		return rows.Err()
	})
	return events, err
}
