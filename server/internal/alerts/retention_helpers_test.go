package alerts

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// aged stamps receipt time, the only clock the sweep measures; a retroactive finding arrives late.
func (e estate) aged(t *testing.T, received time.Time, change func(*Alert)) uuid.UUID {
	t.Helper()
	e.alerts.now = func() time.Time { return received }
	defer func() { e.alerts.now = func() time.Time { return e.now } }()

	a := e.variant(change)
	_, err := e.alerts.Record(e.ctx, a, perMachine)
	require.NoError(t, err)

	var id uuid.UUID
	e.readOne(t, `SELECT id FROM alerts WHERE device_id = $1 AND window_start = $2`,
		[]any{a.DeviceID, a.WindowStart}, &id)
	return id
}

func (e estate) countIn(t *testing.T, table string) int {
	t.Helper()
	var n int
	switch table {
	case "alerts":
		e.readOne(t, `SELECT count(*) FROM alerts`, nil, &n)
	case "incidents":
		e.readOne(t, `SELECT count(*) FROM incidents`, nil, &n)
	case "incident_events":
		e.readOne(t, `SELECT count(*) FROM incident_events`, nil, &n)
	default:
		t.Fatalf("unknown table %q", table)
	}
	return n
}

func (e estate) roomHolding(t *testing.T) uuid.UUID {
	t.Helper()
	var room uuid.UUID
	e.readOne(t, `SELECT incident_id FROM alerts`, nil, &room)
	return room
}

func (e estate) roomsIn(t *testing.T, in tenancy, id uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, dbtx.Scoped(in.ctx, e.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(in.ctx, `SELECT count(*) FROM incidents WHERE id = $1`, id).Scan(&n)
	}))
	return n
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func (e estate) expireAt(t *testing.T, at time.Time, horizon time.Duration, want int) {
	t.Helper()
	e.alerts.now = func() time.Time { return at }
	removed, err := e.alerts.SweepExpired(context.Background(), horizon)
	require.NoError(t, err)
	assert.Equal(t, want, removed, "rows reclaimed by the sweep at %s", at)
}

const year = 365 * 24 * time.Hour

func (e estate) resolveRoomAt(t *testing.T, id uuid.UUID, at time.Time) {
	t.Helper()
	e.exec(t, `UPDATE incidents SET status = 'resolved', resolved_at = $2::timestamptz WHERE id = $1`, id, at)
	e.exec(t, `INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, body)
	           SELECT $1::uuid, i.tenant_id, i.organization_id, i.id, $3::timestamptz, 'resolution', '{}'::jsonb
	             FROM incidents i WHERE i.id = $2`, uuid.New(), id, at)
}
