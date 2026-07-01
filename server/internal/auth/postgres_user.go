package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresUsers implements [UserRepository] against PostgreSQL. The db
// package owns the users schema and migrations; this adapter only issues
// queries.
type PostgresUsers struct {
	db *sql.DB
}

// NewPostgresUsers returns a Postgres-backed [UserRepository].
func NewPostgresUsers(db *sql.DB) *PostgresUsers {
	return &PostgresUsers{db: db}
}

func (p *PostgresUsers) Upsert(ctx context.Context, u *User) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	u.OrgID = tenant.OrgID
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, org_id, email, password_hash, display_name, is_admin, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
			 ON CONFLICT (id) DO UPDATE SET
			   org_id = EXCLUDED.org_id,
			   email = EXCLUDED.email,
			   password_hash = EXCLUDED.password_hash,
			   display_name = EXCLUDED.display_name,
			   is_admin = EXCLUDED.is_admin,
			   updated_at = NOW()`,
			u.ID, tenant.OrgID, u.Email, u.PasswordHash, u.DisplayName, u.IsAdmin)
		return err
	})
}

func (p *PostgresUsers) Get(ctx context.Context, id UserID) (*User, error) {
	var user *User
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		user, err = scanOneUser(tx.QueryRowContext(ctx,
			`SELECT id, org_id, email, password_hash, display_name, is_admin, created_at, updated_at
			 FROM users
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`,
			id))
		return err
	})
	return user, normalizeUserErr(err)
}

func (p *PostgresUsers) GetByEmail(ctx context.Context, email string) (*User, error) {
	scopeCtx := ctx
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		// Login is a pre-tenant lookup. Use policy-based admin scope, not BYPASSRLS.
		scopeCtx = dbtx.WithDefaultTenant(ctx, true)
	}
	var user *User
	err := dbtx.Scoped(scopeCtx, p.db, func(tx *sql.Tx) error {
		var err error
		user, err = scanOneUser(tx.QueryRowContext(scopeCtx,
			`SELECT id, org_id, email, password_hash, display_name, is_admin, created_at, updated_at
			 FROM users
			 WHERE (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
			   AND email = $1`,
			email))
		return err
	})
	return user, normalizeUserErr(err)
}

func (p *PostgresUsers) List(ctx context.Context) ([]*User, error) {
	var users []*User
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, org_id, email, password_hash, display_name, is_admin, created_at, updated_at
			 FROM users
			 WHERE org_id = current_setting('app.current_org')::uuid`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
				return err
			}
			users = append(users, &u)
		}
		return rows.Err()
	})
	return users, err
}

func (p *PostgresUsers) Delete(ctx context.Context, id UserID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM users WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrUserNotFound
		}
		return nil
	})
}

func scanOneUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func normalizeUserErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	return err
}
