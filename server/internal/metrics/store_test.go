package metrics_test

import (
	"context"
	"errors"
	"testing"

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

func TestInstrumentedStore_Close_Delegates(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)
	inner := &mockStore{}
	store := appmetrics.NewInstrumentedStore(inner, m)

	err := store.Close()
	require.NoError(t, err)
}
