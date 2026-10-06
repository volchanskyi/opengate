package agentapi

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	servertestutil "github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestRegisterRecordsServerSideDuration(t *testing.T) {
	store := servertestutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := servertestutil.SeedSite(t, ctx, store)

	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac := newRegisterMetricsConn(t, store, uuid.New(), site.ID, m)

	require.NoError(t, ac.handleRegister(ctx, registerMsg()))

	assert.InDelta(t, 1, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationOK)), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationError)), 0)
	assert.Equal(t, 1, countRegistrationObservations(t, m, appmetrics.RegistrationOK))
}

func TestRegisterCountsEveryRegistrationSeparately(t *testing.T) {
	store := servertestutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := servertestutil.SeedSite(t, ctx, store)

	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac := newRegisterMetricsConn(t, store, uuid.New(), site.ID, m)

	for range 3 {
		require.NoError(t, ac.handleRegister(ctx, registerMsg()))
	}

	assert.InDelta(t, 3, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationOK)), 0)
}

func TestRegisterRecordsFailureOutcome(t *testing.T) {
	store := servertestutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := servertestutil.SeedSite(t, ctx, store)

	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac := newRegisterMetricsConn(t, store, uuid.New(), site.ID, m)
	ac.devices = refusingDevices{Repository: ac.devices}

	require.Error(t, ac.handleRegister(ctx, registerMsg()))

	assert.InDelta(t, 1, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationError)), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationOK)), 0)
	assert.Equal(t, 1, countRegistrationObservations(t, m, appmetrics.RegistrationError))
}

func TestRegisterWithoutMetricsDoesNotPanic(t *testing.T) {
	store := servertestutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := servertestutil.SeedSite(t, ctx, store)

	ac := newRegisterMetricsConn(t, store, uuid.New(), site.ID, nil)

	assert.NotPanics(t, func() {
		assert.NoError(t, ac.handleRegister(ctx, registerMsg()))
	})
}

type refusingDevices struct {
	device.Repository
}

func (refusingDevices) Upsert(context.Context, *device.Device) error {
	return errors.New("device store unavailable")
}

func countRegistrationObservations(t *testing.T, m *appmetrics.Metrics, result string) int {
	t.Helper()
	observer, err := m.AgentRegistrationDuration.GetMetricWithLabelValues(result)
	require.NoError(t, err)
	metric := &dto.Metric{}
	require.NoError(t, observer.(prometheus.Metric).Write(metric))
	return int(metric.GetHistogram().GetSampleCount())
}

func newRegisterMetricsConn(t *testing.T, store *db.PostgresStore, deviceID, siteID uuid.UUID, m *appmetrics.Metrics) *AgentConn {
	t.Helper()
	return &AgentConn{
		DeviceID: deviceID,
		SiteID:   siteID,
		stream:   &bytes.Buffer{},
		codec:    &protocol.Codec{},
		devices:  servertestutil.NewTestDevices(t, store),
		hardware: servertestutil.NewTestHardware(t, store),
		metrics:  m,
		logger:   testLogger(),
	}
}
