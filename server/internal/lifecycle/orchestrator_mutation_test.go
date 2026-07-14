package lifecycle

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequenceSeriesPurger returns configured series counts across verification attempts.
type sequenceSeriesPurger struct {
	counts   []int
	calls    int
	cancelAt int
	cancel   context.CancelFunc
}

// DeleteSeries satisfies SeriesPurger; these tests exercise counting only.
func (*sequenceSeriesPurger) DeleteSeries(context.Context, uuid.UUID, *uuid.UUID) error {
	return nil
}

// CountSeries records each attempt and returns the corresponding configured count.
func (s *sequenceSeriesPurger) CountSeries(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
	s.calls++
	if s.calls == s.cancelAt && s.cancel != nil {
		s.cancel()
	}
	if len(s.counts) == 0 {
		return 1, nil
	}
	return s.counts[min(s.calls-1, len(s.counts)-1)], nil
}

// recordingEdgeDeregistrar captures edge erasure requests for exact assertions.
type recordingEdgeDeregistrar struct {
	agents []uuid.UUID
	orgs   []uuid.UUID
}

// DeregisterAgent records an agent erasure request.
func (r *recordingEdgeDeregistrar) DeregisterAgent(_ context.Context, deviceID uuid.UUID) {
	r.agents = append(r.agents, deviceID)
}

// DeregisterOrg records an organization erasure request.
func (r *recordingEdgeDeregistrar) DeregisterOrg(_ context.Context, orgID uuid.UUID) {
	r.orgs = append(r.orgs, orgID)
}

// TestOrchestratorDefaultsAndOverrides pins the public constructor defaults and overrides.
func TestOrchestratorDefaultsAndOverrides(t *testing.T) {
	defaults := DefaultVerifyConfig()
	assert.Equal(t, 5, defaults.MaxAttempts)
	assert.Equal(t, 500*time.Millisecond, defaults.Interval)

	withDefaults := NewOrchestrator(OrchestratorConfig{})
	assert.Equal(t, defaults, withDefaults.verify)
	assert.Same(t, slog.Default(), withDefaults.logger)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	custom := VerifyConfig{MaxAttempts: 3, Interval: 7 * time.Millisecond}
	withOverrides := NewOrchestrator(OrchestratorConfig{Verify: custom, Logger: logger})
	assert.Equal(t, custom, withOverrides.verify)
	assert.Same(t, logger, withOverrides.logger)
}

// TestOrchestratorVerifyEmptyPinsAttemptBoundaries checks success, exhaustion, and cancellation.
func TestOrchestratorVerifyEmptyPinsAttemptBoundaries(t *testing.T) {
	job := &PurgeJob{OrgID: uuid.New()}

	t.Run("stops when a later count reaches zero", func(t *testing.T) {
		series := &sequenceSeriesPurger{counts: []int{2, 1, 0}}
		orch := NewOrchestrator(OrchestratorConfig{
			Series: series,
			Verify: VerifyConfig{MaxAttempts: 3, Interval: 0},
		})
		empty, err := orch.verifyEmpty(context.Background(), job)
		require.NoError(t, err)
		assert.True(t, empty)
		assert.Equal(t, 3, series.calls)
	})

	t.Run("does not wait after the final attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		series := &sequenceSeriesPurger{counts: []int{1}, cancelAt: 3, cancel: cancel}
		orch := NewOrchestrator(OrchestratorConfig{
			Series: series,
			Verify: VerifyConfig{MaxAttempts: 3, Interval: 0},
		})
		empty, err := orch.verifyEmpty(ctx, job)
		require.NoError(t, err)
		assert.False(t, empty)
		assert.Equal(t, 3, series.calls)
	})

	t.Run("observes cancellation before retrying", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		series := &sequenceSeriesPurger{counts: []int{1}}
		orch := NewOrchestrator(OrchestratorConfig{
			Series: series,
			Verify: VerifyConfig{MaxAttempts: 3, Interval: time.Hour},
		})
		empty, err := orch.verifyEmpty(ctx, job)
		assert.False(t, empty)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, series.calls)
	})
}

// TestOrchestratorPurgeRequestsDeregisterEdgeSubjects pins device and organization erasure calls.
func TestOrchestratorPurgeRequestsDeregisterEdgeSubjects(t *testing.T) {
	t.Run("device", func(t *testing.T) {
		f, ctx, org, device := newSeededPurge(t)
		edge := &recordingEdgeDeregistrar{}
		f.orch.edge = edge

		_, err := f.orch.PurgeDevice(ctx, org, device, nil)
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{device}, edge.agents)
	})

	t.Run("organization and every device", func(t *testing.T) {
		f := newOrchestratorFixture(t)
		ctx := context.Background()
		org := uuid.New()
		devices := []uuid.UUID{
			seedDeviceWithTelemetry(t, f, org),
			seedDeviceWithTelemetry(t, f, org),
		}
		edge := &recordingEdgeDeregistrar{}
		f.orch.edge = edge

		_, err := f.orch.PurgeOrg(ctx, org, nil)
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{org}, edge.orgs)
		for _, device := range devices {
			tombstoned, tombErr := f.tombstone.IsDeviceTombstoned(ctx, org, device)
			require.NoError(t, tombErr)
			assert.True(t, tombstoned)
		}
	})
}
