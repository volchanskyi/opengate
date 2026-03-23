package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

func TestHTTPMiddleware_RecordsRequestCount(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)

	r := chi.NewRouter()
	r.Use(appmetrics.HTTPMiddleware(m))
	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Check counter was incremented
	families, err := reg.Gather()
	require.NoError(t, err)

	found := findMetricFamily(families, "opengate_http_requests_total")
	require.NotNil(t, found, "opengate_http_requests_total not found")

	metric := found.GetMetric()
	require.Len(t, metric, 1)
	assert.Equal(t, float64(1), metric[0].GetCounter().GetValue())

	// Verify labels
	labels := labelMap(metric[0])
	assert.Equal(t, "GET", labels["method"])
	assert.Equal(t, "/api/v1/health", labels["route"])
	assert.Equal(t, "200", labels["status_code"])
}

func TestHTTPMiddleware_RecordsDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)

	r := chi.NewRouter()
	r.Use(appmetrics.HTTPMiddleware(m))
	r.Get("/api/v1/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/abc123", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	families, err := reg.Gather()
	require.NoError(t, err)

	found := findMetricFamily(families, "opengate_http_request_duration_seconds")
	require.NotNil(t, found)

	metric := found.GetMetric()
	require.Len(t, metric, 1)
	assert.Greater(t, metric[0].GetHistogram().GetSampleCount(), uint64(0))

	// Route should use template, not actual path
	labels := labelMap(metric[0])
	assert.Equal(t, "/api/v1/devices/{id}", labels["route"])
}

func TestHTTPMiddleware_CapturesStatusCode(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := appmetrics.NewMetrics(reg)

	r := chi.NewRouter()
	r.Use(appmetrics.HTTPMiddleware(m))
	r.Post("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	families, err := reg.Gather()
	require.NoError(t, err)

	found := findMetricFamily(families, "opengate_http_requests_total")
	require.NotNil(t, found)

	metric := found.GetMetric()
	require.Len(t, metric, 1)
	labels := labelMap(metric[0])
	assert.Equal(t, "POST", labels["method"])
	assert.Equal(t, "401", labels["status_code"])
}

func findMetricFamily(families []*io_prometheus_client.MetricFamily, name string) *io_prometheus_client.MetricFamily {
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

func labelMap(m *io_prometheus_client.Metric) map[string]string {
	result := make(map[string]string)
	for _, lp := range m.GetLabel() {
		result[lp.GetName()] = lp.GetValue()
	}
	return result
}
