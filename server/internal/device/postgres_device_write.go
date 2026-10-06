package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

func (p *PostgresDevices) Upsert(ctx context.Context, d *Device) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	siteID := nullableUUID(d.SiteID)
	capsJSON, err := marshalCapabilities(d.Capabilities)
	if err != nil {
		return err
	}
	// A device naming no customer takes the tenant's oldest; a reconnect keeps its customer and site.
	// A site on first registration counts only when it belongs to the customer the device lands in.
	organizationID := nullableUUID(d.OrganizationID)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO devices (id, tenant_id, organization_id, site_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
			 VALUES ($1, $2,
			         COALESCE($10::uuid, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 ORDER BY o.created_at, o.id LIMIT 1)),
			         (SELECT s.id FROM sites s
			           WHERE s.id = $3::uuid
			             AND s.organization_id = COALESCE($10::uuid, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 ORDER BY o.created_at, o.id LIMIT 1))),
			         $4, $5, $6, $7, $8, $9, NOW(), NOW(), NOW())
			 ON CONFLICT (id) DO UPDATE SET
			   tenant_id = EXCLUDED.tenant_id,
			   organization_id = COALESCE($10::uuid, devices.organization_id),
			   site_id = devices.site_id,
			   hostname = EXCLUDED.hostname,
			   os = EXCLUDED.os,
			   os_display = EXCLUDED.os_display,
			   agent_version = EXCLUDED.agent_version,
			   capabilities = EXCLUDED.capabilities,
			   status = EXCLUDED.status,
			   last_seen = NOW(),
			   updated_at = NOW()`,
			d.ID, tenant.TenantID, siteID, d.Hostname, d.OS, d.OsDisplay, d.AgentVersion, capsJSON, string(d.Status), organizationID)
		return err
	})
}

// UpdateOrganization implements Repository. It checks the customer against the caller's tenant
// first because foreign-key checks bypass row-level security.
func (p *PostgresDevices) UpdateOrganization(ctx context.Context, id DeviceID, organizationID OrganizationID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
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
		// The site clears in the same statement because a site belongs to one customer.
		res, err := tx.ExecContext(ctx,
			`UPDATE devices SET organization_id = $2, site_id = NULL, updated_at = NOW()
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`,
			id, organizationID)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func marshalCapabilities(caps []string) ([]byte, error) {
	if caps == nil {
		caps = []string{}
	}
	capsJSON, err := json.Marshal(caps)
	if err != nil {
		return nil, fmt.Errorf("marshal capabilities: %w", err)
	}
	return capsJSON, nil
}

func (p *PostgresDevices) Delete(ctx context.Context, id DeviceID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM devices WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

// UpdateSite implements Repository. A composite-key violation maps to ErrSiteNotInOrganization.
func (p *PostgresDevices) UpdateSite(ctx context.Context, id DeviceID, siteID SiteID) error {
	sid := nullableUUID(siteID)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE devices SET site_id = $1, updated_at = NOW()
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $2`, sid, id)
		if isForeignKeyViolation(err) {
			return ErrSiteNotInOrganization
		}
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func (p *PostgresDevices) SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE devices SET status = $1, last_seen = NOW(), updated_at = NOW()
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $2`,
			string(status), id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func (p *PostgresDevices) SetMaintenance(ctx context.Context, id DeviceID, on bool, by uuid.UUID, reason string) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// maintenance_since is stamped only on entering maintenance, so a reason edit keeps the clock.
		res, err := tx.ExecContext(ctx,
			`UPDATE devices SET
			   maintenance_on = $1,
			   maintenance_since = CASE
			     WHEN $1 AND NOT maintenance_on THEN NOW()
			     WHEN $1 THEN maintenance_since
			     ELSE NULL END,
			   maintenance_by = CASE WHEN $1 THEN $2::uuid ELSE NULL END,
			   maintenance_reason = CASE WHEN $1 THEN $3 ELSE '' END,
			   updated_at = NOW()
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $4`,
			on, by, reason, id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

// Counts reads the fleet rollup in one aggregate row, tenant-scoped even for administrators.
func (p *PostgresDevices) Counts(ctx context.Context, organizationID OrganizationID) (Counts, error) {
	var c Counts
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// A NULL customer counts the whole tenant.
		return tx.QueryRowContext(ctx,
			`SELECT COUNT(*),
			        COUNT(*) FILTER (WHERE status = 'online'),
			        COUNT(*) FILTER (WHERE maintenance_on)
			   FROM devices
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid
			    AND ($1::uuid IS NULL OR organization_id = $1::uuid)`,
			nullableUUID(organizationID)).
			Scan(&c.Total, &c.Online, &c.Maintenance)
	})
	return c, err
}

func (p *PostgresDevices) ResetAllStatuses(ctx context.Context) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE devices SET status = $1, updated_at = NOW()
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND status = $2`,
			string(StatusOffline), string(StatusOnline))
		return err
	})
}
