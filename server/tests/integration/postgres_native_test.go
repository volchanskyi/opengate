package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// Phase B / B4: Postgres-native integration tests.
//
// The migration from SQLite → PostgreSQL 17 (ADR-014, Phase 13a) introduced
// driver-specific semantics for TIMESTAMPTZ, JSONB and UUID columns that no
// existing test pinned. A pgx/v5 upgrade or pool-config change could silently
// regress any of these. These tests exercise the production tables directly
// through the *sql.DB exposed by testutil.NewTestStore (with schema
// isolation), guarding against:
//
//   - TIMESTAMPTZ: a value inserted with a non-UTC offset must read back as
//     the equivalent UTC instant. Postgres stores TIMESTAMPTZ as UTC and
//     pgx returns time.Time in UTC.
//   - JSONB: bit-level round-trip on device_hardware.network_interfaces with
//     unicode, empty strings, null-equivalent (empty slices), and multi-
//     element values.
//   - UUID: malformed strings rejected at the database boundary; valid
//     UUIDs accepted regardless of letter case and normalised on read.
//   - Concurrent writes: 32 parallel UpsertDevice goroutines complete
//     without pool, deadlock, or constraint errors.
//   - Prepared-statement cache: 200 sequential executes of the same SQL
//     statement complete without "prepared statement already exists"
//     pgx errors.

// pgStore returns the test store and its underlying *sql.DB for postgres-native tests.
func pgStore(t *testing.T) (*db.PostgresStore, *sql.DB) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return store, store.DB()
}

// seedDeviceRow inserts a minimal row directly so we can pin exact
// timestamps. UpsertDevice() sets last_seen = NOW(), which would mask
// the round-trip we're trying to verify.
func seedDeviceRow(t *testing.T, ctx context.Context, sqlDB *sql.DB, id uuid.UUID, lastSeen time.Time) {
	t.Helper()
	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO devices (id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
		VALUES ($1, NULL, 'native-test', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', $2, NOW(), NOW())
	`, id, lastSeen)
	require.NoError(t, err)
}

func TestPostgresTIMESTAMPTZNormalizesNonUTCOffsetToUTC(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := t.Context()

	// IST is UTC+05:30 — chosen for a non-whole-hour offset so a stripped
	// offset would be detectable both in the hour and minute fields.
	// Microsecond is the smallest unit Postgres stores; sub-microsecond
	// digits would be silently truncated and break a strict comparison.
	ist := time.FixedZone("IST", 5*3600+30*60)
	original := time.Date(2026, 3, 14, 9, 15, 30, 123_456_000, ist)
	wantUTC := original.UTC()

	id := uuid.New()
	seedDeviceRow(t, ctx, sqlDB, id, original)

	var got time.Time
	err := sqlDB.QueryRowContext(ctx,
		`SELECT last_seen FROM devices WHERE id = $1`, id,
	).Scan(&got)
	require.NoError(t, err)

	// The returned time.Time may be in time.Local (pgx's default behaviour
	// for TIMESTAMPTZ); what must hold is the *instant* equality at
	// Postgres's microsecond precision. A regression that drops the offset
	// or stores naive timestamps would shift the instant.
	assert.True(t, got.Equal(wantUTC),
		"want UTC instant %v; got %v", wantUTC, got)
	assert.Equal(t, wantUTC.UnixMicro(), got.UnixMicro(),
		"microsecond precision must survive the round-trip")
}

func TestPostgresJSONBNetworkInterfacesRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := pgStore(t)
	ctx := t.Context()

	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)
	dev := testutil.SeedDevice(t, ctx, store, group.ID)

	originals := []device.NetworkInterfaceInfo{
		{Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff",
			IPv4: []string{"10.0.0.1"},
			IPv6: []string{"fe80::1", "2001:db8::1"}},
		// Unicode in name + MAC empty.
		{Name: "测试-🌐", MAC: "",
			IPv4: []string{},
			IPv6: []string{}},
		// Empty name, single IPv4, no IPv6 at all.
		{Name: "", MAC: "11:22:33:44:55:66",
			IPv4: []string{"192.168.1.10"},
			IPv6: []string{}},
	}
	hw := &device.Hardware{
		DeviceID:          dev.ID,
		CPUModel:          "Intel(R) Core(TM) i9-12900K",
		CPUCores:          16,
		RAMTotalMB:        32_768,
		DiskTotalMB:       1_024_000,
		DiskFreeMB:        512_000,
		NetworkInterfaces: originals,
	}
	require.NoError(t, testutil.NewTestHardware(t, store).Upsert(ctx, hw))

	got, err := testutil.NewTestHardware(t, store).Get(ctx, dev.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Compare by re-encoding to JSON to assert bit-level fidelity — slice
	// equality alone would not catch a corrupted IPv6 entry hidden behind
	// reflect.DeepEqual's permissive nil/empty matching.
	wantJSON, err := json.Marshal(originals)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got.NetworkInterfaces)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), string(gotJSON))

	// Unicode preservation: must round-trip the actual rune sequence.
	require.Len(t, got.NetworkInterfaces, 3)
	assert.Equal(t, "测试-🌐", got.NetworkInterfaces[1].Name)
}

func TestPostgresUUIDRejectsMalformedAtBoundary(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := t.Context()

	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"too short", "11111111-1111-1111-1111-11111111111"},  // 35 hex chars
		{"too long", "11111111-1111-1111-1111-1111111111111"}, // 37 hex chars
		{"non-hex char", "gggggggg-gggg-gggg-gggg-gggggggggggg"},
		{"missing dashes", "1111111111111111111111111111111111111"},
		{"garbage", "definitely-not-a-uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO devices (id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
				VALUES ($1, 'x', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', NOW(), NOW(), NOW())
			`, tt.raw)
			require.Error(t, err, "postgres must reject malformed UUID %q", tt.raw)
			assert.Contains(t, strings.ToLower(err.Error()), "invalid input syntax")
		})
	}
}

