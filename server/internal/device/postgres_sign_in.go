package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The statements of one sign-in. The tenant read runs under the admin scope and leaves the
// transaction in the device's own tenant, so everything after it sees that tenant alone.
const (
	signInTenantSQL = `SELECT d.tenant_id,
			        set_config('app.current_tenant', d.tenant_id::text, true),
			        set_config('app.is_admin', 'false', true)
			   FROM devices d
			  WHERE d.id = $1`

	// A machine naming no customer takes the tenant's default; a reconnect keeps customer and site,
	// and a first site counts only inside that customer. The write leaves the tags it reads alone.
	signInWriteSQL = `WITH written AS (
			   INSERT INTO devices (id, tenant_id, organization_id, site_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
			   VALUES ($1, $2,
			           COALESCE($10::uuid, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 AND o.is_default)),
			           (SELECT s.id FROM sites s
			             WHERE s.id = $3::uuid
			               AND s.organization_id = COALESCE($10::uuid, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 AND o.is_default))),
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
			     updated_at = NOW()
			   RETURNING organization_id, site_id, maintenance_on)
			 SELECT w.organization_id, w.site_id, w.maintenance_on,
			        COALESCE((SELECT jsonb_object_agg(t.key, t.value) FROM device_tags t
			                   WHERE t.tenant_id = current_setting('app.current_tenant')::uuid AND t.device_id = $1),
			                 '{}'::jsonb)
			   FROM written w`

	signInFleetSQL = `SELECT COUNT(*) FROM devices
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND organization_id = $1`
)

// SignIn implements Repository. The transaction opens under the admin scope, so the tenant read
// finds the machine whichever tenant holds it.
func (p *PostgresDevices) SignIn(ctx context.Context, d *Device, needsFleetSize func(OrganizationID) bool) (SignIn, error) {
	caller, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return SignIn{}, dbtx.ErrTenantRequired
	}
	capsJSON, err := marshalCapabilities(d.Capabilities)
	if err != nil {
		return SignIn{}, err
	}
	var got SignIn
	err = dbtx.Scoped(dbtx.WithTenant(ctx, caller.TenantID, true), p.db, func(tx *sql.Tx) error {
		got, err = signInWithin(ctx, tx, d, capsJSON, caller.TenantID, needsFleetSize)
		return err
	})
	if err != nil {
		return SignIn{}, err
	}
	return got, nil
}

// signInWithin runs the sign-in's statements in tx, starting under the admin scope.
func signInWithin(ctx context.Context, tx *sql.Tx, d *Device, capsJSON []byte, fallback uuid.UUID, needsFleetSize func(OrganizationID) bool) (SignIn, error) {
	tenantID, err := signInTenant(ctx, tx, d.ID, fallback)
	if err != nil {
		return SignIn{}, err
	}
	got := SignIn{TenantID: tenantID}
	var (
		siteID uuid.NullUUID
		tags   []byte
	)
	err = tx.QueryRowContext(ctx, signInWriteSQL,
		d.ID, tenantID, nullableUUID(d.SiteID), d.Hostname, d.OS, d.OsDisplay, d.AgentVersion, capsJSON,
		string(d.Status), nullableUUID(d.OrganizationID)).
		Scan(&got.OrganizationID, &siteID, &got.MaintenanceOn, &tags)
	if err != nil {
		return SignIn{}, fmt.Errorf("write device: %w", err)
	}
	got.SiteID = siteID.UUID
	if err := json.Unmarshal(tags, &got.Tags); err != nil {
		return SignIn{}, fmt.Errorf("decode device tags: %w", err)
	}
	if needsFleetSize != nil && needsFleetSize(got.OrganizationID) {
		if err := tx.QueryRowContext(ctx, signInFleetSQL, got.OrganizationID).Scan(&got.FleetSize); err != nil {
			return SignIn{}, fmt.Errorf("count customer machines: %w", err)
		}
	}
	return got, nil
}

// signInTenant leaves tx in the device's own tenant, or in fallback for a device never written.
func signInTenant(ctx context.Context, tx *sql.Tx, id DeviceID, fallback uuid.UUID) (uuid.UUID, error) {
	var (
		tenantID         uuid.UUID
		scoped, notAdmin string
	)
	err := tx.QueryRowContext(ctx, signInTenantSQL, id).Scan(&tenantID, &scoped, &notAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, dbtx.SetLocal(ctx, tx, dbtx.Tenant{TenantID: fallback})
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read device tenant: %w", err)
	}
	return tenantID, nil
}
