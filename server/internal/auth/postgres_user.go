package auth

import (
	"context"
	"database/sql"
	"errors"
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
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		 ON CONFLICT (id) DO UPDATE SET
		   email = EXCLUDED.email,
		   password_hash = EXCLUDED.password_hash,
		   display_name = EXCLUDED.display_name,
		   is_admin = EXCLUDED.is_admin,
		   updated_at = NOW()`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.IsAdmin)
	return err
}

func (p *PostgresUsers) Get(ctx context.Context, id UserID) (*User, error) {
	return scanOneUser(p.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE id = $1`,
		id))
}

func (p *PostgresUsers) GetByEmail(ctx context.Context, email string) (*User, error) {
	return scanOneUser(p.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE email = $1`,
		email))
}

func (p *PostgresUsers) List(ctx context.Context) ([]*User, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (p *PostgresUsers) Delete(ctx context.Context, id UserID) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
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
}

func scanOneUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
