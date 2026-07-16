package faulttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canceledContext returns an already-canceled context.
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestSpecApply covers the non-blocking, non-panicking actions in one table:
// each either delegates to the real call or returns a specific typed error.
func TestSpecApply(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("custom boundary error")

	tests := []struct {
		name         string
		spec         Spec
		ctx          context.Context
		wantDelegate bool
		wantErr      error
	}{
		{"none delegates", Spec{Action: ActionNone}, context.Background(), true, nil},
		{"unknown action delegates", Spec{Action: Action(999)}, context.Background(), true, nil},
		{"error returns default", Spec{Action: ActionError}, context.Background(), false, ErrInjected},
		{"error returns override", Spec{Action: ActionError, Err: sentinel}, context.Background(), false, sentinel},
		{"timeout waits then returns ctx error", Spec{Action: ActionTimeout}, canceledContext(), false, context.Canceled},
		{"blocked waits then returns ctx error", Spec{Action: ActionBlocked}, canceledContext(), false, context.Canceled},
		{"delay exits on cancel", Spec{Action: ActionDelay, Delay: time.Hour}, canceledContext(), false, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delegate, err := tt.spec.apply(tt.ctx)
			assert.Equal(t, tt.wantDelegate, delegate)
			if tt.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestSpecTimeoutReturnsDeadlineExceeded(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	delegate, err := Spec{Action: ActionTimeout}.apply(ctx)
	assert.False(t, delegate)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSpecDelayDelegatesAfterWait(t *testing.T) {
	t.Parallel()
	start := time.Now()
	delegate, err := Spec{Action: ActionDelay, Delay: 20 * time.Millisecond}.apply(context.Background())
	assert.True(t, delegate)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestSpecPanic(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t, ErrInjected, func() {
		_, _ = Spec{Action: ActionPanic}.apply(context.Background())
	})
	assert.PanicsWithValue(t, "boom", func() {
		_, _ = Spec{Action: ActionPanic, PanicValue: "boom"}.apply(context.Background())
	})
}

// TestSpecBlockedDoesNotReturnUntilCancel proves the blocked action holds until
// its context is canceled, then exits — a hung dependency freed only by
// cancellation.
func TestSpecBlockedDoesNotReturnUntilCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Spec{Action: ActionBlocked}.apply(ctx)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("blocked action returned before context cancellation")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("blocked action did not exit after context cancellation")
	}
}

func TestFaultSetArmClearIsMethodScoped(t *testing.T) {
	t.Parallel()
	fs := newFaultSet()

	// Unarmed method delegates.
	delegate, err := fs.apply(context.Background(), "Get")
	require.True(t, delegate)
	require.NoError(t, err)

	fs.arm("Get", Spec{Action: ActionError})
	delegate, err = fs.apply(context.Background(), "Get")
	assert.False(t, delegate)
	assert.ErrorIs(t, err, ErrInjected)

	// A non-Once fault persists — a second apply still fires.
	delegate, err = fs.apply(context.Background(), "Get")
	assert.False(t, delegate)
	assert.ErrorIs(t, err, ErrInjected)

	// A different method is unaffected — proving method-scoped isolation.
	delegate, _ = fs.apply(context.Background(), "List")
	assert.True(t, delegate)

	fs.clear("Get")
	delegate, err = fs.apply(context.Background(), "Get")
	assert.True(t, delegate)
	assert.NoError(t, err)
}

func TestFaultSetOnceAutoClears(t *testing.T) {
	t.Parallel()
	fs := newFaultSet()
	fs.arm("Get", Spec{Action: ActionError, Once: true})

	delegate, err := fs.apply(context.Background(), "Get")
	assert.False(t, delegate)
	assert.ErrorIs(t, err, ErrInjected)

	// The second call sees the fault already cleared.
	delegate, err = fs.apply(context.Background(), "Get")
	assert.True(t, delegate)
	assert.NoError(t, err)
}
