package device

import (
	"context"
	"database/sql"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresGroups implements [GroupRepository] against PostgreSQL.
type PostgresGroups struct {
	db *sql.DB
}

// NewPostgresGroups returns a Postgres-backed GroupRepository.
func NewPostgresGroups(db *sql.DB) *PostgresGroups {
	return &PostgresGroups{db: db}
}

func (p *PostgresGroups) Create(ctx context.Context, g *Group) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO groups_ (id, tenant_id, name, created_at, updated_at)
			 VALUES ($1, $2, $3, NOW(), NOW())`,
			g.ID, tenant.TenantID, g.Name)
		return err
	})
}

func (p *PostgresGroups) Get(ctx context.Context, id GroupID) (*Group, error) {
	var g Group
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT id, name, created_at, updated_at
			 FROM groups_
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, id).
			Scan(&g.ID, &g.Name, &g.CreatedAt, &g.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// List returns every group in the caller's tenant. Tenant is the
// visibility boundary, so there is nothing narrower to filter on.
func (p *PostgresGroups) List(ctx context.Context) ([]*Group, error) {
	var groups []*Group
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, name, created_at, updated_at
			 FROM groups_
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid
			 ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var g Group
			if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt, &g.UpdatedAt); err != nil {
				return err
			}
			groups = append(groups, &g)
		}
		return rows.Err()
	})
	return groups, err
}

func (p *PostgresGroups) Delete(ctx context.Context, id GroupID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM groups_ WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, id)
		return checkAffected(res, err, ErrGroupNotFound)
	})
}
