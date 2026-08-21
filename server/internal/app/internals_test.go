package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
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

// TestCleanupRelaySessionUsesBackgroundDelete covers the callback the relay
// fires when a session ends: the row must go by the same token the relay held.
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

// TestRuleIDsAreTheWholeShippedCatalogue keeps the vocabulary the investigation
// series are bounded by equal to the rules this build actually ships. A rule
// missing from it would still fire, still be stored, and be counted under the
// catch-all — visible as a metric nobody can attribute rather than as a rollout
// nobody can read.
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

// TestBudgetsAreBounded pins that the boot-time database work and the
// per-session cleanup both carry a deadline. An unbounded startup query turns a
// slow database into a server that never finishes booting and never says why.
func TestBudgetsAreBounded(t *testing.T) {
	t.Parallel()
	assert.Positive(t, startupWorkBudget)
	assert.Positive(t, relayCleanupBudget)
	assert.Less(t, relayCleanupBudget, startupWorkBudget)
	assert.GreaterOrEqual(t, jwtTokenLifetime, time.Hour)
}

// TestTelemetryPortsAreAllOrNothing states the invariant the erasure path
// depends on: either every face of the metrics store is wired, or none is. A
// half-wired set would give the device page a reader while leaving a deleted
// machine's series behind for ever.
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

// stubOperator stands in for management hardware the test host cannot reach.
type stubOperator struct{ amt.Operator }

// TestAMTOperatorPrefersTheSuppliedStandIn pins the one edge a harness may
// replace. Without a stand-in the assembled service is what the API talks to,
// which is what the shipped binary does.
func TestAMTOperatorPrefersTheSuppliedStandIn(t *testing.T) {
	t.Parallel()

	svc := &amt.Service{}
	assert.Same(t, svc, amtOperator(Config{}, svc))

	stand := &stubOperator{}
	assert.Same(t, stand, amtOperator(Config{AMTOperator: stand}, svc))
}
