package audit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
)

// stubRepo is a minimal Repository test double that records the last
// query it received and returns a canned event slice.
type stubRepo struct {
	gotQuery audit.Query
	events   []*audit.Event
	err      error
}

func (s *stubRepo) Write(context.Context, *audit.Event) error { return nil }

func (s *stubRepo) Query(_ context.Context, q audit.Query) ([]*audit.Event, error) {
	s.gotQuery = q
	return s.events, s.err
}

// The audit module's Handlers struct is the
// per-domain use-case layer. The api package's transport handler delegates
// to ListEvents — passing the parsed query through and returning the raw
// domain slice. These tests pin the contract (single delegation, no
// translation, error pass-through).

func TestHandlers_ListEvents_DelegatesQueryToRepository(t *testing.T) {
	uid := uuid.New()
	q := audit.Query{
		UserID: &uid,
		Action: "device.restart",
		Limit:  25,
		Offset: 100,
	}
	repo := &stubRepo{events: []*audit.Event{{ID: 1}, {ID: 2}}}
	h := audit.NewHandlers(repo)

	events, err := h.ListEvents(context.Background(), q)

	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, int64(1), events[0].ID)
	require.Equal(t, q, repo.gotQuery, "Handlers.ListEvents must pass the Query through unchanged")
}

func TestHandlers_ListEvents_PassesThroughRepositoryError(t *testing.T) {
	repo := &stubRepo{err: errFake}
	h := audit.NewHandlers(repo)

	events, err := h.ListEvents(context.Background(), audit.Query{})

	require.ErrorIs(t, err, errFake)
	require.Nil(t, events)
}

func TestHandlers_ListEvents_EmptyResultReturnsEmptySlice(t *testing.T) {
	repo := &stubRepo{events: []*audit.Event{}}
	h := audit.NewHandlers(repo)

	events, err := h.ListEvents(context.Background(), audit.Query{Limit: 50})

	require.NoError(t, err)
	require.Empty(t, events)
}

// errFake mirrors the no-repo-side-error case; using a package-level sentinel
// so require.ErrorIs is the matcher rather than string comparison.
type errFakeT struct{}

func (errFakeT) Error() string { return "fake" }

var errFake = errFakeT{}
