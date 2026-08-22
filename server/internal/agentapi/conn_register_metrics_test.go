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

// Registration is timed here, where the device row is written, because that is
// the only place the whole operation has happened. A client that stops its own
// clock after handing the register frame to its transport has measured a local
// buffer write, so its number is near zero however slow the server is — which
// is how two load-test ceilings on that number could never fire.

// TestRegisterRecordsServerSideDuration proves a completed registration is
// counted once as ok and lands in the duration histogram.
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

// TestRegisterCountsEveryRegistrationSeparately keeps a reconnect storm
// countable: the rate over this counter is enrollments per second, so a repeat
// registration is its own event rather than an idempotent no-op.
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

// TestRegisterRecordsFailureOutcome — a registration the server could not
// complete is the event a storm-shaped failure shows up as, so it must be
// counted rather than swallowed with the error.
func TestRegisterRecordsFailureOutcome(t *testing.T) {
	store := servertestutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := servertestutil.SeedSite(t, ctx, store)

	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac := newRegisterMetricsConn(t, store, uuid.New(), site.ID, m)
	// Refuse the device write, which is how a registration fails in production:
	// the connection is up and the frame decoded, and the row does not land.
	ac.devices = refusingDevices{Repository: ac.devices}

	require.Error(t, ac.handleRegister(ctx, registerMsg()))

	assert.InDelta(t, 1, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationError)), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(appmetrics.RegistrationOK)), 0)
	assert.Equal(t, 1, countRegistrationObservations(t, m, appmetrics.RegistrationError))
}

// TestRegisterWithoutMetricsDoesNotPanic keeps every connection wired without
// instrumentation — the older test constructions — working unchanged.
func TestRegisterWithoutMetricsDoesNotPanic(t *testing.T) {
	store := servertestutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := servertestutil.SeedSite(t, ctx, store)

	ac := newRegisterMetricsConn(t, store, uuid.New(), site.ID, nil)

	assert.NotPanics(t, func() {
		assert.NoError(t, ac.handleRegister(ctx, registerMsg()))
	})
}

// refusingDevices is a device repository whose write path refuses, so a
// registration fails after the frame has been accepted.
type refusingDevices struct {
	device.Repository
}

func (refusingDevices) Upsert(context.Context, *device.Device) error {
	return errors.New("device store unavailable")
}

// countRegistrationObservations returns how many samples the duration histogram
// holds for one outcome.
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
