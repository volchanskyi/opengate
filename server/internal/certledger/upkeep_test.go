package certledger

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneDeletesOnlyRowsThatCanAdmitNothing(t *testing.T) {
	s := newLedger(t)
	device := uuid.New()
	ancient := record(t, s, leaf(t, device, t0.Add(-400*24*time.Hour)), issuerA, SourceEnroll)
	s.MarkSeen(ancient, t0)
	recent := record(t, s, leaf(t, device, t0.Add(-100*24*time.Hour)), issuerA, SourceRenew)
	s.MarkSeen(recent, t0)
	current := record(t, s, leaf(t, device, t0.Add(-10*24*time.Hour)), issuerA, SourceRenew)
	s.MarkSeen(current, t0)
	lonely := record(t, s, leaf(t, uuid.New(), t0.Add(-400*24*time.Hour)), issuerA, SourceEnroll)

	n, err := s.Prune(context.Background(), t0, 90*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the superseded certificate past expiry and grace goes")
	_, ok := s.Lookup(ancient)
	assert.False(t, ok)
	for _, kept := range []Fingerprint{recent, current, lonely} {
		_, ok := s.Lookup(kept)
		assert.True(t, ok)
	}
	assert.Equal(t, 3, rowCount(t, s))
}

func TestRenewalsSinceCount(t *testing.T) {
	s := newLedger(t)
	device := uuid.New()
	record(t, s, leaf(t, device, t0.Add(-48*time.Hour)), issuerA, SourceRenew)
	record(t, s, leaf(t, device, t0.Add(-2*time.Hour)), issuerA, SourceRenew)
	record(t, s, leaf(t, device, t0.Add(-time.Hour)), issuerA, SourceRenew)
	record(t, s, leaf(t, device, t0.Add(-time.Hour)), issuerA, SourceEnroll)
	record(t, s, leaf(t, uuid.New(), t0.Add(-time.Hour)), issuerA, SourceRenew)

	n, err := s.RenewalsSince(context.Background(), device, t0.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	oldest, err := s.OldestRenewalSince(context.Background(), device, t0.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.True(t, oldest.Equal(t0.Add(-2*time.Hour)))
}

func TestCensusByNewestIssuer(t *testing.T) {
	s := newLedger(t)
	moved := uuid.New()
	record(t, s, leaf(t, moved, t0), issuerA, SourceEnroll)
	record(t, s, leaf(t, moved, t0.Add(time.Hour)), issuerB, SourceRenew)
	record(t, s, leaf(t, uuid.New(), t0), issuerA, SourceEnroll)
	record(t, s, leaf(t, uuid.New(), t0), issuerA, SourceEnroll)

	census := s.CensusByNewestIssuer()
	require.Len(t, census, 2)
	assert.Equal(t, issuerA, census[0].IssuerHash)
	assert.Equal(t, 2, census[0].Devices)
	assert.Equal(t, issuerB, census[1].IssuerHash)
	assert.Equal(t, 1, census[1].Devices)
}
