package organization_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/organization"
)

// The instrumented wrapper's whole job is to report, per call, how long the
// repository took and whether it worked. The second half is the one worth
// pinning: a wrapper that reported every call as a success would leave the
// repository-error rate reading zero while customers saw failures, and no
// dashboard built on it would ever go red.
//
// So every method is driven twice — once over a repository that answers, once
// over one that refuses — and the recorded verdict is asserted both ways.

type observedCall struct {
	op string
	ok bool
}

type recordingObserver struct {
	calls []observedCall
}

func (r *recordingObserver) Observe(op string, _ time.Duration, ok bool) {
	r.calls = append(r.calls, observedCall{op: op, ok: ok})
}

// stubRepo answers every method, or fails every method with err when set.
type stubRepo struct {
	err error
}

func (s stubRepo) Create(context.Context, *organization.Organization) error { return s.err }

func (s stubRepo) Get(context.Context, organization.ID) (*organization.Organization, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &organization.Organization{ID: uuid.New(), Name: "contoso"}, nil
}

func (s stubRepo) List(context.Context, bool) ([]*organization.Organization, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s stubRepo) Rename(context.Context, organization.ID, string) error { return s.err }

func (s stubRepo) SetArchived(context.Context, organization.ID, bool) error { return s.err }

func (s stubRepo) Delete(context.Context, organization.ID) error { return s.err }

func (s stubRepo) EnsureDefault(context.Context) (organization.ID, error) {
	if s.err != nil {
		return uuid.Nil, s.err
	}
	return uuid.New(), nil
}

// call drives one method of the wrapper and reports the error it returned, so
// the table below states each method once rather than per outcome.
func call(t *testing.T, repo *organization.Instrumented, method string) error {
	t.Helper()
	ctx, id := context.Background(), uuid.New()
	switch method {
	case "organization.Create":
		return repo.Create(ctx, &organization.Organization{ID: id, Name: "contoso"})
	case "organization.Get":
		_, err := repo.Get(ctx, id)
		return err
	case "organization.List":
		_, err := repo.List(ctx, false)
		return err
	case "organization.Rename":
		return repo.Rename(ctx, id, "contoso")
	case "organization.SetArchived":
		return repo.SetArchived(ctx, id, true)
	case "organization.Delete":
		return repo.Delete(ctx, id)
	case "organization.EnsureDefault":
		_, err := repo.EnsureDefault(ctx)
		return err
	}
	t.Fatalf("no such method on the wrapper: %s", method)
	return nil
}

// Every method names itself and reports true when the repository answered.
func TestInstrumentedObservesEveryCallAsASuccess(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		"organization.Create",
		"organization.Get",
		"organization.List",
		"organization.Rename",
		"organization.SetArchived",
		"organization.Delete",
		"organization.EnsureDefault",
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			obs := &recordingObserver{}
			repo := organization.NewInstrumented(stubRepo{}, obs)

			require.NoError(t, call(t, repo, method))

			require.Len(t, obs.calls, 1, "one call must be observed exactly once")
			assert.Equal(t, method, obs.calls[0].op)
			assert.True(t, obs.calls[0].ok, "a call that worked must be recorded as one")
		})
	}
}

// And reports false when it refused. This is the half that keeps the error rate
// honest: the failure reaches the caller either way, so only the recorded
// verdict distinguishes a wrapper that observes the outcome from one that
// observes the attempt.
func TestInstrumentedObservesEveryCallAsAFailure(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		"organization.Create",
		"organization.Get",
		"organization.List",
		"organization.Rename",
		"organization.SetArchived",
		"organization.Delete",
		"organization.EnsureDefault",
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			obs := &recordingObserver{}
			repo := organization.NewInstrumented(stubRepo{err: sql.ErrConnDone}, obs)

			require.ErrorIs(t, call(t, repo, method), sql.ErrConnDone,
				"the wrapper passes the repository's refusal through")

			require.Len(t, obs.calls, 1)
			assert.Equal(t, method, obs.calls[0].op)
			assert.False(t, obs.calls[0].ok, "a call that failed must be recorded as one")
		})
	}
}
