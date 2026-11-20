package organization

import (
	"context"
	"database/sql"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// allCustomersQuery reads the caller's tenant's customers, or every tenant's under the admin scope.
const allCustomersQuery = `SELECT tenant_id, id FROM organizations
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	    OR current_setting('app.is_admin', true)::boolean
	 ORDER BY tenant_id, id`

// AllCustomers implements Repository.
func (p *PostgresOrganizations) AllCustomers(ctx context.Context) ([]Ref, error) {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return nil, dbtx.ErrTenantRequired
	}
	var refs []Ref
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, allCustomersQuery)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref Ref
			if err := rows.Scan(&ref.TenantID, &ref.ID); err != nil {
				return err
			}
			refs = append(refs, ref)
		}
		return rows.Err()
	})
	return refs, err
}
