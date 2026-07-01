package notifications

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
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
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO web_push_subscriptions (endpoint, org_id, user_id, p256dh, auth)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (endpoint) DO UPDATE SET
			   org_id = EXCLUDED.org_id,
			   user_id = EXCLUDED.user_id,
			   p256dh = EXCLUDED.p256dh,
			   auth = EXCLUDED.auth`,
			sub.Endpoint, tenant.OrgID, sub.UserID, sub.P256dh, sub.Auth)
		return err
	})
}

func (p *PostgresWebPush) ListForUser(ctx context.Context, userID uuid.UUID) ([]*WebPushSubscription, error) {
	var subs []*WebPushSubscription
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		subs, err = queryWebPushList(ctx, tx,
			`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions
			 WHERE org_id = current_setting('app.current_org')::uuid AND user_id = $1`,
			userID)
		return err
	})
	return subs, err
}

func (p *PostgresWebPush) ListAll(ctx context.Context) ([]*WebPushSubscription, error) {
	var subs []*WebPushSubscription
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		subs, err = queryWebPushList(ctx, tx,
			`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions
			 WHERE org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean`)
		return err
	})
	return subs, err
}

func (p *PostgresWebPush) Delete(ctx context.Context, endpoint string) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM web_push_subscriptions
			 WHERE org_id = current_setting('app.current_org')::uuid AND endpoint = $1`, endpoint)
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
	})
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryWebPushList(ctx context.Context, db queryer, query string, args ...any) ([]*WebPushSubscription, error) {
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
