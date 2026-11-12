package telemetry

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// resendMargin reaches back past the store's unsaved second and its 5 s save interval.
const resendMargin = 30 * time.Second

// StoreWatchInterval is how often the store's start time is read, well inside resendMargin.
const StoreWatchInterval = 5 * time.Second

// StartTimeReader reads when the store process started.
type StartTimeReader interface {
	StartTime(ctx context.Context) (time.Time, error)
}

// StoreWatch notices a store restart by its start time, which moves only with a new process.
// process_start_time_seconds moves with the host clock, so the store's own start stamp is read.
type StoreWatch struct {
	reader    StartTimeReader
	onRestart func(since time.Time)
	now       func() time.Time

	started  time.Time
	lastRead time.Time
}

// NewStoreWatch calls onRestart with the time machines are asked to send again from.
func NewStoreWatch(reader StartTimeReader, onRestart func(since time.Time)) *StoreWatch {
	return &StoreWatch{reader: reader, onRestart: onRestart, now: time.Now}
}

// Poll reads the start time once. A changed one raises a restart from 30 s before the last read
// of the old one, since anything accepted after that read may be lost.
func (s *StoreWatch) Poll(ctx context.Context) {
	started, err := s.reader.StartTime(ctx)
	if err != nil {
		return
	}
	now := s.now()
	if !s.started.IsZero() && !started.Equal(s.started) {
		s.onRestart(s.lastRead.Add(-resendMargin))
	}
	s.started = started
	s.lastRead = now
}

// Run polls every interval until ctx ends.
func (s *StoreWatch) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// startTimeMetric is VictoriaMetrics' own process start stamp, in seconds.
const startTimeMetric = "vm_app_start_timestamp"

// StartTime reads the store's start stamp from its own metrics page.
func (v *VMClient) StartTime(ctx context.Context) (time.Time, error) {
	resp, err := v.getChecked(ctx, "/metrics", "metrics")
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	lines := bufio.NewScanner(resp.Body)
	for lines.Scan() {
		value, found := strings.CutPrefix(lines.Text(), startTimeMetric+" ")
		if !found {
			continue
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse %s: %w", startTimeMetric, err)
		}
		whole, frac := math.Modf(seconds)
		return time.Unix(int64(whole), int64(frac*1e9)), nil
	}
	if err := lines.Err(); err != nil {
		return time.Time{}, fmt.Errorf("read vm metrics: %w", err)
	}
	return time.Time{}, fmt.Errorf("vm metrics carry no %s", startTimeMetric)
}
