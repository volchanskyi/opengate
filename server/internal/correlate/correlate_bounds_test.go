package correlate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCorrelatePassesTenantScopeToFetcher(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{seriesWith("cpu", 10, 500)}}
	e := newTestEngine(t, f, nil)
	org := uuid.New()
	req := stdRequest()
	if _, err := e.Correlate(context.Background(), org, req); err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gotOrg != org {
		t.Errorf("fetcher org = %v, want %v (tenant scope must be propagated)", f.gotOrg, org)
	}
	if f.gotDevice != req.DeviceID {
		t.Errorf("fetcher device = %v, want %v", f.gotDevice, req.DeviceID)
	}
	// Fetch must span baseline start through focus end.
	if !f.gotStart.Equal(req.BaselineStart) || !f.gotEnd.Equal(req.FocusEnd) {
		t.Errorf("fetch window = [%v,%v], want [%v,%v]", f.gotStart, f.gotEnd, req.BaselineStart, req.FocusEnd)
	}
}

func TestCorrelateDefaultBaselineIsPrecedingWindow(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{seriesWith("cpu", 10, 500)}}
	e := newTestEngine(t, f, nil)
	req := Request{DeviceID: uuid.New(), FocusStart: focusStart(), FocusEnd: focusEnd()}
	if _, err := e.Correlate(context.Background(), uuid.New(), req); err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// focus is 10m wide, so the implicit baseline starts 10m before focus.
	wantStart := focusStart().Add(-10 * time.Minute)
	if !f.gotStart.Equal(wantStart) {
		t.Errorf("default baseline start = %v, want %v", f.gotStart, wantStart)
	}
}

func TestCorrelateRejectsInvalidWindow(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t, &fakeFetcher{}, nil)
	req := Request{DeviceID: uuid.New(), FocusStart: focusEnd(), FocusEnd: focusStart()} // end before start
	if _, err := e.Correlate(context.Background(), uuid.New(), req); !errors.Is(err, ErrInvalidWindow) {
		t.Fatalf("invalid window: got %v, want ErrInvalidWindow", err)
	}
}

func TestCorrelateConcurrencyLimitRejects(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{seriesWith("cpu", 10, 500)}, block: make(chan struct{})}
	e := newTestEngine(t, f, func(c *Config) { c.MaxConcurrent = 1 })

	done := make(chan error, 1)
	go func() {
		_, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
		done <- err
	}()
	// Wait until the first call has entered Fetch and is holding the only slot.
	waitFor(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.calls == 1 })

	if _, err := e.Correlate(context.Background(), uuid.New(), stdRequest()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second concurrent call: got %v, want ErrBusy", err)
	}
	close(f.block)
	if err := <-done; err != nil {
		t.Fatalf("first call: %v", err)
	}
}

func TestCorrelateTimeoutFires(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{block: make(chan struct{}), respectsCtx: true}
	e := newTestEngine(t, f, func(c *Config) { c.Timeout = 30 * time.Millisecond })
	_, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: got %v, want context.DeadlineExceeded", err)
	}
	close(f.block)
}
