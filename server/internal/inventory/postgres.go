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
	// defaultInventoryLimit bounds a read with a non-positive limit, above the agent's caps.
	defaultInventoryLimit = 10000
	// maxInventoryFieldLen caps every persisted text field. Legitimate inventory
	// labels are short; a longer value is truncated as defense-in-depth.
	maxInventoryFieldLen = 256
	// redactedField replaces a field carrying control characters, which signal smuggled content.
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
	rows := inventoryRowsOf(components)
	return dbtx.Scoped(ctx, r.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, upsertInventoryStatement,
			tenant.TenantID, deviceID, ts, rows.kinds, rows.names, rows.versions, rows.ports,
			rows.protos, rows.states, rows.runtimes, rows.images); err != nil {
			return fmt.Errorf("upsert inventory components: %w", err)
		}
		// Prune components absent from this scan so the stored rows always equal
		// the device's latest footprint (bounded by the agent's per-category caps).
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM device_inventory
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND device_id = $1 AND last_seen < $2`,
			deviceID, ts); err != nil {
			return fmt.Errorf("prune stale inventory: %w", err)
		}
		return nil
	})
}

// upsertInventoryStatement writes a whole report in one statement, one array per column.
const upsertInventoryStatement = `INSERT INTO device_inventory
	   (tenant_id, device_id, kind, name, version, port, proto, state, runtime, image, first_seen, last_seen)
	 SELECT $1, $2, kind, name, version, port, proto, state, runtime, image, $3, $3
	   FROM unnest($4::text[], $5::text[], $6::text[], $7::int[], $8::text[], $9::text[], $10::text[], $11::text[])
	     AS c(kind, name, version, port, proto, state, runtime, image)
	 ON CONFLICT (tenant_id, device_id, kind, name, port, proto) DO UPDATE SET
	   version   = EXCLUDED.version,
	   state     = EXCLUDED.state,
	   runtime   = EXCLUDED.runtime,
	   image     = EXCLUDED.image,
	   last_seen = EXCLUDED.last_seen`

// inventoryRows is a report's valid components column by column, sanitized, each key once.
type inventoryRows struct {
	kinds, names, versions, protos, states, runtimes, images []string
	ports                                                    []int32
}

// inventoryRowsOf keeps the last component a report names under each key, since one statement
// updates a row once, and skips a component of no accepted kind.
func inventoryRowsOf(components []Component) inventoryRows {
	type key struct {
		kind, name, proto string
		port              int32
	}
	at := make(map[key]int, len(components))
	var rows inventoryRows
	for _, c := range components {
		if !validKind(c.Kind) {
			continue
		}
		name, proto, port := sanitizeInventoryText(c.Name), sanitizeInventoryText(c.Proto), int32(c.Port)
		k := key{kind: c.Kind, name: name, proto: proto, port: port}
		i, seen := at[k]
		if !seen {
			i = len(rows.kinds)
			at[k] = i
			rows.kinds = append(rows.kinds, c.Kind)
			rows.names = append(rows.names, name)
			rows.ports = append(rows.ports, port)
			rows.protos = append(rows.protos, proto)
			rows.versions = append(rows.versions, "")
			rows.states = append(rows.states, "")
			rows.runtimes = append(rows.runtimes, "")
			rows.images = append(rows.images, "")
		}
		rows.versions[i] = sanitizeInventoryText(c.Version)
		rows.states[i] = sanitizeInventoryText(c.State)
		rows.runtimes[i] = sanitizeInventoryText(c.Runtime)
		rows.images[i] = sanitizeInventoryText(c.Image)
	}
	return rows
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
			 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND device_id = $1
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

// validKind reports whether kind is an accepted component kind, so a bad row is skipped before
// the CHECK constraint fails the batch.
func validKind(kind string) bool {
	switch kind {
	case KindPort, KindService, KindDBEngine, KindContainer, KindPackage:
		return true
	default:
		return false
	}
}

// sanitizeInventoryText trims, redacts control-character values, and caps the length of a
// persisted inventory field.
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
