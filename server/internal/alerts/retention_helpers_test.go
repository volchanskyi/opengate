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

// How long the investigation tables actually keep what they are told to keep.
//
// Erasure already cascades: purging a machine or a customer takes its alerts,
// evidence and rooms with it. What these cases are about is the other axis —
// age — and the one rule that makes it safe to act on: a room is never removed
// while an alert still points at it, because the alert would survive with its
// room silently detached, reading as a finding nobody ever investigated.

// aged files one alert whose receipt is stamped at `received`, which is the only
// clock the sweep measures against. Event time is deliberately not used: a
// retroactive finding can arrive legitimately months after it happened, and
// ageing it out on the day it lands would delete evidence the moment it was
// handed over.
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

// countIn reports how many rows a table holds for the customer under test.
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

// roomHolding is the room the estate's alert folded into — the one a case
// closes so an age sweep has a candidate to consider.
func (e estate) roomHolding(t *testing.T) uuid.UUID {
	t.Helper()
	var room uuid.UUID
	e.readOne(t, `SELECT incident_id FROM alerts`, nil, &room)
	return room
}

// roomsIn counts how many times a room exists inside another tenant's scope, so
// a case can state that a sweep reached past the wall it was called through.
func (e estate) roomsIn(t *testing.T, in tenancy, id uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, dbtx.Scoped(in.ctx, e.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(in.ctx, `SELECT count(*) FROM incidents WHERE id = $1`, id).Scan(&n)
	}))
	return n
}

// cancelledContext is a context that is already done, which is what a process
// being asked to stop looks like to a sweep mid-drain.
func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// expireAt runs the sweep as of `at` and asserts how much it reclaimed.
func (e estate) expireAt(t *testing.T, at time.Time, horizon time.Duration, want int) {
	t.Helper()
	e.alerts.now = func() time.Time { return at }
	removed, err := e.alerts.SweepExpired(context.Background(), horizon)
	require.NoError(t, err)
	assert.Equal(t, want, removed, "rows reclaimed by the sweep at %s", at)
}

const year = 365 * 24 * time.Hour

// resolveRoomAt closes a room and stamps when, so an age-based sweep has
// something to measure. A room is only ever removed once it is closed.
//
// The history line goes in alongside it, because that is what closing a room
// actually writes — and a room removed without its history taking the same exit
// leaves rows pointing at an investigation that no longer exists.
func (e estate) resolveRoomAt(t *testing.T, id uuid.UUID, at time.Time) {
	t.Helper()
	e.exec(t, `UPDATE incidents SET status = 'resolved', resolved_at = $2::timestamptz WHERE id = $1`, id, at)
	e.exec(t, `INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, body)
	           SELECT $1::uuid, i.tenant_id, i.organization_id, i.id, $3::timestamptz, 'resolution', '{}'::jsonb
	             FROM incidents i WHERE i.id = $2`, uuid.New(), id, at)
}
