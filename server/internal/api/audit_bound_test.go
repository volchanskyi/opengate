package api

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/db"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// An audited action writes its row on a goroutine so a slow store never holds a
// response open. Unbounded, that is a burst amplifier: every audited request in
// a spike starts one more goroutine competing for the same connection pool, and
// the pool is the thing that was already slow. Its structural sibling —
// telemetry persistence in the agent connection — holds a slot semaphore and
// counts what it sheds. This holds the same shape.

// blockingAudit is an audit repository whose writes park until released, which
// is what a store under load looks like from here.
type blockingAudit struct {
	release  chan struct{}
	inFlight atomic.Int64
	peak     atomic.Int64
	written  atomic.Int64
}

func newBlockingAudit() *blockingAudit {
	return &blockingAudit{release: make(chan struct{})}
}

func (b *blockingAudit) Write(ctx context.Context, _ *audit.Event) error {
	current := b.inFlight.Add(1)
	for {
		peak := b.peak.Load()
		if current <= peak || b.peak.CompareAndSwap(peak, current) {
			break
		}
	}
	defer b.inFlight.Add(-1)
	select {
	case <-b.release:
		b.written.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingAudit) Query(context.Context, audit.Query) ([]*audit.Event, error) {
	return nil, nil
}

// quietAuditLogger keeps the shed-write lines out of the test output.
func quietAuditLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestAuditWritesHoldAConcurrencyBound(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	metrics := appmetrics.NewMetrics(registry)
	repo := newBlockingAudit()
	srv := &Server{
		audit:   repo,
		metrics: metrics,
		logger:  quietAuditLogger(),
	}
	t.Cleanup(func() { close(repo.release) })

	const burst = 200
	var wg sync.WaitGroup
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.auditLog(context.Background(), db.UserID{}, "session.create", "device", "")
		}()
	}
	wg.Wait()

	// Every action is accounted for: the ones that took a slot and the ones that
	// found none. A shed write that nobody counted is the same defect in a
	// quieter form.
	require.Eventually(t, func() bool {
		return repo.inFlight.Load() == int64(auditConcurrentWrites)
	}, 5*time.Second, 10*time.Millisecond,
		"the burst must fill exactly the slots there are")

	assert.LessOrEqual(t, repo.peak.Load(), int64(auditConcurrentWrites),
		"a burst of %d audited actions must never exceed %d concurrent writes", burst, auditConcurrentWrites)

	shed := gatherText(t, registry)
	assert.True(t, strings.Contains(shed, `opengate_audit_writes_total{result="shed"}`),
		"a shed audit write must be counted, not lost; got:\n%s", shed)
}

// The ordinary case: a store that answers writes the row and says so, or the
// arm above proves only that the bound exists and never that anything passes
// through it.
func TestAnAuditedActionWritesItsRow(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	metrics := appmetrics.NewMetrics(registry)
	repo := newBlockingAudit()
	close(repo.release)

	srv := &Server{
		audit:   repo,
		metrics: metrics,
		logger:  quietAuditLogger(),
	}

	srv.auditLog(context.Background(), db.UserID{}, "session.create", "device", "")

	require.Eventually(t, func() bool {
		return repo.written.Load() == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		return strings.Contains(gatherText(t, registry), `opengate_audit_writes_total{result="written"}`)
	}, 5*time.Second, 10*time.Millisecond)
}
