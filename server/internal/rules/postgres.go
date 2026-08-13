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

// The mutable half of a rule: what a customer retuned, how far a rule has been
// rolled out, and which machines cannot evaluate it at all.
//
// Every read and write goes through a tenant-scoped transaction, so row-level
// security is what separates customers rather than a WHERE clause somebody has
// to remember. Each statement also names the tenant itself: the policy is the
// wall, and the predicate is a second lock on the same door.

// scopedToTenant is the predicate every statement here carries. It is a
// constant, so the queries built from it stay compile-time strings rather than
// anything assembled at run time.
const scopedToTenant = `tenant_id = current_setting('app.current_tenant')::uuid`

// levelNames maps a rung of the tenancy ladder to the value stored in the level
// column. The ladder itself lives in internal/settings; this is only its
// spelling in the database.
var levelNames = map[settings.Level]string{
	settings.LevelDevice:       "device",
	settings.LevelSite:         "site",
	settings.LevelOrganization: "organization",
	settings.LevelTenant:       "tenant",
}

// levelByName is levelNames read the other way, for rows coming back.
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

// eachRow runs one query inside a tenant-scoped transaction and hands every row
// to scan.
func (s *Store) eachRow(ctx context.Context, what, query string, args []any, scan func(*sql.Rows) error) error {
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		defer rows.Close() //nolint:errcheck // read-only; rows.Err below is the check

		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// queryRow runs one single-row query inside a tenant-scoped transaction. The
// second return is false when no row matched, which is an answer rather than a
// failure at every call site here.
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
