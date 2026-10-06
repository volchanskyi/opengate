package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// quietTestLogger keeps assembly chatter out of the test output.
func quietTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type relayCleanupRepo struct {
	session.Repository
	token string
	err   error
}

func (r *relayCleanupRepo) DeleteRelaySession(_ context.Context, token string) error {
	r.token = token
	return r.err
}

func TestCleanupRelaySessionUsesBackgroundDelete(t *testing.T) {
	t.Parallel()
	token := protocol.GenerateSessionToken()

	t.Run("deletes by relay token", func(t *testing.T) {
		repo := &relayCleanupRepo{}
		require.NoError(t, cleanupRelaySession(repo, token))
		assert.Equal(t, string(token), repo.token)
	})

	t.Run("propagates repository failure", func(t *testing.T) {
		want := errors.New("delete failed")
		repo := &relayCleanupRepo{err: want}
		assert.ErrorIs(t, cleanupRelaySession(repo, token), want)
	})
}

func TestRuleIDsAreTheWholeShippedCatalogue(t *testing.T) {
	t.Parallel()
	catalogue, err := rules.Embedded()
	require.NoError(t, err)

	ids := ruleIDs(catalogue)

	shipped := catalogue.All()
	require.NotEmpty(t, shipped)
	assert.Len(t, ids, len(shipped))
	for _, def := range shipped {
		assert.Containsf(t, ids, def.ID, "%s is a shipped rule and belongs in the vocabulary", def.ID)
	}
}

func TestBudgetsAreBounded(t *testing.T) {
	t.Parallel()
	assert.Positive(t, startupWorkBudget)
	assert.Positive(t, relayCleanupBudget)
	assert.Less(t, relayCleanupBudget, startupWorkBudget)
	assert.GreaterOrEqual(t, jwtTokenLifetime, time.Hour)
}

func TestTelemetryPortsAreAllOrNothing(t *testing.T) {
	t.Parallel()

	off := newTelemetryPorts(Config{}, quietTestLogger())
	assert.Nil(t, off.writer)
	assert.Nil(t, off.reader)
	assert.Nil(t, off.purger)
	assert.Nil(t, off.inventory)

	on := newTelemetryPorts(Config{VictoriaMetricsURL: "http://127.0.0.1:8428", VMDeleteAuthKey: "k"}, quietTestLogger())
	assert.NotNil(t, on.writer)
	assert.NotNil(t, on.reader)
	assert.NotNil(t, on.purger)
	assert.NotNil(t, on.inventory)
}

func TestTheTelemetryWriterStampsTheServersEnvironment(t *testing.T) {
	ports := newTelemetryPorts(Config{VictoriaMetricsURL: testvm.BaseURL(t), Namespace: "opengate-staging"}, quietTestLogger())
	client, ok := ports.writer.(*telemetry.VMClient)
	require.True(t, ok, "the writer is the metrics store client")

	ctx := context.Background()
	tenant, device := uuid.New(), uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, client.WriteSamples(ctx, tenant, device, []telemetry.Sample{{
		Name: "opengate_test_app_env_metric", Value: 1, TS: ts,
	}}))
	require.NoError(t, client.Flush(ctx))

	series, err := client.Export(ctx, tenant, `opengate_test_app_env_metric{device_id="`+device.String()+`"}`,
		ts.Add(-time.Minute), ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, "opengate-staging", series[0].Metric["namespace"])
}

// stubOperator stands in for management hardware the test host cannot reach.
type stubOperator struct{ amt.Operator }

func TestAMTOperatorPrefersTheSuppliedStandIn(t *testing.T) {
	t.Parallel()

	svc := &amt.Service{}
	assert.Same(t, svc, amtOperator(Config{}, svc))

	stand := &stubOperator{}
	assert.Same(t, stand, amtOperator(Config{AMTOperator: stand}, svc))
}
