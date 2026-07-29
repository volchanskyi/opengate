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

// PostgresHardware implements [HardwareRepository] against PostgreSQL.
type PostgresHardware struct {
	db *sql.DB
}

// NewPostgresHardware returns a Postgres-backed HardwareRepository.
func NewPostgresHardware(db *sql.DB) *PostgresHardware {
	return &PostgresHardware{db: db}
}

// Upsert stores an agent hardware report. It targets only the columns the agent
// owns, so the AMT model and firmware the server reads over WSMAN survive.
// A nil SystemUUID or AMTAvailable means the agent said nothing — the stored
// value stands, which keeps an agent too old to report AMT from orphaning a
// device's AMT link.
func (p *PostgresHardware) Upsert(ctx context.Context, hw *Hardware) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	niJSON, err := json.Marshal(hw.NetworkInterfaces)
	if err != nil {
		return fmt.Errorf("marshal network interfaces: %w", err)
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO device_hardware (device_id, org_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces,
			                              system_uuid, amt_available, amt_version, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10, FALSE), $11, NOW())
			 ON CONFLICT (device_id) DO UPDATE SET
			   org_id = EXCLUDED.org_id,
			   cpu_model = EXCLUDED.cpu_model,
			   cpu_cores = EXCLUDED.cpu_cores,
			   ram_total_mb = EXCLUDED.ram_total_mb,
			   disk_total_mb = EXCLUDED.disk_total_mb,
			   disk_free_mb = EXCLUDED.disk_free_mb,
			   network_interfaces = EXCLUDED.network_interfaces,
			   system_uuid = CASE WHEN $9::uuid IS NULL THEN device_hardware.system_uuid ELSE $9::uuid END,
			   amt_available = CASE WHEN $10::boolean IS NULL THEN device_hardware.amt_available ELSE $10::boolean END,
			   amt_version = CASE WHEN EXCLUDED.amt_version = '' THEN device_hardware.amt_version ELSE EXCLUDED.amt_version END,
			   updated_at = NOW()`,
			hw.DeviceID, tenant.OrgID, hw.CPUModel, hw.CPUCores,
			hw.RAMTotalMB, hw.DiskTotalMB, hw.DiskFreeMB, niJSON,
			hw.SystemUUID, hw.AMTAvailable, hw.AMTVersion)
		return err
	})
}

func (p *PostgresHardware) Get(ctx context.Context, deviceID DeviceID) (*Hardware, error) {
	var hw Hardware
	var niJSON []byte
	var amtAvailable bool
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// system_uuid is deliberately absent: it is a join key the server resolves
		// internally and never hands back to a caller.
		return tx.QueryRowContext(ctx,
			`SELECT device_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces, updated_at,
			        amt_available, amt_version, amt_model, amt_firmware
			 FROM device_hardware
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1`, deviceID).
			Scan(&hw.DeviceID, &hw.CPUModel, &hw.CPUCores,
				&hw.RAMTotalMB, &hw.DiskTotalMB, &hw.DiskFreeMB,
				&niJSON, &hw.UpdatedAt,
				&amtAvailable, &hw.AMTVersion, &hw.AMTModel, &hw.AMTFirmware)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHardwareNotFound
	}
	if err != nil {
		return nil, err
	}
	hw.AMTAvailable = &amtAvailable
	if err := json.Unmarshal(niJSON, &hw.NetworkInterfaces); err != nil {
		return nil, fmt.Errorf("unmarshal network interfaces: %w", err)
	}
	if hw.NetworkInterfaces == nil {
		hw.NetworkInterfaces = []NetworkInterfaceInfo{}
	}
	return &hw, nil
}

// ResolveBySystemUUID implements [HardwareRepository]. A CIRA connection has no
// request tenant to inherit, so this supplies an admin scope and identifies the
// device by the globally unique SMBIOS UUID across organizations.
func (p *PostgresHardware) ResolveBySystemUUID(ctx context.Context, systemUUID uuid.UUID) (DeviceID, uuid.UUID, error) {
	ctx = dbtx.WithDefaultTenant(ctx, true)
	var deviceID, orgID uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// LIMIT 2 turns a duplicate — cloned disk images share a system UUID —
		// into "more than one row", which the caller treats as no match.
		rows, err := tx.QueryContext(ctx,
			`SELECT device_id, org_id FROM device_hardware WHERE system_uuid = $1 LIMIT 2`, systemUUID)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // read-only; the row error is checked below

		matches := 0
		for rows.Next() {
			matches++
			if matches > 1 {
				return ErrHardwareNotFound
			}
			if err := rows.Scan(&deviceID, &orgID); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if matches == 0 {
			return ErrHardwareNotFound
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return deviceID, orgID, nil
}

// SetAMTDetail implements [HardwareRepository]. It writes only the two columns
// the WSMAN query owns, so a concurrent agent report is never clobbered.
func (p *PostgresHardware) SetAMTDetail(ctx context.Context, deviceID DeviceID, model, firmware string) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE device_hardware
			    SET amt_model = CASE WHEN $1 = '' THEN amt_model ELSE $1 END,
			        amt_firmware = CASE WHEN $2 = '' THEN amt_firmware ELSE $2 END,
			        updated_at = NOW()
			  WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $3`,
			model, firmware, deviceID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrHardwareNotFound
		}
		return nil
	})
}
