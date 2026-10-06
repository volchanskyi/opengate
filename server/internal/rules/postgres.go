package rules

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// Statements run in a tenant-scoped transaction and name the tenant, except the platform-wide
// fleet coverage read; the organization predicate separates customers inside one tenant.

// scopedToTenant is the tenant predicate the reading, updating and deleting statements carry.
const scopedToTenant = `tenant_id = current_setting('app.current_tenant')::uuid`

// levelNames maps a tenancy ladder level to the value stored in the level column.
var levelNames = map[settings.Level]string{
	settings.LevelDevice:       "device",
	settings.LevelSite:         "site",
	settings.LevelOrganization: "organization",
	settings.LevelTenant:       "tenant",
}

// levelByName inverts levelNames for rows read back.
var levelByName = func() map[string]settings.Level {
	out := make(map[string]settings.Level, len(levelNames))
	for level, name := range levelNames {
		out[name] = level
	}
	return out
}()

// Store is the Postgres home of everything about a rule that changes without a
// deploy.
type Store struct {
	db *sql.DB
}

// NewStore returns a Postgres-backed rule store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// callerTenant returns the tenant the caller is acting as, which every write
// stamps on the row it creates.
func callerTenant(ctx context.Context) (uuid.UUID, error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return uuid.Nil, dbtx.ErrTenantRequired
	}
	return tenant.TenantID, nil
}

// exec runs one statement inside a tenant-scoped transaction. what names the
// operation for the error an operator eventually reads.
func (s *Store) exec(ctx context.Context, what, query string, args ...any) error {
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	})
}

// affected runs one statement in a tenant-scoped transaction and reports the rows it touched.
// Zero rows is how a statement whose own predicate refused the write reports it.
func (s *Store) affected(ctx context.Context, what, query string, args ...any) (int64, error) {
	var rows int64
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	})
	return rows, err
}

// eachRow runs one query inside a tenant-scoped transaction and hands every row
// to scan.
func (s *Store) eachRow(ctx context.Context, what, query string, args []any, scan func(*sql.Rows) error) error {
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		defer rows.Close()

		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// queryRow runs one single-row query in a tenant-scoped transaction; found is false on no match.
func (s *Store) queryRow(ctx context.Context, what, query string, args []any, dest ...any) (bool, error) {
	found := false
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		switch err := tx.QueryRowContext(ctx, query, args...).Scan(dest...); {
		case err == nil:
			found = true
			return nil
		case errors.Is(err, sql.ErrNoRows):
			return nil
		default:
			return fmt.Errorf("%s: %w", what, err)
		}
	})
	return found, err
}
