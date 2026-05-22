package notifications

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// PostgresWebPush implements [WebPushRepository] against PostgreSQL. The db
// package owns the web_push_subscriptions schema and migrations; this adapter
// only issues queries.
type PostgresWebPush struct {
	db *sql.DB
}

// NewPostgresWebPush returns a Postgres-backed WebPushRepository.
func NewPostgresWebPush(db *sql.DB) *PostgresWebPush {
	return &PostgresWebPush{db: db}
}

func (p *PostgresWebPush) Upsert(ctx context.Context, sub *WebPushSubscription) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO web_push_subscriptions (endpoint, user_id, p256dh, auth)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (endpoint) DO UPDATE SET
		   user_id = EXCLUDED.user_id,
		   p256dh = EXCLUDED.p256dh,
		   auth = EXCLUDED.auth`,
		sub.Endpoint, sub.UserID, sub.P256dh, sub.Auth)
	return err
}

func (p *PostgresWebPush) ListForUser(ctx context.Context, userID uuid.UUID) ([]*WebPushSubscription, error) {
	return queryWebPushList(ctx, p.db,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions WHERE user_id = $1`,
		userID)
}

func (p *PostgresWebPush) ListAll(ctx context.Context) ([]*WebPushSubscription, error) {
	return queryWebPushList(ctx, p.db,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions`)
}

func (p *PostgresWebPush) Delete(ctx context.Context, endpoint string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM web_push_subscriptions WHERE endpoint = $1`, endpoint)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSubscriptionNotFound
	}
	return nil
}

func queryWebPushList(ctx context.Context, db *sql.DB, query string, args ...any) ([]*WebPushSubscription, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*WebPushSubscription
	for rows.Next() {
		var sub WebPushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.UserID, &sub.P256dh, &sub.Auth); err != nil {
			return nil, err
		}
		subs = append(subs, &sub)
	}
	return subs, rows.Err()
}
