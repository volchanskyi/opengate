package metrics

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// InstrumentedStore wraps a db.Store and records query duration and count
// metrics for every operation.
type InstrumentedStore struct {
	inner   db.Store
	metrics *Metrics
}

// NewInstrumentedStore wraps the given store with Prometheus instrumentation.
func NewInstrumentedStore(store db.Store, m *Metrics) *InstrumentedStore {
	return &InstrumentedStore{inner: store, metrics: m}
}

func (s *InstrumentedStore) observe(operation string, start time.Time, err error) {
	s.metrics.Observe(operation, time.Since(start), err == nil)
}

// Observe records a single DB-shaped operation against the standard db_query_*
// metric pair. It lets audit.Instrumented (and other repository decorators
// added under ADR-021) reuse the same dashboards without importing this
// package or duplicating label discipline.
func (m *Metrics) Observe(operation string, duration time.Duration, ok bool) {
	status := "ok"
	if !ok {
		status = "error"
	}
	m.DBQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
	m.DBQueriesTotal.WithLabelValues(operation, status).Inc()
}

// --- Health ------------------------------------------------------------------

// Ping instruments db.Store.Ping.
func (s *InstrumentedStore) Ping(ctx context.Context) error {
	start := time.Now()
	err := s.inner.Ping(ctx)
	s.observe("Ping", start, err)
	return err
}

// Close instruments db.Store.Close.
func (s *InstrumentedStore) Close() error {
	return s.inner.Close()
}
