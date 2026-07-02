package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

const defaultProcessLimit = 100

// PostgresProcessRepository stores Edge Sentinel process reports in Postgres.
type PostgresProcessRepository struct {
	db *sql.DB
}

// NewPostgresProcessRepository returns a Postgres-backed process repository.
func NewPostgresProcessRepository(db *sql.DB) *PostgresProcessRepository {
	return &PostgresProcessRepository{db: db}
}

// UpsertReport stores one timestamped top-N process report for a device.
func (p *PostgresProcessRepository) UpsertReport(ctx context.Context, deviceID uuid.UUID, ts time.Time, samples []ProcessSample) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	if len(samples) == 0 {
		return nil
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		for _, sample := range samples {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO device_processes (org_id, device_id, ts, rank, basename, cmdline_hash, pid, cpu, mem)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				 ON CONFLICT (org_id, device_id, ts, rank) DO UPDATE SET
				   basename = EXCLUDED.basename,
				   cmdline_hash = EXCLUDED.cmdline_hash,
				   pid = EXCLUDED.pid,
				   cpu = EXCLUDED.cpu,
				   mem = EXCLUDED.mem`,
				tenant.OrgID, deviceID, ts.UTC(), int64(sample.Rank), sample.Basename,
				sample.CmdlineHash, int64(sample.PID), sample.CPU, sample.Mem)
			if err != nil {
				return fmt.Errorf("upsert process sample: %w", err)
			}
		}
		return nil
	})
}

// ListLatest returns the newest process rows for a device in the current tenant.
func (p *PostgresProcessRepository) ListLatest(ctx context.Context, deviceID uuid.UUID, limit int) ([]ProcessSample, error) {
	if limit <= 0 {
		limit = defaultProcessLimit
	}
	var out []ProcessSample
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT ts, rank, basename, cmdline_hash, pid, cpu, mem
			 FROM device_processes
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1
			 ORDER BY ts DESC, rank ASC
			 LIMIT $2`,
			deviceID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var sample ProcessSample
			var rank, pid int64
			if err := rows.Scan(&sample.TS, &rank, &sample.Basename, &sample.CmdlineHash, &pid, &sample.CPU, &sample.Mem); err != nil {
				return err
			}
			sample.Rank, err = uint32FromDB("rank", rank)
			if err != nil {
				return err
			}
			sample.PID, err = uint32FromDB("pid", pid)
			if err != nil {
				return err
			}
			out = append(out, sample)
		}
		return rows.Err()
	})
	return out, err
}

func uint32FromDB(field string, value int64) (uint32, error) {
	if value < 0 || value > math.MaxUint32 {
		return 0, fmt.Errorf("%s out of uint32 range: %d", field, value)
	}
	return uint32(value), nil
}
