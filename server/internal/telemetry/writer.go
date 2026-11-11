package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Class is the queue a hand-off waits in. Each class has a sender of its own, so live readings
// never wait behind a catch-up request.
type Class uint8

const (
	// ClassLive carries readings a machine measured this minute.
	ClassLive Class = iota
	// ClassCatchUp carries history a machine sends after time offline.
	ClassCatchUp
)

// String is the class as the writer's metrics label it.
func (c Class) String() string {
	if c == ClassCatchUp {
		return "catch_up"
	}
	return "live"
}

// The reasons a hand-off's readings were not stored.
var (
	ErrQueueFull     = errors.New("telemetry queue full")
	ErrTombstoned    = errors.New("machine erased")
	ErrStoreWrite    = errors.New("store write failed")
	ErrWaitedTooLong = errors.New("catch-up batch waited past its bound")
	ErrShuttingDown  = errors.New("telemetry writer shutting down")
)

// Outcome answers one hand-off. Err is nil when every reading was stored or refused on its own.
type Outcome struct {
	Stored  int
	Refused int
	Err     error
}

// Submission is one message's readings, handed to the writer without waiting on the store.
type Submission struct {
	Class   Class
	Scope   Scope
	Samples []Sample
	// Encoded, when set, is Samples already encoded under Scope, which Submit takes as it is.
	Encoded *Encoded
	// Done is called exactly once, after the request carrying the readings is answered.
	Done func(Outcome)
}

// Importer writes encoded readings to the store in one request.
type Importer interface {
	Import(ctx context.Context, body []byte) error
}

// WriterObserver receives the writer's readings; labels are the class and outcome only.
type WriterObserver interface {
	ReadingsReceived(class string, n int)
	ReadingsSettled(class, outcome string, n int)
	ReadingsAccepted(class string, after time.Duration, n int)
	Held(class string, bytes int, oldest time.Duration)
	Request(class string, bytes int, took time.Duration, err error)
}

// WriterConfig sizes the writer; DefaultWriterConfig holds the starting values.
type WriterConfig struct {
	Store Importer
	// Tombstoned reports an erased machine, checked just before each request goes out.
	Tombstoned func(device uuid.UUID) bool
	Observer   WriterObserver

	LiveBound    int
	CatchUpBound int
	FlushBytes   int
	FlushAge     time.Duration
	WriteTimeout time.Duration
	// QueueWait is how long a catch-up batch may wait before it is answered unwritten.
	QueueWait time.Duration
	// ShutdownGrace is how long the writer keeps sending once it is told to stop.
	ShutdownGrace time.Duration
}

// lowMark is the share of the live bound below which dropped live minutes are asked for again.
const lowMark = 0.2

// DefaultWriterConfig is about 10 s of a 5,000-machine live flow and 2 s of the catch-up budget,
// flushed on vmagent's 1 s or a quarter of a mebibyte, with a write timeout inside the 15 s answer.
func DefaultWriterConfig() WriterConfig {
	return WriterConfig{
		LiveBound:     4 << 20,
		CatchUpBound:  8 << 20,
		FlushBytes:    256 << 10,
		FlushAge:      time.Second,
		WriteTimeout:  5 * time.Second,
		QueueWait:     10 * time.Second,
		ShutdownGrace: 5 * time.Second,
	}
}

// Writer is the server's one numeric writer: two bounded queues of encoded readings, one sender
// each, a fixed number of goroutines whatever the fleet size.
type Writer struct {
	cfg    WriterConfig
	queues [2]*queue
	writes registry

	mu sync.Mutex
	// drops holds each machine's first live minute dropped since it was last reported.
	drops map[uuid.UUID]time.Time
}

// NewWriter builds a writer; Start runs its senders.
func NewWriter(cfg WriterConfig) *Writer {
	return &Writer{
		cfg: cfg,
		queues: [2]*queue{
			newQueue(ClassLive, cfg.LiveBound, cfg.Observer),
			newQueue(ClassCatchUp, cfg.CatchUpBound, cfg.Observer),
		},
		writes: registry{open: map[*writeToken]struct{}{}},
		drops:  map[uuid.UUID]time.Time{},
	}
}

// Start runs one sender per class until ctx ends; the returned channel closes once both have
// drained and answered everything they held.
func (w *Writer) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	var senders sync.WaitGroup
	for _, q := range w.queues {
		senders.Go(func() { w.run(ctx, q) })
	}
	go func() {
		senders.Wait()
		close(done)
	}()
	return done
}

