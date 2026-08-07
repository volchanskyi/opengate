package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const testPathDeviceSummary = "/api/v1/devices/summary"

// seedSummaryDevice inserts one device with the given status into the default
// tenant, optionally in maintenance.
func seedSummaryDevice(t *testing.T, srv *Server, status device.DeviceStatus, maintenance bool) *device.Device {
	t.Helper()
	ctx := testTenantContext(t)
	d := &device.Device{ID: uuid.New(), Hostname: "sum-" + uuid.New().String()[:8], OS: "linux", Status: status}
	require.NoError(t, srv.devices.Upsert(ctx, d))
	if maintenance {
		require.NoError(t, srv.devices.SetMaintenance(ctx, d.ID, true, uuid.New(), "patching"))
	}
	return d
}

func fetchSummary(t *testing.T, srv *Server, token string) DeviceSummary {
	t.Helper()
	w := doRequest(srv, http.MethodGet, testPathDeviceSummary, token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var got DeviceSummary
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	return got
}

func TestGetDeviceSummary(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "summary@example.com", false)

	seedSummaryDevice(t, srv, db.StatusOnline, false)
	seedSummaryDevice(t, srv, db.StatusOnline, true)
	seedSummaryDevice(t, srv, db.StatusOffline, false)
	seedSummaryDevice(t, srv, db.StatusConnecting, false)

	t.Run("status counts", func(t *testing.T) {
		srv.telemetryReader = &fakeMetricsReader{}
		got := fetchSummary(t, srv, token)
		assert.Equal(t, 4, got.Total)
		assert.Equal(t, 2, got.Online)
		assert.Equal(t, 2, got.Offline, "a connecting device counts as offline")
		assert.Equal(t, 1, got.Maintenance)
		assert.Equal(t, got.Total, got.Online+got.Offline)
	})

	t.Run("health bands and unknown remainder", func(t *testing.T) {
		fake := &fakeMetricsReader{bands: telemetry.BandCounts{Anomalous: 1, Watch: 1, Healthy: 1}}
		srv.telemetryReader = fake
		got := fetchSummary(t, srv, token)
		assert.Equal(t, 1, got.Health.Anomalous)
		assert.Equal(t, 1, got.Health.Watch)
		assert.Equal(t, 1, got.Health.Healthy)
		assert.Equal(t, 1, got.Health.Unknown, "the fourth device reported no rate")
		assert.Equal(t, 1, fake.bandsSeen, "one instant query per request, whatever the fleet size")
		assert.Equal(t, watchThreshold, fake.bandsWatch)
		assert.Equal(t, anomalousThreshold, fake.bandsAnom)
	})

	t.Run("unknown never goes negative", func(t *testing.T) {
		// More banded devices than the tenant holds (a sample that outlived its
		// device) must not push unknown below zero.
		srv.telemetryReader = &fakeMetricsReader{bands: telemetry.BandCounts{Anomalous: 9, Watch: 9, Healthy: 9}}
		got := fetchSummary(t, srv, token)
		assert.Equal(t, 0, got.Health.Unknown)
	})

	t.Run("bands zeroed when telemetry is not configured", func(t *testing.T) {
		srv.telemetryReader = nil
		got := fetchSummary(t, srv, token)
		assert.Equal(t, 4, got.Total, "the tiles still render without telemetry")
		assert.Equal(t, FleetHealthCounts{Unknown: 4}, got.Health)
	})

	t.Run("bands zeroed when the telemetry query fails", func(t *testing.T) {
		srv.telemetryReader = &fakeMetricsReader{bandsErr: assertAnErr}
		got := fetchSummary(t, srv, token)
		assert.Equal(t, 4, got.Total)
		assert.Equal(t, FleetHealthCounts{Unknown: 4}, got.Health)
	})

	t.Run("summary does not route into GetDevice", func(t *testing.T) {
		srv.telemetryReader = &fakeMetricsReader{}
		w := doRequest(srv, http.MethodGet, testPathDeviceSummary, token, nil)
		require.Equal(t, http.StatusOK, w.Code)
		// A device payload would carry a hostname; the summary never does.
		assert.NotContains(t, w.Body.String(), "hostname")
	})

	t.Run("unauthenticated is refused", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDeviceSummary, "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// TestGetDeviceSummaryIsTenantScoped proves the deliberate scope choice: the
// summary always describes the caller's own tenant, so its tiles and its
// health bands cover one device set.
func TestGetDeviceSummaryIsTenantScoped(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	seedSummaryDevice(t, srv, db.StatusOnline, false)

	outsider, _ := seedTestUser(t, srv, cfg, "summary-outsider@example.com", false)
	outsiderToken, err := cfg.GenerateToken(outsider.ID, outsider.Email, false, uuid.New())
	require.NoError(t, err)

	srv.telemetryReader = &fakeMetricsReader{}
	got := fetchSummary(t, srv, outsiderToken)
	assert.Equal(t, DeviceSummary{}, got, "another tenant's fleet is invisible")
}

// TestGetDeviceSummaryIsConstantSize proves the payload does not grow with the
// fleet. The response is a fixed set of keys holding integers, so growing the
// fleet 51-fold may only widen those integers by a digit or two — it can never
// add a field, and it can never add a per-device entry.
func TestGetDeviceSummaryIsConstantSize(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "summary-size@example.com", false)
	srv.telemetryReader = &fakeMetricsReader{}

	seedSummaryDevice(t, srv, db.StatusOnline, false)
	small := doRequest(srv, http.MethodGet, testPathDeviceSummary, token, nil)
	require.Equal(t, http.StatusOK, small.Code)
	smallBody := small.Body.String()

	for range 50 {
		seedSummaryDevice(t, srv, db.StatusOnline, false)
	}
	large := doRequest(srv, http.MethodGet, testPathDeviceSummary, token, nil)
	require.Equal(t, http.StatusOK, large.Code)
	largeBody := large.Body.String()

	assert.Equal(t, jsonShape(t, smallBody), jsonShape(t, largeBody),
		"the payload's shape must not change with fleet size")
	// 51 devices instead of 1 widens total/online/unknown by one digit each.
	assert.LessOrEqual(t, len(largeBody)-len(smallBody), 3,
		"a 51-fold fleet may only widen the integers, never add structure")
}

// jsonShape reduces a JSON object to its sorted key path set, so two payloads
// can be compared for structure while ignoring the values.
func jsonShape(t *testing.T, body string) []string {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))

	var paths []string
	var walk func(prefix string, v map[string]any)
	walk = func(prefix string, v map[string]any) {
		for k, val := range v {
			path := prefix + k
			paths = append(paths, path)
			if nested, ok := val.(map[string]any); ok {
				walk(path+".", nested)
			}
		}
	}
	walk("", decoded)
	sort.Strings(paths)
	return paths
}
