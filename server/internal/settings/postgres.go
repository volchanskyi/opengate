package settings

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresReader implements [Reader] against PostgreSQL.
type PostgresReader struct {
	db *sql.DB
}

// NewPostgresReader returns a Postgres-backed Reader.
func NewPostgresReader(db *sql.DB) *PostgresReader {
	return &PostgresReader{db: db}
}

// ScopeFor reads one machine's whole ladder in a single row. A device outside the
// caller's tenant reads as not found.
func (p *PostgresReader) ScopeFor(ctx context.Context, deviceID uuid.UUID) (Scope, error) {
	var (
		scope  Scope
		siteID uuid.NullUUID
	)
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT d.id, d.site_id, d.organization_id, d.tenant_id
			   FROM devices d
			  WHERE d.tenant_id = current_setting('app.current_tenant')::uuid AND d.id = $1`, deviceID).
			Scan(&scope.DeviceID, &siteID, &scope.OrganizationID, &scope.TenantID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrDeviceNotFound
	}
	if err != nil {
		return Scope{}, err
	}
	// An unfiled machine has no site rung; the zero value marks it.
	if siteID.Valid {
		scope.SiteID = siteID.UUID
	}
	return scope, nil
}
