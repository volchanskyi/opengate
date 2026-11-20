package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// uniqueViolation is Postgres's SQLSTATE for a broken unique constraint: the per-tenant name, or
// the tenant's one default.
const uniqueViolation = "23505"

// defaultIndex is the unique index holding one default customer per tenant.
const defaultIndex = "idx_organizations_one_default"

// Each statement is one fixed literal that repeats the row-level-security tenant clause, so a
// mistake in either one alone still cannot reach another tenant's row.
const (
	getByIDQuery = `SELECT id, name, archived_at, created_at, updated_at FROM organizations
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

	listActiveQuery = `SELECT id, name, archived_at, created_at, updated_at FROM organizations
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND archived_at IS NULL ORDER BY name`
	listAllQuery = `SELECT id, name, archived_at, created_at, updated_at FROM organizations
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid ORDER BY name`

	renameQuery = `UPDATE organizations SET name = $2, updated_at = NOW()
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

	setArchivedQuery = `UPDATE organizations
			    SET archived_at = CASE WHEN $2 THEN COALESCE(archived_at, NOW()) ELSE NULL END,
			        updated_at = NOW()
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

	deleteQuery = `DELETE FROM organizations WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1
			 RETURNING is_default`

	defaultQuery = `SELECT id FROM organizations
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND is_default`
	handDefaultQuery = `UPDATE organizations SET is_default = TRUE
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid
			   AND id = (SELECT id FROM organizations
			              WHERE tenant_id = current_setting('app.current_tenant')::uuid
			              ORDER BY created_at, id LIMIT 1)`
)

// PostgresOrganizations implements [Repository] against PostgreSQL.
type PostgresOrganizations struct {
	db *sql.DB
}

// NewPostgresOrganizations returns a Postgres-backed Repository.
func NewPostgresOrganizations(db *sql.DB) *PostgresOrganizations {
	return &PostgresOrganizations{db: db}
}

// Create implements Repository. A tenant's first customer is marked its default.
func (p *PostgresOrganizations) Create(ctx context.Context, org *Organization) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	if err := ValidateName(org.Name); err != nil {
		return err
	}
	err := p.insert(ctx, tenant.TenantID, org)
	if violates(err, defaultIndex) {
		// Another create made the tenant's first customer at the same moment, so this one is
		// stored beside it unmarked.
		err = p.insert(ctx, tenant.TenantID, org)
	}
	return err
}

func (p *PostgresOrganizations) insert(ctx context.Context, tenantID uuid.UUID, org *Organization) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx,
			`INSERT INTO organizations (id, tenant_id, name, is_default, created_at, updated_at)
			 VALUES ($1, $2, $3,
			         NOT EXISTS (SELECT 1 FROM organizations WHERE tenant_id = $2 AND is_default),
			         NOW(), NOW())
			 RETURNING created_at, updated_at`,
			org.ID, tenantID, org.Name).Scan(&org.CreatedAt, &org.UpdatedAt)
		if isUniqueViolation(err) && !violates(err, defaultIndex) {
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
	// Two fixed statements, one per flag value, keep the SQL text free of runtime input.
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

// Delete implements Repository; devices and everything below them cascade. Deleting the default
// hands the mark to the oldest customer left, so a tenant with customers keeps one.
func (p *PostgresOrganizations) Delete(ctx context.Context, id ID) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var wasDefault bool
		err := tx.QueryRowContext(ctx, deleteQuery, id).Scan(&wasDefault)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || !wasDefault {
			return err
		}
		_, err = tx.ExecContext(ctx, handDefaultQuery)
		return err
	})
}

// EnsureDefault implements Repository. It returns the tenant's default customer and inserts one
// only when the tenant is empty.
func (p *PostgresOrganizations) EnsureDefault(ctx context.Context) (ID, error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return uuid.Nil, dbtx.ErrTenantRequired
	}
	var id uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, defaultQuery).Scan(&id)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// The insert races another connection doing the same thing; the one-default index
		// settles it and the loser reads the winner.
		_, err = tx.ExecContext(ctx,
			`INSERT INTO organizations (id, tenant_id, name, is_default, created_at, updated_at)
			 VALUES ($1, $2, $3, TRUE, NOW(), NOW())
			 ON CONFLICT DO NOTHING`,
			uuid.New(), tenant.TenantID, DefaultName)
		if err != nil {
			return fmt.Errorf("create default organization: %w", err)
		}
		return tx.QueryRowContext(ctx, defaultQuery).Scan(&id)
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

// violates reports whether err is a unique violation of the named index.
func violates(err error, index string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == index
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == uniqueViolation
}
