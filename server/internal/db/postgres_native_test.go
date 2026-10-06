package db_test

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
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func pgStore(t *testing.T) (*db.PostgresStore, *sql.DB) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return store, store.DB()
}

// seedDeviceRow inserts a device row directly because the upsert sets last_seen to NOW().
func seedDeviceRow(t *testing.T, ctx context.Context, sqlDB *sql.DB, id uuid.UUID, lastSeen time.Time) {
	t.Helper()
	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO devices (id, tenant_id, organization_id, site_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
		VALUES ($1, $2, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 ORDER BY o.created_at LIMIT 1), NULL, 'native-test', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', $3, NOW(), NOW())
	`, id, dbtx.DefaultTenantID, lastSeen)
	require.NoError(t, err)
}

func TestPostgresTIMESTAMPTZNormalizesNonUTCOffsetToUTC(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	// IST is UTC+05:30, a non-whole-hour offset; Postgres stores microsecond precision.
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

	// pgx may return the time in time.Local, so only the instant is compared.
	assert.True(t, got.Equal(wantUTC),
		"want UTC instant %v; got %v", wantUTC, got)
	assert.Equal(t, wantUTC.UnixMicro(), got.UnixMicro(),
		"microsecond precision must survive the round-trip")
}

func TestPostgresJSONBNetworkInterfacesRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := pgStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	dev := testutil.SeedDevice(t, ctx, store, site.ID)

	originals := []device.NetworkInterfaceInfo{
		{Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff",
			IPv4: []string{"10.0.0.1"},
			IPv6: []string{"fe80::1", "2001:db8::1"}},
		{Name: "测试-🌐", MAC: "",
			IPv4: []string{},
			IPv6: []string{}},
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

	wantJSON, err := json.Marshal(originals)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got.NetworkInterfaces)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), string(gotJSON))

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
					INSERT INTO devices (id, tenant_id, organization_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
					VALUES ($1, $2, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 ORDER BY o.created_at LIMIT 1), 'x', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', NOW(), NOW(), NOW())
				`, tt.raw, dbtx.DefaultTenantID)
			require.Error(t, err, "postgres must reject malformed UUID %q", tt.raw)
			assert.Contains(t, strings.ToLower(err.Error()), "invalid input syntax")
		})
	}
}

func TestPostgresUUIDAcceptsAllCases(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	canonical := "550e8400-e29b-41d4-a716-446655440000"
	tests := []struct {
		name string
		raw  string
	}{
		{"all lowercase", canonical},
		{"all uppercase", strings.ToUpper(canonical)},
		{"mixed case", "550E8400-e29B-41D4-a716-446655440000"},
		{"no dashes", strings.ReplaceAll(canonical, "-", "")},
	}
	want := uuid.MustParse(canonical)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sqlDB.ExecContext(ctx, `
					INSERT INTO devices (id, tenant_id, organization_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
					VALUES ($1, $2, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 ORDER BY o.created_at LIMIT 1), 'case-test', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', NOW(), NOW(), NOW())
					ON CONFLICT (id) DO UPDATE SET hostname = EXCLUDED.hostname
				`, tt.raw, dbtx.DefaultTenantID)
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
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)

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
				SiteID:       site.ID,
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

	devices, err := testutil.NewTestDevices(t, store).List(ctx, device.Filter{SiteID: site.ID})
	require.NoError(t, err)
	assert.Len(t, devices, N, "all concurrent inserts must be visible after wg.Wait")
}

func TestPostgresPreparedStatementCacheReuse(t *testing.T) {
	t.Parallel()
	store, _ := pgStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)

	const N = 200
	for i := range N {
		err := testutil.NewTestDevices(t, store).Upsert(ctx, &device.Device{
			ID:           uuid.New(),
			SiteID:       site.ID,
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

	devices, err := testutil.NewTestDevices(t, store).List(ctx, device.Filter{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(devices), N)
}

func TestPostgresMalformedUUIDInsertRollbackable(t *testing.T) {
	t.Parallel()
	_, sqlDB := pgStore(t)
	ctx := t.Context()

	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO devices (id, tenant_id, organization_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
		VALUES ($1, $2, (SELECT o.id FROM organizations o WHERE o.tenant_id = $2 ORDER BY o.created_at LIMIT 1), 'malformed', 'linux', 'Linux', '0.1.0', '[]'::jsonb, 'online', NOW(), NOW(), NOW())
	`, "not-a-uuid", dbtx.DefaultTenantID)
	require.Error(t, err)

	var n int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT 1`).Scan(&n))
	assert.Equal(t, 1, n)
}
