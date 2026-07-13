package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

const (
	// defaultInventoryLimit bounds a read when the caller passes a non-positive
	// limit. It sits above the sum of the agent's per-category caps so the full
	// footprint of a large host still returns in one page.
	defaultInventoryLimit = 10000
	// maxInventoryFieldLen caps every persisted text field. Legitimate inventory
	// labels are short; a longer value is truncated as defense-in-depth.
	maxInventoryFieldLen = 256
	// redactedField replaces a field carrying control characters (newlines/tabs),
	// which never appear in a legitimate inventory label and signal smuggled or
	// multi-line content.
	redactedField = "[redacted]"
)

// PostgresInventoryRepository stores Edge Sentinel discovery inventory in the
// tenant-scoped device_inventory RLS table.
type PostgresInventoryRepository struct {
	db *sql.DB
}

// NewPostgresInventoryRepository returns a Postgres-backed inventory repository.
func NewPostgresInventoryRepository(db *sql.DB) *PostgresInventoryRepository {
	return &PostgresInventoryRepository{db: db}
}

// Replace implements Repository.
func (r *PostgresInventoryRepository) Replace(ctx context.Context, deviceID uuid.UUID, ts time.Time, components []Component) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		return dbtx.ErrTenantRequired
	}
	if len(components) == 0 {
		return nil
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	ts = ts.UTC()
	tenant, _ := dbtx.TenantFromContext(ctx)
	return dbtx.Scoped(ctx, r.db, func(tx *sql.Tx) error {
		for _, c := range components {
			if !validKind(c.Kind) {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO device_inventory
				   (org_id, device_id, kind, name, version, port, proto, state, runtime, image, first_seen, last_seen)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
				 ON CONFLICT (org_id, device_id, kind, name, port, proto) DO UPDATE SET
				   version   = EXCLUDED.version,
				   state     = EXCLUDED.state,
				   runtime   = EXCLUDED.runtime,
				   image     = EXCLUDED.image,
				   last_seen = EXCLUDED.last_seen`,
				tenant.OrgID, deviceID, c.Kind,
				sanitizeInventoryText(c.Name),
				sanitizeInventoryText(c.Version),
				int(c.Port),
				sanitizeInventoryText(c.Proto),
				sanitizeInventoryText(c.State),
				sanitizeInventoryText(c.Runtime),
				sanitizeInventoryText(c.Image),
				ts); err != nil {
				return fmt.Errorf("upsert inventory component: %w", err)
			}
		}
		// Prune components absent from this scan so the stored rows always equal
		// the device's latest footprint (bounded by the agent's per-category caps).
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM device_inventory
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1 AND last_seen < $2`,
			deviceID, ts); err != nil {
			return fmt.Errorf("prune stale inventory: %w", err)
		}
		return nil
	})
}

// ListForDevice implements Repository.
func (r *PostgresInventoryRepository) ListForDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]Component, error) {
	if limit <= 0 {
		limit = defaultInventoryLimit
	}
	var out []Component
	err := dbtx.Scoped(ctx, r.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT kind, name, version, port, proto, state, runtime, image, first_seen, last_seen
			 FROM device_inventory
			 WHERE org_id = current_setting('app.current_org')::uuid AND device_id = $1
			 ORDER BY kind ASC, name ASC, port ASC
			 LIMIT $2`,
			deviceID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var c Component
			var port int
			if err := rows.Scan(&c.Kind, &c.Name, &c.Version, &port, &c.Proto, &c.State, &c.Runtime, &c.Image, &c.FirstSeen, &c.LastSeen); err != nil {
				return err
			}
			c.Port = clampPort(port)
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// validKind reports whether kind is one of the accepted component kinds. The DB
// CHECK constraint is the backstop; this skips a poison row before it fails the
// whole batch.
func validKind(kind string) bool {
	switch kind {
	case KindPort, KindService, KindDBEngine, KindContainer, KindPackage:
		return true
	default:
		return false
	}
}

// sanitizeInventoryText trims, redacts control-char-bearing values, and caps the
// length of a persisted inventory field. WS-16 already forbids secrets in the
// report; this is the persistence-boundary defense-in-depth.
func sanitizeInventoryText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return redactedField
	}
	if len(s) > maxInventoryFieldLen {
		s = s[:maxInventoryFieldLen]
	}
	return s
}

// clampPort narrows a DB integer into the uint16 port range, treating any value
// outside [0, 65535] as 0.
func clampPort(v int) uint16 {
	if v < 0 || v > 65535 {
		return 0
	}
	return uint16(v)
}
