package device

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The SQLSTATEs Postgres raises when a constraint rejects a write.
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
)

// PostgresSites implements [SiteRepository] against PostgreSQL.
type PostgresSites struct {
	db *sql.DB
}

// NewPostgresSites returns a Postgres-backed SiteRepository.
func NewPostgresSites(db *sql.DB) *PostgresSites {
	return &PostgresSites{db: db}
}

// Create stores a site under the named customer, checked against the caller's tenant first
// because foreign-key checks bypass row-level security. No customer means the tenant's own.
func (p *PostgresSites) Create(ctx context.Context, s *Site) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		if s.OrganizationID == uuid.Nil {
			fallback, err := tenantOwnOrganization(ctx, tx)
			if err != nil {
				return err
			}
			s.OrganizationID = fallback
		} else if err := organizationInTenant(ctx, tx, s.OrganizationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO sites (id, tenant_id, organization_id, name, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, NOW(), NOW())`,
			s.ID, tenant.TenantID, s.OrganizationID, s.Name)
		if isUniqueViolation(err) {
			return ErrSiteNameTaken
		}
		return err
	})
}

// tenantOwnOrganization returns the caller's tenant's oldest customer.
func tenantOwnOrganization(ctx context.Context, tx *sql.Tx) (OrganizationID, error) {
	var id OrganizationID
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM organizations
		  WHERE tenant_id = current_setting('app.current_tenant')::uuid
		  ORDER BY created_at, id
		  LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrOrganizationNotFound
	}
	return id, err
}

func (p *PostgresSites) Get(ctx context.Context, id SiteID) (*Site, error) {
	var s Site
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT id, organization_id, name, created_at, updated_at
			 FROM sites
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, id).
			Scan(&s.ID, &s.OrganizationID, &s.Name, &s.CreatedAt, &s.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// List returns the caller's sites by name, narrowed to one customer unless organizationID is zero.
func (p *PostgresSites) List(ctx context.Context, organizationID OrganizationID) ([]*Site, error) {
	var sites []*Site
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, organization_id, name, created_at, updated_at
			 FROM sites
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid
			   AND ($1::uuid IS NULL OR organization_id = $1)
			 ORDER BY name`, nullableUUID(organizationID))
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var s Site
			if err := rows.Scan(&s.ID, &s.OrganizationID, &s.Name, &s.CreatedAt, &s.UpdatedAt); err != nil {
				return err
			}
			sites = append(sites, &s)
		}
		return rows.Err()
	})
	return sites, err
}

// Delete removes a site; its devices stay with their customer, unfiled.
func (p *PostgresSites) Delete(ctx context.Context, id SiteID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM sites WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, id)
		return checkAffected(res, err, ErrSiteNotFound)
	})
}

// organizationInTenant returns ErrOrganizationNotFound for a customer outside the caller's tenant.
func organizationInTenant(ctx context.Context, tx *sql.Tx, organizationID OrganizationID) error {
	var exists bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM organizations
		    WHERE id = $1 AND tenant_id = current_setting('app.current_tenant')::uuid)`,
		organizationID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrOrganizationNotFound
	}
	return nil
}

// isUniqueViolation reports whether err is Postgres refusing a duplicate.
func isUniqueViolation(err error) bool {
	return hasSQLState(err, uniqueViolation)
}

// isForeignKeyViolation reports whether err is Postgres refusing a reference to a missing row.
func isForeignKeyViolation(err error) bool {
	return hasSQLState(err, foreignKeyViolation)
}

func hasSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