// Submit encodes a hand-off at once and queues it; a refusal is answered before Submit returns.
func (w *Writer) Submit(sub Submission) {
	q := w.queues[sub.Class]
	class := sub.Class.String()
	if sub.Encoded == nil {
		sub.Encoded = Encode(sub.Scope, sub.Samples)
	}
	readings := sub.Encoded.readings
	if w.cfg.Observer != nil {
		w.cfg.Observer.ReadingsReceived(class, readings.kept+readings.refused)
	}
	if readings.refused > 0 && w.cfg.Observer != nil {
		w.cfg.Observer.ReadingsSettled(class, "invalid", readings.refused)
	}
	if readings.kept == 0 {
		sub.Done(Outcome{Refused: readings.refused})
		return
	}
	e := &entry{sub: sub, readings: readings, received: time.Now()}
	if err := q.push(e); err != nil {
		w.settle(q, e, err)
	}
}

// Fill is the share of a class's bound held right now.
func (w *Writer) Fill(class Class) float64 { return w.queues[class].fill() }

// settle answers an entry once: stored when err is nil, otherwise refused under err's reason.
func (w *Writer) settle(q *queue, e *entry, err error) {
	class := q.class.String()
	if err == nil {
		if w.cfg.Observer != nil {
			w.cfg.Observer.ReadingsSettled(class, "stored", e.readings.kept)
			w.cfg.Observer.ReadingsAccepted(class, time.Since(e.received), e.readings.kept)
		}
		e.sub.Done(Outcome{Stored: e.readings.kept, Refused: e.readings.refused})
		return
	}
	if w.cfg.Observer != nil {
		w.cfg.Observer.ReadingsSettled(class, outcomeLabel(err), e.readings.kept)
	}
	if q.class == ClassLive && (errors.Is(err, ErrQueueFull) || errors.Is(err, ErrStoreWrite)) {
		w.noteDrop(e.sub.Scope.Device, e.readings.first)
	}
	e.sub.Done(Outcome{Refused: e.readings.refused, Err: err})
}

// outcomeLabel is the metric spelling of the reason readings were not stored.
func outcomeLabel(err error) string {
	switch {
	case errors.Is(err, ErrQueueFull):
		return "queue_full"
	case errors.Is(err, ErrTombstoned):
		return "tombstoned"
	case errors.Is(err, ErrWaitedTooLong):
		return "waited_too_long"
	case errors.Is(err, ErrShuttingDown):
		return "shutdown"
	default:
		return "write_failed"
	}
}

// noteDrop keeps the earliest dropped live minute for a machine.
func (w *Writer) noteDrop(device uuid.UUID, at time.Time) {
	minute := at.UTC().Truncate(time.Minute)
	w.mu.Lock()
	defer w.mu.Unlock()
	if first, ok := w.drops[device]; !ok || minute.Before(first) {
		w.drops[device] = minute
	}
}

// LiveDrops hands over each machine's first dropped live minute once the live queue is under its
// low mark, and nothing while it is above; each drop is handed over once.
func (w *Writer) LiveDrops() map[uuid.UUID]time.Time {
	if w.queues[ClassLive].fill() >= lowMark {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.drops) == 0 {
		return nil
	}
	drops := w.drops
	w.drops = map[uuid.UUID]time.Time{}
	return drops
}

// WaitForWrites returns once every write registered so far for the device, or for every device
// of the tenant when device is nil, has been answered by the store.
func (w *Writer) WaitForWrites(ctx context.Context, tenant uuid.UUID, device *uuid.UUID) error {
	return w.writes.wait(ctx, tenant, device)
}

// writeToken marks one write between its registration and the store's answer.
type writeToken struct {
	scope Scope
	done  chan struct{}
}

// registry holds the writes in flight, which an erasure waits for before it deletes series.
type registry struct {
	mu   sync.Mutex
	open map[*writeToken]struct{}
}

func (r *registry) register(scope Scope) *writeToken {
	token := &writeToken{scope: scope, done: make(chan struct{})}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.open[token] = struct{}{}
	return token
}

func (r *registry) finish(token *writeToken) {
	r.mu.Lock()
	delete(r.open, token)
	r.mu.Unlock()
	close(token.done)
}

func (r *registry) wait(ctx context.Context, tenant uuid.UUID, device *uuid.UUID) error {
	r.mu.Lock()
	var pending []*writeToken
	for token := range r.open {
		if token.scope.Tenant == tenant && (device == nil || token.scope.Device == *device) {
			pending = append(pending, token)
		}
	}
	r.mu.Unlock()
	for _, token := range pending {
		select {
		case <-token.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
