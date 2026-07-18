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
	groupID := nullableUUID(d.GroupID)
	capsJSON, err := marshalCapabilities(d.Capabilities)
	if err != nil {
		return err
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO devices (id, org_id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), NOW())
			 ON CONFLICT (id) DO UPDATE SET
			   org_id = EXCLUDED.org_id,
			   group_id = COALESCE(EXCLUDED.group_id, devices.group_id),
			   hostname = EXCLUDED.hostname,
			   os = EXCLUDED.os,
			   os_display = EXCLUDED.os_display,
			   agent_version = EXCLUDED.agent_version,
			   capabilities = EXCLUDED.capabilities,
			   status = EXCLUDED.status,
			   last_seen = NOW(),
			   updated_at = NOW()`,
			d.ID, tenant.OrgID, groupID, d.Hostname, d.OS, d.OsDisplay, d.AgentVersion, capsJSON, string(d.Status))
		return err
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
			`DELETE FROM devices WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`, id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func (p *PostgresDevices) UpdateGroup(ctx context.Context, id DeviceID, groupID GroupID) error {
	gid := nullableUUID(groupID)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE devices SET group_id = $1, updated_at = NOW()
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $2`, gid, id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func (p *PostgresDevices) SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE devices SET status = $1, last_seen = NOW(), updated_at = NOW()
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $2`,
			string(status), id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func (p *PostgresDevices) SetMaintenance(ctx context.Context, id DeviceID, on bool, by uuid.UUID, reason string) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// maintenance_since is stamped only on the Active→Maintenance transition
		// (NOT maintenance_on AND on), so editing the reason in place while already
		// in maintenance never resets the entry clock. Exiting clears all three.
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
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $4`,
			on, by, reason, id)
		return checkAffected(res, err, ErrDeviceNotFound)
	})
}

func (p *PostgresDevices) CountInMaintenance(ctx context.Context) (int, error) {
	var count int
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM devices
			 WHERE (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
			   AND maintenance_on`).Scan(&count)
	})
	return count, err
}

func (p *PostgresDevices) ResetAllStatuses(ctx context.Context) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE devices SET status = $1, updated_at = NOW()
			 WHERE org_id = current_setting('app.current_org')::uuid AND status = $2`,
			string(StatusOffline), string(StatusOnline))
		return err
	})
}
