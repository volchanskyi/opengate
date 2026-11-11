package telemetry_test

import (
	"bytes"
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// fakeStore records every request body; hold, when set, runs before a request is answered.
type fakeStore struct {
	mu     sync.Mutex
	bodies [][]byte
	hold   func(body []byte)
	fail   atomic.Pointer[error]
}

func (s *fakeStore) Import(ctx context.Context, body []byte) error {
	if s.hold != nil {
		s.hold(body)
	}
	if err := s.fail.Load(); err != nil {
		return *err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	return ctx.Err()
}

func (s *fakeStore) failWith(err error) { s.fail.Store(&err) }

func (s *fakeStore) written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(bytes.Join(s.bodies, nil))
}

// recorder counts every reading outcome the writer reports, by class and outcome.
type recorder struct {
	mu       sync.Mutex
	received map[string]int
	outcomes map[string]map[string]int
	accepted []time.Duration
	held     map[string]int
	peak     map[string]int
}

func newRecorder() *recorder {
	return &recorder{
		received: map[string]int{},
		outcomes: map[string]map[string]int{},
		held:     map[string]int{},
		peak:     map[string]int{},
	}
}

func (r *recorder) ReadingsReceived(class string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.received[class] += n
}

func (r *recorder) ReadingsSettled(class, outcome string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.outcomes[class] == nil {
		r.outcomes[class] = map[string]int{}
	}
	r.outcomes[class][outcome] += n
}

func (r *recorder) ReadingsAccepted(_ string, after time.Duration, _ int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accepted = append(r.accepted, after)
}

func (r *recorder) Held(class string, bytes int, _ time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held[class] = bytes
	r.peak[class] = max(r.peak[class], bytes)
}

func (r *recorder) Request(string, int, time.Duration, error) {}

func (r *recorder) settled(class string) map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int{}
	maps.Copy(out, r.outcomes[class])
	return out
}

// answers collects each submission's single answer.
type answers struct {
	mu   sync.Mutex
	got  []telemetry.Outcome
	once map[int]int
	ch   chan telemetry.Outcome
}

func newAnswers() *answers {
	return &answers{once: map[int]int{}, ch: make(chan telemetry.Outcome, 4096)}
}

func (a *answers) done(id int) func(telemetry.Outcome) {
	return func(o telemetry.Outcome) {
		a.mu.Lock()
		a.once[id]++
		a.got = append(a.got, o)
		a.mu.Unlock()
		a.ch <- o
	}
}

func (a *answers) await(t *testing.T, within time.Duration) telemetry.Outcome {
	t.Helper()
	select {
	case o := <-a.ch:
		return o
	case <-time.After(within):
		t.Fatalf("no answer within %s", within)
		return telemetry.Outcome{}
	}
}

func (a *answers) answeredOnce(t *testing.T, n int) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	require.Len(t, a.once, n, "every submission is answered")
	for id, count := range a.once {
		require.Equalf(t, 1, count, "submission %d answered %d times", id, count)
	}
}

func readings(n int, at time.Time) []telemetry.Sample {
	out := make([]telemetry.Sample, n)
	for i := range out {
		out[i] = telemetry.Sample{
			Name: "opengate_edge_metric_avg", Value: float64(i), TS: at,
			Labels: map[string]string{"dim": "cpu.total"},
		}
	}
	return out
}

// readingTime is the instant every test reading is taken at.
var readingTime = time.Unix(1_763_000_000, 0)

// anyScope is a fresh machine of a fresh customer.
func anyScope() telemetry.Scope {
	return telemetry.Scope{Tenant: uuid.New(), Device: uuid.New()}
}

// plainWriter starts a writer over a fresh store with no observer, and the answers it gives.
func plainWriter(t *testing.T) (*fakeStore, *telemetry.Writer, *answers) {
	t.Helper()
	store := &fakeStore{}
	return store, started(t, quickConfig(store, nil)), newAnswers()
}

func quickConfig(store telemetry.Importer, obs telemetry.WriterObserver) telemetry.WriterConfig {
	cfg := telemetry.DefaultWriterConfig()
	cfg.Store = store
	cfg.Observer = obs
	cfg.FlushAge = 20 * time.Millisecond
	return cfg
}

func started(t *testing.T, cfg telemetry.WriterConfig) *telemetry.Writer {
	t.Helper()
	w := telemetry.NewWriter(cfg)
	ctx, stop := context.WithCancel(context.Background())
	done := w.Start(ctx)
	t.Cleanup(func() {
		stop()
		<-done
	})
	return w
}
