package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// mockStore implements the minimum db.Store methods needed for tests.
type mockStore struct {
	db.Store
	pingErr error
}

func (m *mockStore) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *mockStore) Close() error {
	return nil
}

func (m *mockStore) ListAllDevices(ctx context.Context) ([]*db.Device, error) {
	return []*db.Device{}, nil
}

// mockStoreWithLogs extends mockStore with device log methods.
type mockStoreWithLogs struct {
	mockStore
}

func (m *mockStoreWithLogs) UpsertDeviceLogs(_ context.Context, _ db.DeviceID, _ []db.DeviceLogEntry) error {
	return nil
}

func (m *mockStoreWithLogs) QueryDeviceLogs(_ context.Context, _ db.DeviceID, _ db.LogFilter) ([]db.DeviceLogEntry, int, error) {
	return nil, 0, nil
}

func (m *mockStoreWithLogs) HasRecentLogs(_ context.Context, _ db.DeviceID, _ time.Duration) (bool, error) {
	return false, nil
}

func TestInstrumentedStore_Ping_Success(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)
	inner := &mockStore{}
	store := appmetrics.NewInstrumentedStore(inner, m)

	err := store.Ping(context.Background())
	require.NoError(t, err)

	families, err := reg.Gather()
	require.NoError(t, err)

	// Check counter
	counter := findMetricFamily(families, "opengate_db_queries_total")
	require.NotNil(t, counter)

	metric := counter.GetMetric()
	require.Len(t, metric, 1)
	assert.Equal(t, float64(1), metric[0].GetCounter().GetValue())

	labels := labelMap(metric[0])
	assert.Equal(t, "Ping", labels["operation"])
	assert.Equal(t, "ok", labels["status"])

	// Check histogram
	hist := findMetricFamily(families, "opengate_db_query_duration_seconds")
	require.NotNil(t, hist)

	hm := hist.GetMetric()
	require.Len(t, hm, 1)
	assert.Greater(t, hm[0].GetHistogram().GetSampleCount(), uint64(0))
}

func TestInstrumentedStore_Ping_Error(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)
	inner := &mockStore{pingErr: errors.New("db down")}
	store := appmetrics.NewInstrumentedStore(inner, m)

	err := store.Ping(context.Background())
	require.Error(t, err)

	families, err := reg.Gather()
	require.NoError(t, err)

	counter := findMetricFamily(families, "opengate_db_queries_total")
	require.NotNil(t, counter)

	metric := counter.GetMetric()
	require.Len(t, metric, 1)
	labels := labelMap(metric[0])
	assert.Equal(t, "Ping", labels["operation"])
	assert.Equal(t, "error", labels["status"])
}

func TestInstrumentedStore_ListAllDevices(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)
	inner := &mockStore{}
	store := appmetrics.NewInstrumentedStore(inner, m)

	devices, err := store.ListAllDevices(context.Background())
	require.NoError(t, err)
	assert.Empty(t, devices)

	families, err := reg.Gather()
	require.NoError(t, err)

	counter := findMetricFamily(families, "opengate_db_queries_total")
	require.NotNil(t, counter)

	// Find the ListAllDevices metric
	var found bool
	for _, metric := range counter.GetMetric() {
		labels := labelMap(metric)
		if labels["operation"] == "ListAllDevices" {
			assert.Equal(t, "ok", labels["status"])
			assert.Equal(t, float64(1), metric.GetCounter().GetValue())
			found = true
		}
	}
	assert.True(t, found, "ListAllDevices metric not found")
}

func TestInstrumentedStore_DeviceLogMethods(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)
	inner := &mockStoreWithLogs{}
	store := appmetrics.NewInstrumentedStore(inner, m)
	ctx := context.Background()
	deviceID := uuid.New()

	t.Run("UpsertDeviceLogs", func(t *testing.T) {
		err := store.UpsertDeviceLogs(ctx, deviceID, []db.DeviceLogEntry{
			{DeviceID: deviceID, Level: "INFO", Message: "test"},
		})
		require.NoError(t, err)
	})

	t.Run("QueryDeviceLogs", func(t *testing.T) {
		entries, total, err := store.QueryDeviceLogs(ctx, deviceID, db.LogFilter{Limit: 10})
		require.NoError(t, err)
		assert.Empty(t, entries)
		assert.Equal(t, 0, total)
	})

	t.Run("HasRecentLogs", func(t *testing.T) {
		ok, err := store.HasRecentLogs(ctx, deviceID, 5*time.Minute)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	// Verify metrics were recorded
	families, err := reg.Gather()
	require.NoError(t, err)
	counter := findMetricFamily(families, "opengate_db_queries_total")
	require.NotNil(t, counter)

	ops := make(map[string]bool)
	for _, metric := range counter.GetMetric() {
		labels := labelMap(metric)
		ops[labels["operation"]] = true
	}
	assert.True(t, ops["UpsertDeviceLogs"])
	assert.True(t, ops["QueryDeviceLogs"])
	assert.True(t, ops["HasRecentLogs"])
}

func TestInstrumentedStore_Close_Delegates(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)
	inner := &mockStore{}
	store := appmetrics.NewInstrumentedStore(inner, m)

	err := store.Close()
	require.NoError(t, err)
}
