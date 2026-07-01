package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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
			`INSERT INTO device_hardware (device_id, org_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
			 ON CONFLICT (device_id) DO UPDATE SET
			   org_id = EXCLUDED.org_id,
			   cpu_model = EXCLUDED.cpu_model,
			   cpu_cores = EXCLUDED.cpu_cores,
			   ram_total_mb = EXCLUDED.ram_total_mb,
			   disk_total_mb = EXCLUDED.disk_total_mb,
			   disk_free_mb = EXCLUDED.disk_free_mb,
			   network_interfaces = EXCLUDED.network_interfaces,
			   updated_at = NOW()`,
			hw.DeviceID, tenant.OrgID, hw.CPUModel, hw.CPUCores,
			hw.RAMTotalMB, hw.DiskTotalMB, hw.DiskFreeMB, niJSON)
		return err
	})
}

func (p *PostgresHardware) Get(ctx context.Context, deviceID DeviceID) (*Hardware, error) {
	var hw Hardware
	var niJSON []byte
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT device_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces, updated_at
			 FROM device_hardware
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1`, deviceID).
			Scan(&hw.DeviceID, &hw.CPUModel, &hw.CPUCores,
				&hw.RAMTotalMB, &hw.DiskTotalMB, &hw.DiskFreeMB,
				&niJSON, &hw.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHardwareNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(niJSON, &hw.NetworkInterfaces); err != nil {
		return nil, fmt.Errorf("unmarshal network interfaces: %w", err)
	}
	if hw.NetworkInterfaces == nil {
		hw.NetworkInterfaces = []NetworkInterfaceInfo{}
	}
	return &hw, nil
}
