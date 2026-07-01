package device

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresLogs implements [LogsRepository] against PostgreSQL.
type PostgresLogs struct {
	db *sql.DB
}

// NewPostgresLogs returns a Postgres-backed LogsRepository.
func NewPostgresLogs(db *sql.DB) *PostgresLogs {
	return &PostgresLogs{db: db}
}

func (p *PostgresLogs) Upsert(ctx context.Context, deviceID DeviceID, entries []LogEntry) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM device_logs
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1`, deviceID); err != nil {
			return fmt.Errorf("delete old logs: %w", err)
		}

		for _, e := range entries {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO device_logs (org_id, device_id, timestamp, level, target, message, fetched_at)
				 VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
				tenant.OrgID, deviceID, e.Timestamp, e.Level, e.Target, e.Message); err != nil {
				return fmt.Errorf("insert log entry: %w", err)
			}
		}
		return nil
	})
}

// Query is sentinel-parameterized to keep the SQL static (Sonar go:S2077).
// Level is compared via a severity-ordered CASE so "WARN" matches WARN+ERROR,
// mirroring the agent-side logger semantics.
func (p *PostgresLogs) Query(ctx context.Context, deviceID DeviceID, filter LogFilter) ([]LogEntry, int, error) {
	searchPattern := ""
	if filter.Search != "" {
		searchPattern = "%" + filter.Search + "%"
	}

	filterArgs := []any{
		deviceID,
		filter.Level,
		filter.From,
		filter.To,
		filter.Search, searchPattern,
	}

	var total int
	var entries []LogEntry
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, logCountSQL, filterArgs...).Scan(&total); err != nil {
			return fmt.Errorf("count logs: %w", err)
		}

		dataArgs := append(append([]any{}, filterArgs...), logLimit(filter.Limit), filter.Offset)
		rows, err := tx.QueryContext(ctx, logRowsSQL, dataArgs...)
		if err != nil {
			return fmt.Errorf("query logs: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var e LogEntry
			if err := rows.Scan(&e.ID, &e.DeviceID, &e.Timestamp, &e.Level, &e.Target, &e.Message, &e.FetchedAt); err != nil {
				return fmt.Errorf("scan log entry: %w", err)
			}
			entries = append(entries, e)
		}
		return rows.Err()
	})
	return entries, total, err
}

func logLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func (p *PostgresLogs) HasRecent(ctx context.Context, deviceID DeviceID, maxAge time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	var exists bool
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT EXISTS(
				SELECT 1 FROM device_logs
				WHERE org_id = current_setting('app.current_org')::uuid
				  AND device_id = $1 AND fetched_at > $2)`,
			deviceID, cutoff).Scan(&exists)
	})
	if err != nil {
		return false, fmt.Errorf("check recent logs: %w", err)
	}
	return exists, nil
}
