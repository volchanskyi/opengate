package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// uniqueViolation is Postgres's SQLSTATE for a broken unique constraint, which
// here can only be the per-tenant name.
const uniqueViolation = "23505"

// tenantPredicate is the scope every statement carries. The RLS policy already
// filters, so this repeats the boundary in the statement itself: a mistake in
// either one alone still cannot reach another tenant's row.
const tenantPredicate = `tenant_id = current_setting('app.current_tenant')::uuid`

const organizationSelect = `SELECT id, name, archived_at, created_at, updated_at FROM organizations `

// Every statement is assembled here, at compile time, from constants only, and
// each call site passes one identifier. Nothing is ever built from a value that
// reached the process at runtime.
const (
	getByIDQuery = organizationSelect + `WHERE ` + tenantPredicate + ` AND id = $1`

	listActiveQuery = organizationSelect + `WHERE ` + tenantPredicate + ` AND archived_at IS NULL ORDER BY name`
	listAllQuery    = organizationSelect + `WHERE ` + tenantPredicate + ` ORDER BY name`

	renameQuery = `UPDATE organizations SET name = $2, updated_at = NOW()
			 WHERE ` + tenantPredicate + ` AND id = $1`

	setArchivedQuery = `UPDATE organizations
			    SET archived_at = CASE WHEN $2 THEN COALESCE(archived_at, NOW()) ELSE NULL END,
			        updated_at = NOW()
			  WHERE ` + tenantPredicate + ` AND id = $1`

	deleteQuery = `DELETE FROM organizations WHERE ` + tenantPredicate + ` AND id = $1`

	oldestQuery     = `SELECT id FROM organizations WHERE ` + tenantPredicate + ` ORDER BY created_at, id LIMIT 1`
	findByNameQuery = `SELECT id FROM organizations WHERE ` + tenantPredicate + ` AND name = $1`
)

// PostgresOrganizations implements [Repository] against PostgreSQL.
type PostgresOrganizations struct {
	db *sql.DB
}

// NewPostgresOrganizations returns a Postgres-backed Repository.
func NewPostgresOrganizations(db *sql.DB) *PostgresOrganizations {
	return &PostgresOrganizations{db: db}
}

// Create implements Repository.
func (p *PostgresOrganizations) Create(ctx context.Context, org *Organization) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	if err := ValidateName(org.Name); err != nil {
		return err
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx,
			`INSERT INTO organizations (id, tenant_id, name, created_at, updated_at)
			 VALUES ($1, $2, $3, NOW(), NOW())
			 RETURNING created_at, updated_at`,
			org.ID, tenant.TenantID, org.Name).Scan(&org.CreatedAt, &org.UpdatedAt)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		return err
	})
}

// Get implements Repository.
func (p *PostgresOrganizations) Get(ctx context.Context, id ID) (*Organization, error) {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return nil, dbtx.ErrTenantRequired
	}
	var org Organization
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return scanOrganization(tx.QueryRowContext(ctx, getByIDQuery, id), &org)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// List implements Repository.
func (p *PostgresOrganizations) List(ctx context.Context, includeArchived bool) ([]*Organization, error) {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return nil, dbtx.ErrTenantRequired
	}
	// Two fixed statements rather than one assembled from the flag, so nothing
	// here is built from runtime input.
	query := listActiveQuery
	if includeArchived {
		query = listAllQuery
	}

	var orgs []*Organization
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var org Organization
			if err := scanOrganization(rows, &org); err != nil {
				return err
			}
			orgs = append(orgs, &org)
		}
		return rows.Err()
	})
	return orgs, err
}

// Rename implements Repository.
func (p *PostgresOrganizations) Rename(ctx context.Context, id ID, name string) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return dbtx.ErrTenantRequired
	}
	if err := ValidateName(name); err != nil {
		return err
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, renameQuery, id, name)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		return checkAffected(res, err)
	})
}

// SetArchived implements Repository.
func (p *PostgresOrganizations) SetArchived(ctx context.Context, id ID, archived bool) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, setArchivedQuery, id, archived)
		return checkAffected(res, err)
	})
}

// Delete implements Repository. The devices foreign key cascades, which in turn
// cascades their telemetry, inventory, hardware and update rows.
func (p *PostgresOrganizations) Delete(ctx context.Context, id ID) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, deleteQuery, id)
		return checkAffected(res, err)
	})
}

// EnsureDefault implements Repository. It prefers any customer the tenant
// already has, so the default is a floor rather than a fixture, and inserts the
// default one only when the tenant is empty.
func (p *PostgresOrganizations) EnsureDefault(ctx context.Context) (ID, error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return uuid.Nil, dbtx.ErrTenantRequired
	}
	var id uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// The tenant's oldest customer, which is the same row a device row falls
		// back to when it is written without one named.
		err := tx.QueryRowContext(ctx, oldestQuery).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// The insert races another connection doing the same thing; the
		// per-tenant name constraint settles it and the loser reads the winner.
		id = uuid.New()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO organizations (id, tenant_id, name, created_at, updated_at)
			 VALUES ($1, $2, $3, NOW(), NOW())
			 ON CONFLICT (tenant_id, name) DO NOTHING`,
			id, tenant.TenantID, DefaultName)
		if err != nil {
			return fmt.Errorf("create default organization: %w", err)
		}
		return tx.QueryRowContext(ctx, findByNameQuery, DefaultName).Scan(&id)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func scanOrganization(sc interface{ Scan(...any) error }, org *Organization) error {
	var archivedAt sql.NullTime
	if err := sc.Scan(&org.ID, &org.Name, &archivedAt, &org.CreatedAt, &org.UpdatedAt); err != nil {
		return err
	}
	if archivedAt.Valid {
		org.ArchivedAt = &archivedAt.Time
	}
	return nil
}

// checkAffected turns a zero-row mutation into ErrNotFound, so "no such
// customer" and "not yours" answer the same way.
func checkAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == uniqueViolation
}