func TestPostgresUUIDAcceptsAllCases(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := t.Context()

	canonical := "550e8400-e29b-41d4-a716-446655440000"
	tests := []struct {
		name string
		raw  string
	}{
		{"all lowercase", canonical},
		{"all uppercase", strings.ToUpper(canonical)},
		{"mixed case", "550E8400-e29B-41D4-a716-446655440000"},
		// 32 hex chars without dashes is also accepted by Postgres.
		{"no dashes", strings.ReplaceAll(canonical, "-", "")},
	}
	want := uuid.MustParse(canonical)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO devices (id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
				VALUES ($1, 'case-test', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', NOW(), NOW(), NOW())
				ON CONFLICT (id) DO UPDATE SET hostname = EXCLUDED.hostname
			`, tt.raw)
			require.NoError(t, err)

			var got uuid.UUID
			err = sqlDB.QueryRowContext(ctx,
				`SELECT id FROM devices WHERE hostname = 'case-test'`).Scan(&got)
			require.NoError(t, err)
			assert.Equal(t, want, got, "postgres must normalise UUID to canonical bytes regardless of input case")
		})
	}
}

func TestPostgresConcurrentUpsertDevices(t *testing.T) {
	t.Parallel()
	store, _ := pgStore(t)
	ctx := t.Context()

	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)

	const N = 32
	ids := make([]uuid.UUID, N)
	for i := range ids {
		ids[i] = uuid.New()
	}

	var wg sync.WaitGroup
	errCh := make(chan error, N)
	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := testutil.NewTestDevices(t, store).Upsert(ctx, &device.Device{
				ID:           ids[i],
				GroupID:      group.ID,
				Hostname:     fmt.Sprintf("concurrent-%d", i),
				OS:           "linux",
				OsDisplay:    "Linux",
				AgentVersion: "0.1.0",
				Status:       db.StatusOnline,
			})
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	require.Empty(t, errs, "concurrent UpsertDevice produced errors")

	devices, err := testutil.NewTestDevices(t, store).List(ctx, group.ID)
	require.NoError(t, err)
	assert.Len(t, devices, N, "all concurrent inserts must be visible after wg.Wait")
}

func TestPostgresPreparedStatementCacheReuse(t *testing.T) {
	t.Parallel()
	store, _ := pgStore(t)
	ctx := t.Context()

	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)

	// 200 sequential upserts of distinct rows. The driver caches the
	// prepared statement keyed by SQL text; a pool-config regression that
	// recreates statements on every call would surface here either as a
	// "prepared statement already exists" error from pgx or as exhausting
	// the server-side statement table.
	const N = 200
	for i := range N {
		err := testutil.NewTestDevices(t, store).Upsert(ctx, &device.Device{
			ID:           uuid.New(),
			GroupID:      group.ID,
			Hostname:     fmt.Sprintf("cache-%d", i),
			OS:           "linux",
			OsDisplay:    "Linux",
			AgentVersion: "0.1.0",
			Status:       db.StatusOnline,
		})
		if err != nil {
			require.NoErrorf(t, err, "upsert %d failed; possible prepared-statement cache regression", i)
		}
	}

	// Reset of a single connection should not leak server-side prepared
	// statements either — ensure subsequent queries still work.
	devices, err := testutil.NewTestDevices(t, store).ListAll(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(devices), N)
}

// TestPostgresMalformedUUIDInsertRollbackable proves that a failed UUID
// insert is reported with a recoverable error rather than tearing down
// the connection — important because the pgx driver pools connections
// and a permanently-broken conn would poison the pool for later tests.
func TestPostgresMalformedUUIDInsertRollbackable(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := t.Context()

	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO devices (id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
		VALUES ($1, 'malformed', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', NOW(), NOW(), NOW())
	`, "not-a-uuid")
	require.Error(t, err)

	// The pool must still be usable.
	var n int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT 1`).Scan(&n))
	assert.Equal(t, 1, n)
}
